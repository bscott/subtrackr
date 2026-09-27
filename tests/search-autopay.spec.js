// @ts-check
const { test, expect } = require('@playwright/test');

test('search survives sorting and autopay states remain distinct', async ({ page, request }) => {
  const categoriesResponse = await request.get('/api/categories');
  expect(categoriesResponse.ok()).toBeTruthy();
  let categories = await categoriesResponse.json();
  if (categories.length === 0) {
    const categoryResponse = await request.post('/api/categories', {
      data: { name: `Search Test ${Date.now()}` },
    });
    expect(categoryResponse.ok()).toBeTruthy();
    categories = [await categoryResponse.json()];
  }

  const suffix = Date.now();
  const subscriptions = [
    { name: `Automatic Search ${suffix}`, autopay: 'true', state: 'true' },
    { name: `Manual Search ${suffix}`, autopay: 'false', state: 'false' },
    { name: `Unknown Search ${suffix}`, state: 'unknown' },
  ];
  const createdIDs = [];

  try {
    for (const subscription of subscriptions) {
      /** @type {Record<string, string>} */
      const form = {
        name: subscription.name,
        cost: '9.99',
        schedule: 'Monthly',
        status: 'Active',
        original_currency: 'USD',
        category_id: String(categories[0].id),
      };
      if (subscription.autopay !== undefined) {
        form.autopay = subscription.autopay;
      }

      const response = await request.post('/api/subscriptions', { form });
      expect(response.ok()).toBeTruthy();
      const created = await response.json();
      const expectedAutopay = subscription.autopay === undefined ? null : subscription.autopay === 'true';
      expect(created.autopay).toBe(expectedAutopay);
      createdIDs.push(created.id);
    }

    const exportResponse = await request.get('/api/export/csv');
    expect(exportResponse.ok()).toBeTruthy();
    const csvLines = (await exportResponse.text()).trim().split('\n');
    expect(csvLines[0]).toContain('Autopay');
    for (const subscription of subscriptions) {
      const line = csvLines.find(candidate => candidate.includes(subscription.name));
      expect(line).toBeDefined();
      expect(line).toContain(`,${subscription.state === 'unknown' ? 'unknown' : subscription.state},`);
    }

    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');

    for (const subscription of subscriptions) {
      const row = page.locator('tbody tr', { hasText: subscription.name });
      await expect(row).toBeVisible();
      await expect(row.locator(`[data-autopay-state="${subscription.state}"]`)).toBeVisible();
    }

    const search = page.locator('#subscription-search');
    await search.fill(`  AUTOMATIC SEARCH ${suffix}  `);
    await expect(page.locator('tbody tr', { hasText: subscriptions[0].name })).toBeVisible();
    await expect(page.locator('tbody tr', { hasText: subscriptions[1].name })).toBeHidden();
    await expect(page.locator('tbody tr', { hasText: subscriptions[2].name })).toBeHidden();

    const sortResponse = page.waitForResponse(response => {
      const url = new URL(response.url());
      return url.pathname === '/api/subscriptions'
        && (url.searchParams.get('sorts') || '').startsWith('name:')
        && response.ok();
    });
    await page.locator('#subscription-list thead button', { hasText: 'Name' }).click();
    await sortResponse;

    await expect(search).toHaveValue(`  AUTOMATIC SEARCH ${suffix}  `);
    await expect(page.locator('tbody tr', { hasText: subscriptions[0].name })).toBeVisible();
    await expect(page.locator('tbody tr', { hasText: subscriptions[1].name })).toBeHidden();

    await search.fill(`No subscription ${suffix}`);
    await expect(page.locator('#subscription-search-empty')).toBeVisible();

    await search.fill('');
    for (const subscription of subscriptions) {
      await expect(page.locator('tbody tr', { hasText: subscription.name })).toBeVisible();
    }
  } finally {
    await Promise.all(createdIDs.map(id => request.delete(`/api/subscriptions/${id}`)));
  }
});
