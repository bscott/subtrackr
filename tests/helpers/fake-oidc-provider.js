// A small in-process OpenID Connect provider for the OIDC login spec.
//
// It implements the pieces SubTrackr's authorization-code flow needs: discovery,
// a JWKS endpoint, an authorization page the browser lands on (with "Sign in"
// and "Deny" buttons), a token endpoint that checks client credentials, the
// one-time code and the PKCE verifier, and a userinfo endpoint. ID tokens are
// RS256-signed JWTs. `misbehave` switches on deliberate faults (wrong nonce,
// rogue signing key, wrong issuer) so the spec can check that SubTrackr rejects
// bad tokens rather than only that the happy path works. No network access and
// no external credentials are involved.
const crypto = require('crypto');
const http = require('http');

function base64url(input) {
  return Buffer.from(input).toString('base64url');
}

function signJWT(claims, privateKey, kid) {
  const header = base64url(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid }));
  const payload = base64url(JSON.stringify(claims));
  const signature = crypto.sign('sha256', Buffer.from(`${header}.${payload}`), privateKey).toString('base64url');
  return `${header}.${payload}.${signature}`;
}

function parseForm(req) {
  return new Promise((resolve) => {
    let body = '';
    req.on('data', (chunk) => { body += chunk; });
    req.on('end', () => resolve(Object.fromEntries(new URLSearchParams(body))));
  });
}

function page(title, body) {
  return `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>${title}</title></head><body>${body}</body></html>`;
}

/**
 * Starts the provider on a free loopback port.
 *
 * @param {{ clientId: string, clientSecret: string, redirectURIs: string[], user?: object }} options
 */
