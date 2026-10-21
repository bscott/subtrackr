// @ts-check
const { test, expect } = require('@playwright/test');

test('language selector switches nav to Spanish', async ({ page, request }) => {
  // Set language to Spanish via API
  await request.post('/api/settings/language', {
    data: { lang: 'es' },
  });

  await page.goto('/subscriptions');
  await page.waitForLoadState('networkidle');
  // Nav should be Spanish
  await expect(page.getByRole('link', { name: /Suscripciones/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Panel/ })).toBeVisible();
});

test('settings page shows language selector with options', async ({ request, page }) => {
  await request.post('/api/settings/language', { data: { lang: 'en' } });
  await page.goto('/settings');
  await page.waitForLoadState('networkidle');
  const langSelect = page.locator('#language-select');
  await expect(langSelect).toBeVisible();
  const options = await langSelect.locator('option').allTextContents();
  expect(options).toContain('English');
  expect(options).toContain('Español');
  expect(options).toContain('Deutsch');
  expect(options).toContain('Nederlands');
});
