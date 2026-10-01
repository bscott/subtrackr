// @ts-check
const { test, expect } = require('@playwright/test');
const { seedSubscription } = require('./helpers/subscriptions');

const fixtureTags = 'work, autopay, important';

test('tag chips render under the subscription name', async ({ page, request }) => {
  const subscription = await seedSubscription(request, { tags: fixtureTags });
  try {
    await page.goto('/subscriptions');
    const row = page.locator('tbody tr', { hasText: subscription.name });
    await expect(row.getByText('#work')).toBeVisible();
    await expect(row.getByText('#autopay')).toBeVisible();
    await expect(row.getByText('#important')).toBeVisible();
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});

test('tags input pre-fills in edit form', async ({ page, request }) => {
  const subscription = await seedSubscription(request, { tags: fixtureTags });
  try {
    await page.goto(`/form/subscription/${subscription.id}`);
    await expect(page.locator('input[name="tags"]')).toHaveValue(/work.*autopay.*important/);
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});