async function startFakeOIDCProvider(options) {
  const { clientId, clientSecret, redirectURIs } = options;
  const user = { sub: 'user-1234', email: 'e2e@example.test', name: 'E2E Person', ...options.user };
  const { publicKey, privateKey } = crypto.generateKeyPairSync('rsa', { modulusLength: 2048 });
  const rogue = crypto.generateKeyPairSync('rsa', { modulusLength: 2048 });
  const kid = 'e2e-key-1';
  const provider = {
    issuer: '',
    /** Deliberate faults: { nonce: 'wrong' | undefined, rogueKey: bool, issuer: string | undefined } */
    misbehave: {},
    /** Every /authorize request the browser made, newest last. */
    authorizeRequests: [],
    /** Every /token request, newest last. */
    tokenRequests: [],
    pendingLogins: new Map(),
    codes: new Map(),
  };

  function discovery() {
    return {
      issuer: provider.issuer,
      authorization_endpoint: `${provider.issuer}/authorize`,
      token_endpoint: `${provider.issuer}/token`,
      jwks_uri: `${provider.issuer}/jwks`,
      userinfo_endpoint: `${provider.issuer}/userinfo`,
      response_types_supported: ['code'],
      subject_types_supported: ['public'],
      id_token_signing_alg_values_supported: ['RS256'],
      scopes_supported: ['openid', 'profile', 'email'],
      token_endpoint_auth_methods_supported: ['client_secret_basic', 'client_secret_post'],
      code_challenge_methods_supported: ['S256'],
    };
  }

  function jwks() {
    const jwk = publicKey.export({ format: 'jwk' });
    return { keys: [{ ...jwk, kid, use: 'sig', alg: 'RS256' }] };
  }

  function clientAuth(req, form) {
    const header = req.headers.authorization || '';
    if (header.startsWith('Basic ')) {
      const [id, secret] = Buffer.from(header.slice(6), 'base64').toString().split(':').map(decodeURIComponent);
      return { id, secret, method: 'client_secret_basic' };
    }
    return { id: form.client_id, secret: form.client_secret, method: 'client_secret_post' };
  }

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, provider.issuer);
    const json = (status, body) => {
      res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify(body));
    };
    const html = (status, body) => {
      res.writeHead(status, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(body);
    };

    if (req.method === 'GET' && url.pathname === '/.well-known/openid-configuration') return json(200, discovery());
    if (req.method === 'GET' && url.pathname === '/jwks') return json(200, jwks());

    if (req.method === 'GET' && url.pathname === '/authorize') {
      const params = Object.fromEntries(url.searchParams);
      provider.authorizeRequests.push(params);
      if (params.client_id !== clientId) return html(400, page('Error', `<p>unknown client ${params.client_id}</p>`));
      if (!redirectURIs.includes(params.redirect_uri)) return html(400, page('Error', `<p>redirect_uri not registered: ${params.redirect_uri}</p>`));
      if (params.response_type !== 'code') return html(400, page('Error', '<p>unsupported response_type</p>'));
      const sid = crypto.randomUUID();
      provider.pendingLogins.set(sid, params);
      return html(200, page('Fake IdP', `
        <h1>Fake identity provider</h1>
        <p>${clientId} is asking to sign you in as ${user.email}.</p>
        <form method="post" action="/authorize/decide">
          <input type="hidden" name="sid" value="${sid}">
          <button type="submit" name="decision" value="approve">Sign in</button>
          <button type="submit" name="decision" value="deny">Deny</button>
        </form>`));
    }

    if (req.method === 'POST' && url.pathname === '/authorize/decide') {
      const form = await parseForm(req);
      const params = provider.pendingLogins.get(form.sid);
      if (!params) return html(400, page('Error', '<p>unknown login</p>'));
      provider.pendingLogins.delete(form.sid);
      const redirect = new URL(params.redirect_uri);
      if (params.state) redirect.searchParams.set('state', params.state);
      if (form.decision !== 'approve') {
        redirect.searchParams.set('error', 'access_denied');
      } else {
        const code = crypto.randomBytes(24).toString('base64url');
        provider.codes.set(code, { ...params, issuedAt: Date.now() });
        redirect.searchParams.set('code', code);
      }
      res.writeHead(302, { Location: redirect.toString() });
      return res.end();
    }

    if (req.method === 'POST' && url.pathname === '/token') {
      const form = await parseForm(req);
      const auth = clientAuth(req, form);
      provider.tokenRequests.push({ ...form, auth: { id: auth.id, method: auth.method } });
      if (auth.id !== clientId || auth.secret !== clientSecret) return json(401, { error: 'invalid_client' });
      if (form.grant_type !== 'authorization_code') return json(400, { error: 'unsupported_grant_type' });
      const issued = provider.codes.get(form.code);
      provider.codes.delete(form.code);
      if (!issued) return json(400, { error: 'invalid_grant', error_description: 'unknown or already used code' });
      if (issued.redirect_uri !== form.redirect_uri) return json(400, { error: 'invalid_grant', error_description: 'redirect_uri mismatch' });
      if (issued.code_challenge) {
        if (issued.code_challenge_method !== 'S256') return json(400, { error: 'invalid_request', error_description: 'only S256 is supported' });
        const expected = crypto.createHash('sha256').update(form.code_verifier || '').digest('base64url');
        if (expected !== issued.code_challenge) return json(400, { error: 'invalid_grant', error_description: 'PKCE verifier mismatch' });
      }
      const now = Math.floor(Date.now() / 1000);
      const claims = {
        iss: provider.misbehave.issuer || provider.issuer,
        sub: user.sub,
        aud: clientId,
        iat: now,
        exp: now + 300,
        nonce: provider.misbehave.nonce !== undefined ? provider.misbehave.nonce : issued.nonce,
        email: user.email,
        name: user.name,
      };
      const key = provider.misbehave.rogueKey ? rogue.privateKey : privateKey;
      return json(200, {
        access_token: crypto.randomBytes(16).toString('base64url'),
        token_type: 'Bearer',
        expires_in: 300,
        id_token: signJWT(claims, key, kid),
      });
    }

    if (req.method === 'GET' && url.pathname === '/userinfo') return json(200, user);

    html(404, page('Not found', `<p>${req.method} ${url.pathname}</p>`));
  });

  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  provider.issuer = `http://127.0.0.1:${server.address().port}`;
  provider.user = user;
  provider.close = () => new Promise((resolve) => server.close(resolve));
  return provider;
}

module.exports = { startFakeOIDCProvider };
