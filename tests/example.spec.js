// @ts-check
const { test, expect } = require('@playwright/test');

test('has title', async ({ page }) => {
  await page.goto('/');

  // Expect a title "to contain" a substring.
  await expect(page).toHaveTitle(/SubTrackr/);
});

test('can navigate to subscriptions', async ({ page }) => {
  await page.goto('/');

  // Click the subscriptions link.
  await page.click('a[href="/subscriptions"]');

  // Expects page to have a heading with the name of subscriptions.
  await expect(page.getByRole('heading', { name: 'Subscriptions' })).toBeVisible();
});