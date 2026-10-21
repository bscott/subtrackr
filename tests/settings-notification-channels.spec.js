// @ts-check
// Settings page: SMTP, Pushover, Webhook and Telegram configuration.
//
// Tests marked test.fail() describe what users should see but currently do not.
// The ones about error messages fail because HTMX 1.9 does not swap 4xx/5xx
// responses, so the smtp-message.html error the handler returns never reaches
// the page. They should start passing after the HTMX 4 upgrade; remove the
// test.fail() then.
const http = require('http');
const { test, expect } = require('./helpers/settings-server');

test.describe.configure({ mode: 'serial' });

test.describe('SMTP', () => {
  test('saves the configuration and shows it after reload without the password', async ({ page, request }) => {
    const password = `smtp-secret-${Date.now()}`;
    await page.goto('/settings');
    await page.locator('#smtp_host').fill('127.0.0.1');
    await page.locator('#smtp_port').fill('2525');
    await page.locator('#smtp_username').fill('mailer@example.test');
    await page.locator('#smtp_password').fill(password);
    await page.locator('#smtp_from').fill('noreply@example.test');
    await page.locator('#smtp_from_name').fill('SubTrackr E2E');
    await page.locator('#smtp_to').fill('owner@example.test');
    await page.locator('#smtp-form button[type="submit"]').click();
    await expect(page.locator('#smtp-message')).toHaveText('SMTP settings saved successfully');

    await page.reload();
    await expect(page.locator('#smtp_host')).toHaveValue('127.0.0.1');
    await expect(page.locator('#smtp_port')).toHaveValue('2525');
    await expect(page.locator('#smtp_username')).toHaveValue('mailer@example.test');
    await expect(page.locator('#smtp_from')).toHaveValue('noreply@example.test');
    await expect(page.locator('#smtp_from_name')).toHaveValue('SubTrackr E2E');
    await expect(page.locator('#smtp_to')).toHaveValue('owner@example.test');
    await expect(page.locator('#smtp_password')).toHaveValue('');
    expect(await page.content()).not.toContain(password);

    const api = await (await request.get('/api/settings/smtp')).json();
    expect(api.configured).toBe(true);
    expect(JSON.stringify(api)).toContain('mailer@example.test');
    expect(JSON.stringify(api)).not.toContain(password);
  });

  test('save with missing required fields shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: the handler answers 400 with smtp-message.html,
    // which HTMX 1.9 does not swap into #smtp-message.
    test.fail();
    await page.goto('/settings');
    await page.locator('#smtp_host').fill('');
    await page.locator('#smtp_password').fill('');
    await page.locator('#smtp-form button[type="submit"]').click();
    await expect(page.locator('#smtp-message')).toContainText('Required SMTP fields');
  });

  test('Test connection shows the error for an unreachable server', async ({ page }) => {
    // Should pass after HTMX 4: every failed SMTP test is a 400 response,
    // which HTMX 1.9 does not swap into #smtp-message.
    test.fail();
    await page.goto('/settings');
    await page.locator('#smtp_host').fill('127.0.0.1');
    await page.locator('#smtp_port').fill('1');
    await page.locator('#smtp_username').fill('user');
    await page.locator('#smtp_password').fill('password');
    await page.locator('#test-smtp-btn').click();
    await expect(page.locator('#smtp-message')).toContainText('Failed to connect');
  });
});

test.describe('Pushover', () => {
  test('saves the configuration and keeps the user key after reload', async ({ page, request }) => {
    await page.goto('/settings');
    await page.locator('#pushover_user_key').fill('u-e2e-user-key');
    await page.locator('#pushover_app_token').fill('a-e2e-app-token');
    await page.locator('#pushover-form button[type="submit"]').click();
    await expect(page.locator('#pushover-message')).toHaveText('Pushover settings saved successfully');

    await page.reload();
    await expect(page.locator('#pushover_user_key')).toHaveValue('u-e2e-user-key');
    const api = await (await request.get('/api/settings/pushover')).json();
    expect(api).toEqual({ configured: true, has_user_key: true, has_app_token: true });
  });

  test('does not render the saved app token back into the page', async ({ page }) => {
    const token = `a-secret-${Date.now()}`;
    await page.goto('/settings');
    await page.locator('#pushover_user_key').fill('u-e2e-user-key');
    await page.locator('#pushover_app_token').fill(token);
    await page.locator('#pushover-form button[type="submit"]').click();
    await expect(page.locator('#pushover-message')).toHaveText('Pushover settings saved successfully');

    await page.reload();
    await expect(page.locator('#pushover_app_token')).toHaveValue('');
    expect(await page.content()).not.toContain(token);
  });

  test('save with a missing app token shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    await page.locator('#pushover_user_key').fill('u-e2e-user-key');
    await page.locator('#pushover_app_token').fill('');
    await page.locator('#pushover-form button[type="submit"]').click();
    await expect(page.locator('#pushover-message')).toContainText('User Key and App Token are required');
  });

  test('Test connection with missing fields shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    await page.locator('#pushover_user_key').fill('');
    await page.locator('#pushover_app_token').fill('');
    await page.locator('#pushover-form button', { hasText: 'Test' }).click();
    await expect(page.locator('#pushover-message')).toContainText('User Key and App Token are required for testing');
  });
});

