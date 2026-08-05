// @ts-check
const { test, expect } = require('@playwright/test');

test('updates the cost prefix and submits the selected currency', async ({ page }) => {
  let categories = await (await page.request.get('/api/categories')).json();
  if (categories.length === 0) {
    const categoryResponse = await page.request.post('/api/categories', {
      data: { name: `Currency Test ${Date.now()}` },
    });
    categories = [await categoryResponse.json()];
  }

  await page.goto('/subscriptions');
  await page.locator('header button:has-text("Add"), header button:has-text("Añadir")').first().click();
  await page.waitForSelector('input[name="name"]');

  const currency = page.locator('select[name="original_currency"]');
  const costPrefix = page.locator('#cost-currency-symbol');

  await currency.selectOption('SEK');
  await expect(costPrefix).toHaveText('kr');

  await currency.selectOption('USD');
  await expect(costPrefix).toHaveText('$');

  const subscriptionName = `USD Currency Test ${Date.now()}`;
  await page.fill('input[name="name"]', subscriptionName);
  await page.fill('input[name="cost"]', '9.99');
  await page.selectOption('select[name="category_id"]', String(categories[0].id));
  await page.selectOption('select#schedule_combo', 'Monthly_1');
  await page.selectOption('select[name="status"]', 'Active');

  const createResponse = page.waitForResponse(response =>
    response.url().endsWith('/api/subscriptions') && response.request().method() === 'POST'
  );
  await page.locator('form button[type="submit"]').click();
  await createResponse;
  await page.waitForLoadState('networkidle');

  const row = page.locator('tr', { hasText: subscriptionName }).first();
  await expect(row).toBeVisible();
  const editButton = row.locator('button[title*="Edit"], button[title*="Editar"], button[title*="Bearbeiten"], button[title*="Bewerken"]');
  const editPath = await editButton.getAttribute('hx-get');
  if (!editPath) throw new Error('Edit button is missing its subscription path');
  await editButton.click();
  await expect(page.locator('select[name="original_currency"]')).toHaveValue('USD');

  const subscriptionID = editPath.split('/').pop();
  await page.request.delete(`/api/subscriptions/${subscriptionID}`);
});
