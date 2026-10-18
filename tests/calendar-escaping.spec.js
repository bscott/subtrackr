// @ts-check
const { test, expect } = require('@playwright/test');
const { seedSubscription } = require('./helpers/subscriptions');

test('calendar shows subscription names as text, not markup', async ({ page, request }) => {
  const name = `<img src=x onerror="window.__calendarXSS=1"><b>bold</b> ${Date.now()}`;
  const renewal = new Date(Date.now() + 10 * 24 * 60 * 60 * 1000);
  const subscription = await seedSubscription(request, { name, renewal_date: renewal.toISOString().slice(0, 10) });
  try {
    await page.goto(`/calendar?year=${renewal.getUTCFullYear()}&month=${renewal.getUTCMonth() + 1}`);
    const event = page.locator('#calendar-grid button', { hasText: name });
    await expect(event).toBeVisible();
    await expect(event.locator('b')).toHaveCount(0);
    await expect(event).toHaveAttribute('title', new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} - `));
    expect(await page.evaluate(() => window.__calendarXSS)).toBeUndefined();
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});

test('calendar escapes subscription icon URLs', async ({ page, request }) => {
  const renewal = new Date(Date.now() + 10 * 24 * 60 * 60 * 1000);
  const subscription = await seedSubscription(request, {
    icon_url: '"><img src=x onerror="window.__iconXSS=1">',
    renewal_date: renewal.toISOString().slice(0, 10),
  });
  try {
    await page.goto(`/calendar?year=${renewal.getUTCFullYear()}&month=${renewal.getUTCMonth() + 1}`);
    const event = page.locator('#calendar-grid button', { hasText: subscription.name });
    await expect(event).toBeVisible();
    await expect(event.locator('img')).toHaveCount(1);
    expect(await page.evaluate(() => window.__iconXSS)).toBeUndefined();
  } finally {
    await request.delete(`/api/subscriptions/${subscription.id}`);
  }
});
