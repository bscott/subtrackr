// @ts-check
// Settings page: iCal subscription feed and the application base URL.
// The iCal toggle and regenerate buttons use hx-swap="none" plus an
// htmx:afterRequest listener that reads the JSON response, so these tests
// cover the listener that has to be ported to HTMX 4.
const { test, expect } = require('./helpers/settings-server');

test.describe.configure({ mode: 'serial' });

const toggle = (page) => page.locator('#ical-toggle-btn');
const urlSection = (page) => page.locator('#ical-url-section');
const urlInput = (page) => page.locator('#ical-url-input');
const regenerate = (page) => urlSection(page).getByRole('button', { name: 'Regenerate' });

async function icalState(request) {
  return (await request.get('/api/settings/ical/url')).json();
}

/** Path of the feed, so requests go to the test server whatever host the URL names. */
function feedPath(url) {
  return new URL(url).pathname;
}

async function restoreICal(request, wantEnabled) {
  if ((await icalState(request)).enabled !== wantEnabled) {
    await request.post('/api/settings/ical/toggle');
  }
  expect((await icalState(request)).enabled).toBe(wantEnabled);
}

test.describe('iCal subscription', () => {
  test('the toggle shows a working feed URL and turning it off disables the feed', async ({ page, request }) => {
    const original = await icalState(request);
    expect(original.enabled).toBe(false);
    try {
      await page.goto('/settings');
      await expect(toggle(page)).toHaveAttribute('aria-checked', 'false');
      await expect(urlSection(page)).toBeHidden();

      await toggle(page).click();
      await expect(toggle(page)).toHaveAttribute('aria-checked', 'true');
      await expect(toggle(page)).toHaveClass(/bg-primary/);
      await expect(urlSection(page)).toBeVisible();
      await expect(urlInput(page)).toHaveValue(/\/ical\/[A-Za-z0-9_-]+=*$/);
      const url = await urlInput(page).inputValue();

      const feed = await request.get(feedPath(url));
      expect(feed.status()).toBe(200);
      expect(feed.headers()['content-type']).toContain('text/calendar');
      expect(await feed.text()).toContain('BEGIN:VCALENDAR');

      await page.reload();
      await expect(toggle(page)).toHaveAttribute('aria-checked', 'true');
      await expect(urlInput(page)).toHaveValue(url);

      await toggle(page).click();
      await expect(toggle(page)).toHaveAttribute('aria-checked', 'false');
      await expect(urlSection(page)).toBeHidden();
      expect((await request.get(feedPath(url))).status()).toBe(404);

      await page.reload();
      await expect(toggle(page)).toHaveAttribute('aria-checked', 'false');
      await expect(urlSection(page)).toBeHidden();
    } finally {
      await restoreICal(request, original.enabled);
    }
  });

  test('regenerate replaces the token after confirming and revokes the old URL', async ({ page, request }) => {
    const original = await icalState(request);
    try {
      await page.goto('/settings');
      if ((await toggle(page).getAttribute('aria-checked')) !== 'true') await toggle(page).click();
      await expect(urlInput(page)).toHaveValue(/\/ical\//);
      const oldURL = await urlInput(page).inputValue();

      // Cancelling the confirm keeps the current URL working.
      page.once('dialog', (dialog) => dialog.dismiss());
      await regenerate(page).click();
      await expect(urlInput(page)).toHaveValue(oldURL);
      expect((await request.get(feedPath(oldURL))).status()).toBe(200);

      page.once('dialog', (dialog) => {
        expect(dialog.message()).toContain('Your old subscription URL will stop working');
        dialog.accept();
      });
      await regenerate(page).click();
      await expect(urlInput(page)).not.toHaveValue(oldURL);
      await expect(urlInput(page)).toHaveValue(/\/ical\/[A-Za-z0-9_-]+=*$/);
      const newURL = await urlInput(page).inputValue();

      expect((await request.get(feedPath(oldURL))).status()).toBe(401);
      const feed = await request.get(feedPath(newURL));
      expect(feed.status()).toBe(200);
      expect(await feed.text()).toContain('BEGIN:VCALENDAR');

      await page.reload();
      await expect(urlInput(page)).toHaveValue(newURL);
    } finally {
      await restoreICal(request, original.enabled);
    }
  });
});

test.describe('base URL', () => {
  test('saving the base URL persists it and is used for the iCal feed URL', async ({ page, request }) => {
    const input = page.locator('#base-url-input');
    const save = page.locator('form[hx-post="/api/settings/base-url"] button[type="submit"]');
    await page.goto('/settings');
    const originalBaseURL = await input.inputValue();
    const originalICal = await icalState(request);
    try {
      await input.fill('https://subs.example.test');
      await save.click();
      await expect(save).toHaveText('Saved!');
      await expect(save).toHaveText('Save', { timeout: 5000 });

      await page.reload();
      await expect(input).toHaveValue('https://subs.example.test');
      await expect(page.locator('code', { hasText: '/auth/oidc/callback' })).toHaveText('https://subs.example.test/auth/oidc/callback');

      if ((await toggle(page).getAttribute('aria-checked')) !== 'true') await toggle(page).click();
      await expect(urlInput(page)).toHaveValue(/^https:\/\/subs\.example\.test\/ical\//);
      const feed = await request.get(feedPath(await urlInput(page).inputValue()));
      expect(feed.status()).toBe(200);
    } finally {
      await request.post('/api/settings/base-url', { form: { base_url: originalBaseURL } });
      await restoreICal(request, originalICal.enabled);
    }
    await page.reload();
    await expect(input).toHaveValue(originalBaseURL);
  });
});
