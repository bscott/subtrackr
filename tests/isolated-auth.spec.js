// Optional login, logout, and password reset. These change global server
// state, so they run in the `isolated` project against its own server (:8083).
const { test, expect } = require('@playwright/test');
const { startSmtpSink } = require('./helpers/isolated-smtp-sink');

const USERNAME = 'admin';
const PASSWORD = 'correct-horse-1';
const NEW_PASSWORD = 'battery-staple-2';

test.describe.configure({ mode: 'serial' });

let smtp;
// Tracks the password the server currently expects, so cleanup can log in.
let currentPassword = PASSWORD;

async function signIn(page, username, password) {
  await page.getByLabel('Username').fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

async function requestResetLink(page) {
  const message = smtp.nextMessage();
  await page.goto('/forgot-password');
  await page.getByRole('button', { name: 'Send Reset Link' }).click();
  await expect(page.getByText('Password reset link has been sent to your email')).toBeVisible();
  const raw = await message;
  const match = raw.match(/\/reset-password\?token=([^"&\s<]+)/);
  expect(match, 'reset email contains a reset link').toBeTruthy();
  return decodeURIComponent(match[1]);
}

test.beforeAll(async () => {
  smtp = await startSmtpSink();
});

test.afterAll(async ({ playwright, baseURL }) => {
  // Leave the server with login disabled even if a test failed midway.
  const request = await playwright.request.newContext({ baseURL });
  const status = await (await request.get('/api/settings/auth/status')).json().catch(() => null);
  if (status === null || status.enabled !== false) {
    await request.post('/api/auth/login', { form: { username: USERNAME, password: currentPassword } });
    await request.post('/api/settings/auth/disable');
  }
  await request.dispose();
  await smtp.close();
});

test('auth status reports login disabled and pages are open by default', async ({ page, request }) => {
  const status = await request.get('/api/settings/auth/status');
  expect(status.ok()).toBeTruthy();
  expect(await status.json()).toMatchObject({ enabled: false });

  await page.goto('/subscriptions');
  await expect(page).toHaveURL(/\/subscriptions$/);
});

test('login setup stays disabled until email is configured', async ({ page }) => {
  await page.goto('/settings');
  await page.locator('label:has(#auth-toggle)').click();
  await expect(page.getByText('Please configure email settings above before enabling login.')).toBeVisible();
  await expect(page.locator('#auth_username')).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Enable Authentication' })).toBeDisabled();
});

test('setup API rejects a request when email is not configured', async ({ request }) => {
  const response = await request.post('/api/settings/auth/setup', {
    form: { username: USERNAME, password: PASSWORD, confirm_password: PASSWORD },
  });
  expect(response.status()).toBe(400);
  expect(await response.text()).toContain('Please configure email settings first');
  expect(await (await request.get('/api/settings/auth/status')).json()).toMatchObject({ enabled: false });
});

test('enabling login from settings protects every page', async ({ page, browser, baseURL }) => {
  await page.goto('/settings');
  await page.locator('#smtp_host').fill(smtp.host);
  await page.locator('#smtp_port').fill(String(smtp.port));
  await page.locator('#smtp_username').fill('sink-user');
  await page.locator('#smtp_password').fill('sink-pass');
  await page.locator('#smtp_from').fill('subtrackr@example.test');
  await page.locator('#smtp_to').fill('owner@example.test');
  await page.getByRole('button', { name: 'Save SMTP Settings' }).click();
  await expect(page.locator('#smtp-message')).toContainText('SMTP settings saved successfully');

  await page.reload();
  await page.locator('label:has(#auth-toggle)').click();
  await page.locator('#auth_username').fill(USERNAME);
  await page.locator('#auth_password').fill(PASSWORD);
  await page.locator('#auth_confirm_password').fill(PASSWORD);
  await page.getByRole('button', { name: 'Enable Authentication' }).click();
  await expect(page.locator('#auth-message')).toContainText('Authentication enabled successfully');

  // Setup does not sign the admin in, so even the status endpoint now needs a session.
  expect((await page.request.get('/api/settings/auth/status')).status()).toBe(401);

  // A fresh visitor with no session is sent to the login page.
  const anonymous = await browser.newContext({ baseURL });
  const visitor = await anonymous.newPage();
  for (const path of ['/', '/dashboard', '/subscriptions', '/analytics', '/settings', '/calendar']) {
    await visitor.goto(path);
    await expect(visitor, `${path} redirects to login`).toHaveURL(
      new RegExp(`/login\\?redirect=${encodeURIComponent(path)}$`),
    );
  }
  await expect(visitor.getByRole('heading', { name: 'Sign in to SubTrackr' })).toBeVisible();

  // API calls without a session get 401 instead of a redirect.
  const api = await anonymous.request.get('/api/subscriptions', { headers: { Accept: 'application/json' } });
  expect(api.status()).toBe(401);
  await anonymous.close();
});

test('unauthenticated callers cannot change login settings', async ({ request }) => {
  // Login is enabled now, so these calls are unauthenticated and must not change anything.
  const response = await request.post('/api/settings/auth/setup', {
    form: { username: 'intruder', password: 'whatever123', confirm_password: 'whatever123' },
  });
  expect(response.status()).toBe(401);
  expect((await request.post('/api/settings/auth/disable')).status()).toBe(401);
});

test('a wrong password shows the login error and keeps the user out', async ({ page }) => {
  // BUG (htmx 1.9.10): /api/auth/login answers a bad password with HTTP 401 and
  // an error fragment for #login-error, but htmx 1.x does not swap 4xx
  // responses, so nothing is shown. Remove test.fail() once the error renders.
  test.fail();
  await page.goto('/login');
  await signIn(page, USERNAME, 'wrong-password');
  await expect(page.locator('#login-error')).toContainText('Invalid username or password');
});

test('a wrong password does not create a session', async ({ page }) => {
  await page.goto('/login');
  const login = page.waitForResponse('**/api/auth/login');
  await signIn(page, USERNAME, 'wrong-password');
  expect((await login).status()).toBe(401);
  await page.goto('/subscriptions');
  await expect(page).toHaveURL(/\/login\?redirect=%2Fsubscriptions$/);
});

test('the right password logs in and returns to the requested page', async ({ page }) => {
  await page.goto('/subscriptions');
  await expect(page).toHaveURL(/\/login\?redirect=%2Fsubscriptions$/);

  const login = page.waitForResponse('**/api/auth/login');
  await signIn(page, USERNAME, PASSWORD);
  expect((await login).headers()['hx-redirect']).toBe('/subscriptions');
  await expect(page).toHaveURL(/\/subscriptions$/);

  const status = await page.request.get('/api/settings/auth/status');
  expect(await status.json()).toMatchObject({ enabled: true, username: USERNAME });

  // The session sticks: other pages load, and /login bounces back to the app.
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  await page.goto('/login');
  await expect(page).toHaveURL(/\/$/);
});

test('login ignores an off-site redirect target', async ({ page }) => {
  await page.goto('/login?redirect=' + encodeURIComponent('https://evil.example/'));
  await signIn(page, USERNAME, PASSWORD);
  await expect(page).toHaveURL(/localhost:8083\/$/);
});

test('logout ends the session', async ({ page }) => {
  await page.goto('/login');
  await signIn(page, USERNAME, PASSWORD);
  await expect(page).toHaveURL(/\/$/);

  // No page links to logout today, so visit the endpoint directly.
  await page.goto('/api/auth/logout');
  await expect(page).toHaveURL(/\/login$/);
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/login\?redirect=%2Fdashboard$/);
});

test('forgot and reset password pages render', async ({ page }) => {
  await page.goto('/login');
  await page.getByRole('link', { name: 'Forgot your password?' }).click();
  await expect(page).toHaveURL(/\/forgot-password$/);
  await expect(page.getByRole('button', { name: 'Send Reset Link' })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Login' }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test('an invalid or missing reset token shows the error page', async ({ page }) => {
  let response = await page.goto('/reset-password?token=not-a-real-token');
  expect(response.status()).toBe(400);
  await expect(page.getByText('Invalid or expired reset token')).toBeVisible();
  await expect(page.getByLabel('New Password')).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Request a new reset link' })).toHaveAttribute('href', '/forgot-password');

  response = await page.goto('/reset-password');
  expect(response.status()).toBe(400);
  await expect(page.getByText('Invalid reset token')).toBeVisible();
});

test('a tampered reset token does not change the password', async ({ page }) => {
  const token = await requestResetLink(page);
  await page.goto('/reset-password?token=' + encodeURIComponent(token));
  // Tamper with the hidden token, as if the link had been altered.
  await page.locator('input[name="token"]').evaluate((input) => { input.value = 'tampered'; });
  await page.getByLabel('New Password').fill(NEW_PASSWORD);
  await page.getByLabel('Confirm Password').fill(NEW_PASSWORD);
  const reset = page.waitForResponse('**/api/auth/reset-password');
  await page.getByRole('button', { name: 'Reset Password' }).click();
  expect((await reset).status()).toBe(400);
  await page.goto('/login');
  await signIn(page, USERNAME, PASSWORD);
  await expect(page).toHaveURL(/\/$/);
});

test('reset error messages are shown to the user', async ({ page }) => {
  // BUG (htmx 1.9.10): /api/auth/reset-password answers validation failures
  // (mismatch, too short, bad token) with HTTP 400 and an error fragment, but
  // htmx 1.x does not swap 4xx responses, so the form just sits there.
  // Remove test.fail() once the error renders.
  test.fail();
  const token = await requestResetLink(page);
  await page.goto('/reset-password?token=' + encodeURIComponent(token));
  await page.getByLabel('New Password').fill(NEW_PASSWORD);
  await page.getByLabel('Confirm Password').fill(NEW_PASSWORD + 'x');
  await page.getByRole('button', { name: 'Reset Password' }).click();
  await expect(page.getByText('Passwords do not match')).toBeVisible();
});

test('a reset link from email sets a new password once', async ({ page }) => {
  const token = await requestResetLink(page);
  expect(smtp.messages.at(-1)).toContain('Subject: SubTrackr Password Reset');

  await page.goto('/reset-password?token=' + encodeURIComponent(token));
  await expect(page.getByRole('heading', { name: 'Set New Password' })).toBeVisible();
  await page.getByLabel('New Password').fill(NEW_PASSWORD);
  await page.getByLabel('Confirm Password').fill(NEW_PASSWORD);
  await page.getByRole('button', { name: 'Reset Password' }).click();
  await expect(page.getByText('Password reset successfully')).toBeVisible();
  currentPassword = NEW_PASSWORD;

  // The used token is cleared.
  await page.goto('/reset-password?token=' + encodeURIComponent(token));
  await expect(page.getByText('Invalid or expired reset token')).toBeVisible();

  // The old password no longer works; the new one does.
  await page.goto('/login');
  const failed = page.waitForResponse('**/api/auth/login');
  await signIn(page, USERNAME, PASSWORD);
  expect((await failed).status()).toBe(401);
  await signIn(page, USERNAME, NEW_PASSWORD);
  await expect(page).toHaveURL(/\/$/);
});

test('disabling login from settings opens the app again', async ({ page, browser, baseURL }) => {
  await page.goto('/login?redirect=%2Fsettings');
  await signIn(page, USERNAME, currentPassword);
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByText('Authentication is enabled')).toBeVisible();
  await expect(page.getByText(`Username: ${USERNAME}`)).toBeVisible();

  await page.getByRole('button', { name: 'Disable Authentication' }).click();
  await expect(page.locator('#auth-message')).toContainText('Authentication disabled successfully');

  const status = await (await page.request.get('/api/settings/auth/status')).json();
  expect(status).toMatchObject({ enabled: false });

  const anonymous = await browser.newContext({ baseURL });
  const visitor = await anonymous.newPage();
  await visitor.goto('/subscriptions');
  await expect(visitor).toHaveURL(/\/subscriptions$/);
  await anonymous.close();
});
