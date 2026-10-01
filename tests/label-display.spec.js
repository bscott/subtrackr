// @ts-check
const { test, expect } = require('@playwright/test');

async function seedLabeledSubscription(request) {
  const name = `Label test ${Date.now()}`;
  const response = await request.post('/api/subscriptions', { form: {
    name, label: 'example.com', cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
  } });
  expect(response.ok()).toBeTruthy();
  return { name, id: (await response.json()).id };
}

test('label field renders under subscription name', async ({ page, request }) => {
  const subscription = await seedLabeledSubscription(request);
  try {
    await page.goto('/subscriptions');
    const row = page.locator('tbody tr', { hasText: subscription.name });
    await expect(row.getByText('example.com')).toBeVisible();
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});

test('label input visible in edit form', async ({ page, request }) => {
  const subscription = await seedLabeledSubscription(request);
  try {
    await page.goto('/subscriptions');
    const row = page.locator('tbody tr', { hasText: subscription.name });
    await row.locator('button[title="Edit"]').click();
    await expect(page.locator('input[name="label"]')).toHaveValue('example.com');
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});