test.describe('Webhook', () => {
  /** @type {http.Server} */
  let receiver;
  /** @type {{ headers: http.IncomingHttpHeaders, body: any }[]} */
  let received;
  let receiverURL = '';

  test.beforeAll(async () => {
    received = [];
    receiver = http.createServer((req, res) => {
      let body = '';
      req.on('data', (chunk) => { body += chunk; });
      req.on('end', () => {
        received.push({ headers: req.headers, body: JSON.parse(body || 'null') });
        res.writeHead(200).end('ok');
      });
    });
    await new Promise((r) => receiver.listen(0, '127.0.0.1', () => r(undefined)));
    const address = receiver.address();
    if (!address || typeof address === 'string') throw new Error('webhook receiver has no port');
    receiverURL = `http://127.0.0.1:${address.port}/hook`;
  });

  test.afterAll(async () => {
    await new Promise((r) => receiver.close(() => r(undefined)));
  });

  test('saves the URL and headers and shows them after reload', async ({ page }) => {
    await page.goto('/settings');
    await page.locator('#webhook_url').fill(receiverURL);
    await page.locator('#webhook_headers').fill('X-E2E-Source: settings-spec\nX-Team: billing');
    await page.locator('#webhook-form button[type="submit"]').click();
    await expect(page.locator('#webhook-message')).toHaveText('Webhook settings saved successfully');

    await page.reload();
    await expect(page.locator('#webhook_url')).toHaveValue(receiverURL);
    const headers = await page.locator('#webhook_headers').inputValue();
    expect(headers).toContain('X-E2E-Source: settings-spec');
    expect(headers).toContain('X-Team: billing');
  });

  test('Test connection posts a test payload and shows success', async ({ page }) => {
    received.length = 0;
    await page.goto('/settings');
    await page.locator('#webhook_url').fill(receiverURL);
    await page.locator('#webhook_headers').fill('X-E2E-Test: yes');
    await page.locator('#webhook-form button', { hasText: 'Test' }).click();
    await expect(page.locator('#webhook-message')).toContainText('Webhook test successful!');

    expect(received).toHaveLength(1);
    expect(received[0].body.event).toBe('test');
    expect(received[0].headers['x-e2e-test']).toBe('yes');
  });

  test('save with a non-HTTP URL shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    await page.locator('#webhook_url').fill('ftp://example.test/hook');
    await page.locator('#webhook-form button[type="submit"]').click();
    await expect(page.locator('#webhook-message')).toContainText('Webhook URL must use http:// or https:// scheme');
  });

  test('Test connection shows the error when the endpoint fails', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    // Port 1 on loopback refuses the connection immediately.
    await page.locator('#webhook_url').fill('http://127.0.0.1:1/hook');
    await page.locator('#webhook-form button', { hasText: 'Test' }).click();
    await expect(page.locator('#webhook-message')).toContainText('Webhook test failed');
  });
});

test.describe('Telegram', () => {
  test('saves the configuration and keeps the chat ID after reload', async ({ page, request }) => {
    await page.goto('/settings');
    await page.locator('#telegram_bot_token').fill('123456:e2e-bot-token');
    await page.locator('#telegram_chat_id').fill('-100123456');
    await page.locator('#telegram-form button[type="submit"]').click();
    await expect(page.locator('#telegram-message')).toHaveText('Telegram settings saved successfully');

    await page.reload();
    await expect(page.locator('#telegram_chat_id')).toHaveValue('-100123456');
    const api = await (await request.get('/api/settings/telegram')).json();
    expect(api).toEqual({ configured: true, has_bot_token: true, has_chat_id: true });
  });

  test('does not render the saved bot token back into the page', async ({ page }) => {
    const token = `987654:secret-${Date.now()}`;
    await page.goto('/settings');
    await page.locator('#telegram_bot_token').fill(token);
    await page.locator('#telegram_chat_id').fill('-100123456');
    await page.locator('#telegram-form button[type="submit"]').click();
    await expect(page.locator('#telegram-message')).toHaveText('Telegram settings saved successfully');

    await page.reload();
    await expect(page.locator('#telegram_bot_token')).toHaveValue('');
    expect(await page.content()).not.toContain(token);
  });

  test('save with a missing chat ID shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    await page.locator('#telegram_bot_token').fill('123456:e2e-bot-token');
    await page.locator('#telegram_chat_id').fill('');
    await page.locator('#telegram-form button[type="submit"]').click();
    await expect(page.locator('#telegram-message')).toContainText('Bot Token and Chat ID are required');
  });

  test('Test connection with missing fields shows the validation error', async ({ page }) => {
    // Should pass after HTMX 4: 400 response is not swapped by HTMX 1.9.
    test.fail();
    await page.goto('/settings');
    await page.locator('#telegram_bot_token').fill('');
    await page.locator('#telegram_chat_id').fill('');
    await page.locator('#test-telegram-btn').click();
    await expect(page.locator('#telegram-message')).toContainText('Bot Token and Chat ID are required for testing');
  });
});
