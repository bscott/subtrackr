// End-to-end OpenID Connect login against an in-process fake identity provider
// (tests/helpers/fake-oidc-provider.js). The app runs on a private server
// (tests/helpers/settings-server.js) because enabling OIDC gates every page
// behind login. No external IdP, network access or credentials are involved.
//
// Covers the browser flow (login page, provider consent, callback, session),
// the protocol details SubTrackr promises (PKCE S256, nonce, state, one-time
// callback), what happens when the provider misbehaves (denied login, wrong
// nonce, token signed by another key, wrong issuer), and how protected
// requests are answered once the session is gone (HTML, JSON and HTMX).
const base = require('./helpers/settings-server');
const { startFakeOIDCProvider } = require('./helpers/fake-oidc-provider');

const CLIENT_ID = 'subtrackr-e2e';
const CLIENT_SECRET = 'subtrackr-e2e-client-secret';

const test = base.test.extend({
  idp: [async ({ isolatedServer }, use) => {
    const provider = await startFakeOIDCProvider({
      clientId: CLIENT_ID,
      clientSecret: CLIENT_SECRET,
      redirectURIs: [`${isolatedServer.baseURL}/auth/oidc/callback`],
    });
    await use(provider);
    await provider.close();
  }, { scope: 'worker' }],
});
const { expect } = base;

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ playwright, baseURL, idp }) => {
  const request = await playwright.request.newContext({ baseURL });
  const base = await request.post('/api/settings/base-url', { form: { base_url: baseURL } });
  expect(base.ok(), await base.text()).toBeTruthy();
  const oidc = await request.post('/api/settings/auth/oidc', { form: {
    enabled: 'true', display_name: 'Fake IdP', issuer_url: idp.issuer,
    client_id: CLIENT_ID, client_secret: CLIENT_SECRET, scopes: 'openid profile email',
  } });
  expect(oidc.status(), await oidc.text()).toBe(200);
  await request.dispose();
});

test.beforeEach(({ idp }) => {
  idp.misbehave = {};
});

async function startLoginFromProtectedPage(page, path = '/settings') {
  await page.goto(path);
  await expect(page).toHaveURL(/\/login\?redirect=/);
  const oidcLink = page.locator('a[href^="/auth/oidc/login"]');
  await expect(oidcLink).toHaveCount(1);
  await expect(oidcLink).toHaveText(/Fake IdP/);
  await oidcLink.click();
  await expect(page.getByRole('heading', { name: 'Fake identity provider' })).toBeVisible();
}

test('the login page offers only the provider button while local login is disabled', async ({ page }) => {
  await page.goto('/subscriptions');
  await expect(page).toHaveURL(/\/login\?redirect=%2Fsubscriptions/);
  await expect(page.locator('a[href^="/auth/oidc/login"]')).toHaveText(/Fake IdP/);
  await expect(page.getByLabel('Username')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Sign in' })).toHaveCount(0);
});

test('signing in at the provider returns to the requested page with a session', async ({ page, idp, baseURL }) => {
  let callbackURL = '';
  page.on('request', (request) => {
    if (request.url().startsWith(`${baseURL}/auth/oidc/callback?`)) callbackURL = request.url();
  });

  await startLoginFromProtectedPage(page, '/settings');

  const authorize = idp.authorizeRequests.at(-1);
  expect(authorize.redirect_uri).toBe(`${baseURL}/auth/oidc/callback`);
  expect(authorize.scope.split(' ')).toEqual(expect.arrayContaining(['openid', 'profile', 'email']));
  expect(authorize.code_challenge_method).toBe('S256');
  expect(authorize.code_challenge).toMatch(/^[A-Za-z0-9_-]{43}$/);
  expect(authorize.nonce).toMatch(/^[A-Za-z0-9_-]{20,}$/);
  expect(authorize.state).toMatch(/^[A-Za-z0-9_-]{20,}$/);

  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(`${baseURL}/settings`);
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  expect(callbackURL, 'browser visited the SubTrackr callback').toContain('code=');

  const token = idp.tokenRequests.at(-1);
  expect(token.grant_type).toBe('authorization_code');
  expect(token.code_verifier, 'token exchange carries the PKCE verifier').toMatch(/^[A-Za-z0-9_.~-]{43,128}$/);
  expect(token.auth.id).toBe(CLIENT_ID);

  // The settings page shows OIDC as enabled without echoing the client secret.
  await expect(page.locator('#oidc_enabled')).toBeChecked();
  await expect(page.locator('#oidc_issuer_url')).toHaveValue(idp.issuer);
  await expect(page.locator('#oidc_client_secret')).toHaveValue('');
  await expect(page.locator('body')).not.toContainText(CLIENT_SECRET);

  // The callback is one-time: replaying it must not mint another session.
  const replay = await page.request.get(callbackURL, { maxRedirects: 0 });
  expect(replay.status()).toBe(302);
  expect(replay.headers().location).toMatch(/^\/login\?error=Invalid\+or\+expired\+OIDC\+login\+state/);

  // Other pages are open with the session, and the session survives a reload.
  await page.goto('/subscriptions');
  await expect(page).toHaveURL(/\/subscriptions$/);
  await page.reload();
  await expect(page).toHaveURL(/\/subscriptions$/);
});

test('denying the login at the provider shows an error on the login page without a session', async ({ page }) => {
  await startLoginFromProtectedPage(page);
  await page.getByRole('button', { name: 'Deny' }).click();
  await expect(page).toHaveURL(/\/login\?error=/);
  await expect(page.getByText('OIDC login was denied')).toBeVisible();
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/login\?redirect=/);
});

