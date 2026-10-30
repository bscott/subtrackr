// @ts-check
// Settings page: ntfy and Gotify configuration, against a private server and
// local HTTP sinks. No real notifications are sent.
//
const http = require('http');
const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');

test.describe.configure({ mode: 'serial' });

/**
 * Starts a sink on an ephemeral loopback port that records POST requests. Other
 * methods are answered but not recorded: local tools may probe a newly opened
 * port with GET, and every notification is a POST.
 */
async function startSink(status = 200, responseBody = '{"id":"e2e"}') {
  const requests = [];
  const server = http.createServer((req, res) => {
    let raw = '';
    req.setEncoding('utf8');
    req.on('data', (chunk) => { raw += chunk; });
    req.on('end', () => {
      if (req.method === 'POST') {
        requests.push({ url: req.url, headers: req.headers, body: JSON.parse(raw) });
      }
      res.writeHead(status, { 'Content-Type': 'application/json' });
      res.end(responseBody);
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = /** @type {import('net').AddressInfo} */ (server.address());
  return {
    url: `http://127.0.0.1:${port}`,
    requests,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

test.describe('ntfy', () => {
  test('starts with the public server and default priority', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.locator('#ntfy_server_url')).toHaveValue('https://ntfy.sh');
    await expect(page.locator('#ntfy_topic')).toHaveValue('');
    await expect(page.locator('#ntfy_priority')).toHaveValue('3');
    await expect(page.locator('#ntfy_clear_token')).toHaveCount(0);
  });

  test('Test sends the submitted values to the server and does not save them', async ({ page, request }) => {
    const sink = await startSink();
    try {
      await page.goto('/settings');
      await page.locator('#ntfy_server_url').fill(`${sink.url}/`);
      await page.locator('#ntfy_topic').fill('e2e_topic-1');
      await page.locator('#ntfy_access_token').fill('tk_e2e_form');
      await page.locator('#ntfy_priority').selectOption('5');
      await page.locator('#test-ntfy-btn').click();
      await expect(page.locator('#ntfy-message')).toContainText('ntfy test sent');

      expect(sink.requests).toHaveLength(1);
      const [sent] = sink.requests;
      expect(sent.url).toBe('/');
      expect(sent.headers.authorization).toBe('Bearer tk_e2e_form');
      expect(sent.body).toMatchObject({ topic: 'e2e_topic-1', title: 'SubTrackr Test', priority: 5 });

      const api = await (await request.get('/api/settings/ntfy')).json();
      expect(api.configured).toBe(false);
    } finally {
      await sink.close();
    }
  });

  test('saves the configuration and never renders the token back', async ({ page, request }) => {
    const sink = await startSink();
    const token = `tk_e2e_saved_${Date.now()}`;
    try {
      await page.goto('/settings');
      await page.locator('#ntfy_server_url').fill(sink.url);
      await page.locator('#ntfy_topic').fill('e2e_saved');
      await page.locator('#ntfy_access_token').fill(token);
      await page.locator('#ntfy_priority').selectOption('4');
      await page.locator('#ntfy-form button[type="submit"]').click();
      await expect(page.locator('#ntfy-message')).toHaveText('ntfy settings saved successfully');

      await page.reload();
      await expect(page.locator('#ntfy_server_url')).toHaveValue(sink.url);
      await expect(page.locator('#ntfy_topic')).toHaveValue('e2e_saved');
      await expect(page.locator('#ntfy_priority')).toHaveValue('4');
      await expect(page.locator('#ntfy_access_token')).toHaveValue('');
      await expect(page.locator('#ntfy_access_token')).toHaveAttribute('placeholder', /Token saved/);
      await expect(page.locator('#ntfy_clear_token')).toHaveCount(1);
      expect(await page.content()).not.toContain(token);
      const api = await (await request.get('/api/settings/ntfy')).text();
      expect(api).not.toContain(token);

      // A blank token field uses the saved token for the same server.
      await page.locator('#test-ntfy-btn').click();
      await expect(page.locator('#ntfy-message')).toContainText('ntfy test sent');
      expect(sink.requests.at(-1).headers.authorization).toBe(`Bearer ${token}`);

      // Saving with a blank token keeps it.
      await page.locator('#ntfy-form button[type="submit"]').click();
      await expect(page.locator('#ntfy-message')).toHaveText('ntfy settings saved successfully');
      await page.locator('#test-ntfy-btn').click();
      await expect(page.locator('#ntfy-message')).toContainText('ntfy test sent');
      expect(sink.requests.at(-1).headers.authorization).toBe(`Bearer ${token}`);

      // The checkbox removes it.
      await page.locator('#ntfy_clear_token').check();
      await page.locator('#ntfy-form button[type="submit"]').click();
      await expect(page.locator('#ntfy-message')).toHaveText('ntfy settings saved successfully');
      await page.reload();
      await expect(page.locator('#ntfy_clear_token')).toHaveCount(0);
      await page.locator('#test-ntfy-btn').click();
      await expect(page.locator('#ntfy-message')).toContainText('ntfy test sent');
      expect(sink.requests.at(-1).headers.authorization).toBeUndefined();
    } finally {
      await sink.close();
    }
  });

  test('rejects invalid input and provider failures with safe messages', async ({ request }) => {
    const invalid = await request.post('/api/settings/ntfy', {
      form: { ntfy_server_url: 'https://me:hunter2@ntfy.example', ntfy_topic: 't' },
    });
    expect(invalid.status()).toBe(400);
    const invalidBody = await invalid.text();
    expect(invalidBody).toContain('Server URL must be');
    expect(invalidBody).not.toContain('hunter2');

    const sink = await startSink(401, '{"error":"echo tk_e2e_leak"}');
    try {
      const failed = await request.post('/api/settings/ntfy/test', {
        form: { ntfy_server_url: sink.url, ntfy_topic: 't', ntfy_access_token: 'tk_e2e_leak' },
      });
      expect(failed.status()).toBe(400);
      const body = await failed.text();
      expect(body).toContain('HTTP 401');
      expect(body).not.toContain('tk_e2e_leak');
      expect(body).not.toContain(sink.url);
    } finally {
      await sink.close();
    }
  });

  test('shows a validation error on the page', async ({ page }) => {
    await page.goto('/settings');
    await page.locator('#ntfy_topic').fill('not a valid topic');
    await page.locator('#ntfy-form button[type="submit"]').click();
    await expect(page.locator('#ntfy-message')).toContainText('Topic may only contain');
  });
});

test.describe('Gotify', () => {
  test('saves, tests through the header token and never renders it back', async ({ page, request }) => {
    const sink = await startSink();
    const token = `AppE2E${Date.now()}`;
    try {
      await page.goto('/settings');
      await expect(page.locator('#gotify_priority')).toHaveValue('5');
      await page.locator('#gotify_server_url').fill(`${sink.url}/`);
      await page.locator('#gotify_app_token').fill(token);
      await page.locator('#gotify_priority').fill('8');
      await page.locator('#test-gotify-btn').click();
      await expect(page.locator('#gotify-message')).toContainText('Gotify test sent');

      expect(sink.requests).toHaveLength(1);
      const [sent] = sink.requests;
      expect(sent.url).toBe('/message');
      expect(sent.headers['x-gotify-key']).toBe(token);
      expect(sent.body).toMatchObject({ title: 'SubTrackr Test', priority: 8 });
      expect((await (await request.get('/api/settings/gotify')).json()).configured).toBe(false);

      await page.locator('#gotify-form button[type="submit"]').click();
      await expect(page.locator('#gotify-message')).toHaveText('Gotify settings saved successfully');

      await page.reload();
      await expect(page.locator('#gotify_server_url')).toHaveValue(sink.url);
      await expect(page.locator('#gotify_priority')).toHaveValue('8');
      await expect(page.locator('#gotify_app_token')).toHaveValue('');
      await expect(page.locator('#gotify_app_token')).toHaveAttribute('placeholder', /Token saved/);
      expect(await page.content()).not.toContain(token);
      expect(await (await request.get('/api/settings/gotify')).text()).not.toContain(token);

      await page.locator('#test-gotify-btn').click();
      await expect(page.locator('#gotify-message')).toContainText('Gotify test sent');
      expect(sink.requests.at(-1).headers['x-gotify-key']).toBe(token);
    } finally {
      await sink.close();
    }
  });

  test('will not send the saved token to a different server', async ({ request }) => {
    const saved = await startSink();
    const other = await startSink();
    try {
      expect((await request.post('/api/settings/gotify', {
        form: { gotify_server_url: saved.url, gotify_app_token: 'AppE2EKeep' },
      })).ok()).toBeTruthy();
      const response = await request.post('/api/settings/gotify/test', { form: { gotify_server_url: other.url } });
      expect(response.status()).toBe(400);
      expect(await response.text()).toContain('Enter the token again');
      expect(other.requests).toHaveLength(0);
      expect(saved.requests).toHaveLength(0);
    } finally {
      await saved.close();
      await other.close();
    }
  });

  test('shows a malformed URL error on the page', async ({ page }) => {
    await page.goto('/settings');
    await page.locator('#gotify_server_url').fill('not a url');
    await page.locator('#test-gotify-btn').click();
    await expect(page.locator('#gotify-message')).toContainText('Server URL must be');
  });
});

test.describe('delivery', () => {
  test('a high-cost subscription reaches both channels with its name intact', async ({ request }) => {
    const ntfy = await startSink();
    const gotify = await startSink();
    try {
      expect((await request.post('/api/settings/ntfy', {
        form: { ntfy_server_url: ntfy.url, ntfy_topic: 'e2e_alerts', ntfy_priority: '4' },
      })).ok()).toBeTruthy();
      expect((await request.post('/api/settings/gotify', {
        form: { gotify_server_url: gotify.url, gotify_app_token: 'AppE2EDelivery', gotify_priority: '6' },
      })).ok()).toBeTruthy();
      expect((await request.post('/api/settings/notifications/threshold', { form: { high_cost_threshold: '5' } })).ok()).toBeTruthy();
      // An unset toggle does not send; off then on stores it explicitly.
      await request.post('/api/settings/notifications/highcost');
      await request.post('/api/settings/notifications/highcost');

      const name = 'Ünïcødé *Stream* 🎬 <b>';
      await seedSubscription(request, { name, cost: '99' });

      for (const sink of [ntfy, gotify]) {
        await expect.poll(() => sink.requests.length).toBe(1);
        expect(sink.requests[0].body.title).toBe(`High Cost Alert: ${name}`);
        expect(sink.requests[0].body.message).toContain(`Subscription: ${name}`);
        expect(sink.requests[0].body.message).toContain('$99.00 Monthly');
      }
      expect(ntfy.requests[0].url).toBe('/');
      expect(ntfy.requests[0].body).toMatchObject({ topic: 'e2e_alerts', priority: 4 });
      expect(gotify.requests[0].url).toBe('/message');
      expect(gotify.requests[0].headers['x-gotify-key']).toBe('AppE2EDelivery');
    } finally {
      await ntfy.close();
      await gotify.close();
    }
  });
});

test.describe('translations', () => {
  const headings = {
    en: ['ntfy Notifications', 'Gotify Notifications'],
    es: ['Notificaciones de ntfy', 'Notificaciones de Gotify'],
    de: ['ntfy-Benachrichtigungen', 'Gotify-Benachrichtigungen'],
    nl: ['ntfy-meldingen', 'Gotify-meldingen'],
    zh: ['ntfy 通知', 'Gotify 通知'],
  };

  for (const [lang, expected] of Object.entries(headings)) {
    test(`renders the new sections in ${lang}`, async ({ page, request }) => {
      expect((await request.post('/api/settings/language', { data: { lang } })).ok()).toBeTruthy();
      try {
        await page.goto('/settings');
        await expect(page.locator('html')).toHaveAttribute('lang', lang);
        for (const heading of expected) {
          await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
        }
        const body = await page.locator('body').innerText();
        expect(body).not.toMatch(/settings\.(ntfy|gotify|push)_/);
      } finally {
        await request.post('/api/settings/language', { data: { lang: 'en' } });
      }
    });
  }
});
