const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');

test('a failed base URL save shows a safe error and keeps the form usable', async ({ page }) => {
  await page.goto('/settings');
  await page.route('**/api/settings/base-url', route => route.fulfill({
    status: 400, contentType: 'application/json',
    body: JSON.stringify({ error: 'Save failed <img src=x onerror=alert(1)>' }),
  }));
  await page.locator('#base-url-input').fill('https://example.test');
  await page.locator('form[hx-post="/api/settings/base-url"] button[type="submit"]').click();
  await expect(page.getByRole('alert')).toHaveText('Save failed <img src=x onerror=alert(1)>');
  await expect(page.getByRole('alert').locator('img')).toHaveCount(0);
  await expect(page.locator('#base-url-input')).toHaveValue('https://example.test');
  await page.unroute('**/api/settings/base-url');
  await page.locator('form[hx-post="/api/settings/base-url"] button[type="submit"]').click();
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('duplicating a removed subscription shows an error without destroying the list', async ({ page, request }) => {
  const sub = await seedSubscription(request, { name: 'Removed before duplicate' });
  await page.goto('/subscriptions');
  await request.delete(`/api/subscriptions/${sub.id}`);
  page.once('dialog', dialog => dialog.accept());
  await page.locator('tr', { hasText: sub.name }).getByTitle('Duplicate', { exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Failed to duplicate subscription');
  await expect(page.locator('#subscription-list')).toBeVisible();
  await expect(page.locator('tr', { hasText: sub.name })).toBeVisible();
});


test('a JSON server error on delete preserves the list and hides internal details', async ({ page, request }) => {
  const sub = await seedSubscription(request, { name: 'Delete failure keeps the table' });
  try {
    await page.goto('/subscriptions');
    await page.route(`**/api/subscriptions/${sub.id}`, route => route.fulfill({
      status: 500, contentType: 'application/json',
      body: JSON.stringify({ error: 'SQLite internal path /private/database.db' }),
    }));
    page.once('dialog', dialog => dialog.accept());
    await page.locator('tr', { hasText: sub.name }).getByTitle('Delete', { exact: true }).click();
    await expect(page.getByRole('alert')).toHaveText('Unable to complete this request. Please try again.');
    await expect(page.locator('#subscription-list table')).toBeVisible();
    await expect(page.locator('tr', { hasText: sub.name })).toBeVisible();
    await expect(page.locator('body')).not.toContainText('SQLite internal path');
  } finally {
    await request.delete(`/api/subscriptions/${sub.id}`);
  }
});