for (const [label, misbehave] of [
  ['an ID token with the wrong nonce', { nonce: 'not-the-nonce' }],
  ['an ID token signed by an unknown key', { rogueKey: true }],
  ['an ID token from another issuer', { issuer: 'http://evil.example.test' }],
]) {
  test(`${label} is rejected`, async ({ page, idp }) => {
    idp.misbehave = misbehave;
    await startLoginFromProtectedPage(page);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page).toHaveURL(/\/login\?error=/);
    await expect(page.getByText('OIDC login could not be verified')).toBeVisible();
    await page.goto('/settings');
    await expect(page).toHaveURL(/\/login\?redirect=/);
  });
}

test('a callback without a matching login state is rejected', async ({ page }) => {
  const response = await page.request.get('/auth/oidc/callback?state=forged&code=whatever', { maxRedirects: 0 });
  expect(response.status()).toBe(302);
  expect(response.headers().location).toMatch(/^\/login\?error=Invalid\+or\+expired\+OIDC\+login\+state/);
});

test('logging out protects pages again and unauthenticated requests get the right answer per client', async ({ page, baseURL }) => {
  await startLoginFromProtectedPage(page, '/calendar');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(`${baseURL}/calendar`);

  await page.goto('/api/auth/logout');
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/login\?redirect=%2Fsettings/);

  // Browser navigation: redirect to the login page, remembering where to return.
  const html = await page.request.get('/subscriptions', { maxRedirects: 0, headers: { Accept: 'text/html' } });
  expect(html.status()).toBe(302);
  expect(html.headers().location).toBe('/login?redirect=%2Fsubscriptions');

  // API clients: a JSON 401, no redirect.
  const json = await page.request.get('/api/subscriptions', { maxRedirects: 0, headers: { Accept: 'application/json' } });
  expect(json.status()).toBe(401);
  expect(await json.json()).toEqual({ error: 'Authentication required' });

  // HTMX requests: a 401 with HX-Redirect so the whole page goes to login and
  // comes back to the page the request was made from (never off-site).
  const htmx = await page.request.get('/api/subscriptions', { maxRedirects: 0, headers: {
    Accept: 'text/html', 'HX-Request': 'true', 'HX-Current-URL': `${baseURL}/subscriptions?sort=name`,
  } });
  expect(htmx.status()).toBe(401);
  expect(htmx.headers()['hx-redirect']).toBe('/login?redirect=%2Fsubscriptions%3Fsort%3Dname');
  const offsite = await page.request.get('/api/subscriptions', { maxRedirects: 0, headers: {
    Accept: 'text/html', 'HX-Request': 'true', 'HX-Current-URL': 'https://evil.example.test/phish',
  } });
  expect(offsite.headers()['hx-redirect']).toBe('/login?redirect=%2Fphish');

  // Public routes stay public.
  expect((await page.request.get('/healthz')).status()).toBe(200);
  expect((await page.request.get('/static/js/themes.js')).status()).toBe(200);
});

test('signing in again after logout works and lands on the dashboard by default', async ({ page, baseURL }) => {
  await page.goto('/login');
  await page.locator('a[href^="/auth/oidc/login"]').click();
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(`${baseURL}/`);
  await expect(page.getByRole('heading', { name: 'All Subscriptions' })).toBeVisible();
});
