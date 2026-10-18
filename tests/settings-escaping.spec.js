// @ts-check
const { test, expect } = require('@playwright/test');

test('settings shows category names as text, not markup', async ({ page, request }) => {
  const name = `<img src=x onerror="window.__categoryXSS=1"><b>bold</b> ${Date.now()}`;
  const created = await request.post('/api/categories', { data: { name } });
  expect(created.ok()).toBeTruthy();
  const category = await created.json();
  try {
    await page.goto('/settings');
    const row = page.locator(`#category-name-${category.id}`);
    await expect(row).toHaveText(name);
    await expect(row.locator('b')).toHaveCount(0);
    await expect(page.locator(`#edit-category-form-${category.id} input[name="name"]`)).toHaveValue(name);
    expect(await page.evaluate(() => window.__categoryXSS)).toBeUndefined();
  } finally {
    await request.delete(`/api/categories/${category.id}`);
  }
});
