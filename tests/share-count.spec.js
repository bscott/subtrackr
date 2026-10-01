// @ts-check
const { test, expect } = require('@playwright/test');
const { seedSubscription } = require('./helpers/subscriptions');

test('shared subscription shows split badge with your share', async ({ page, request }) => {
  const subscription = await seedSubscription(request, { cost: '16', share_count: '4' });
  try {
    await page.goto('/subscriptions');
    const row = page.locator('tbody tr', { hasText: subscription.name });
    await expect(row.getByText(/split 4 ways/)).toBeVisible();
    await expect(row.getByText(/your share \$4\.00/)).toBeVisible();
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});

test('share_count field present in form, defaults to 1', async ({ page }) => {
  await page.goto('/form/subscription');
  await expect(page.locator('input[name="share_count"]')).toHaveValue('1');
});
