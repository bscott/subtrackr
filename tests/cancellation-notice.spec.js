// @ts-check
// Covers issue #132 (per-subscription cancellation notice period).
const { test, expect } = require('@playwright/test');

function formatDateInput(date) {
  return date.toISOString().slice(0, 10);
}

test('cancellation notice period round-trips through form, API, CSV, and list badge', async ({ page, request }) => {
  const categoriesResponse = await request.get('/api/categories');
  expect(categoriesResponse.ok()).toBeTruthy();
  let categories = await categoriesResponse.json();
  if (categories.length === 0) {
    const categoryResponse = await request.post('/api/categories', {
      data: { name: `Notice Test ${Date.now()}` },
    });
    expect(categoryResponse.ok()).toBeTruthy();
    categories = [await categoryResponse.json()];
  }

  const suffix = Date.now();
  const name = `Gym Contract ${suffix}`;
  const renewal = new Date();
  renewal.setDate(renewal.getDate() + 60);
  const createdIDs = [];

  try {
    const response = await request.post('/api/subscriptions', {
      form: {
        name,
        cost: '39.99',
        schedule: 'Annual',
        status: 'Active',
        original_currency: 'USD',
        category_id: String(categories[0].id),
        renewal_date: formatDateInput(renewal),
        cancellation_notice_days: '28',
      },
    });
    expect(response.ok()).toBeTruthy();
    const created = await response.json();
    expect(created.cancellation_notice_days).toBe(28);
    createdIDs.push(created.id);

    // CSV export carries the notice period
    const exportResponse = await request.get('/api/export/csv');
    expect(exportResponse.ok()).toBeTruthy();
    const csvLines = (await exportResponse.text()).trim().split('\n');
    expect(csvLines[0]).toContain('Cancellation Notice Days');
    const line = csvLines.find(candidate => candidate.includes(name));
    expect(line).toBeDefined();
    expect(line).toContain(',28,');

    // List shows the cancel-by badge for the subscription
    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');
    const row = page.locator('tbody tr', { hasText: name });
    await expect(row).toBeVisible();
    await expect(row.locator('[data-cancel-by]')).toBeVisible();

    // Edit form is pre-filled and can update the value
    await row.locator('button[hx-get^="/form/subscription/"]').click();
    const noticeInput = page.locator('#cancellation_notice_days');
    await expect(noticeInput).toBeVisible();
    await expect(noticeInput).toHaveValue('28');
    await noticeInput.fill('14');
    const refresh = page.waitForEvent('load');
    await page.locator('#modal-content form button[type="submit"]').click();
    await refresh;

    const updatedResponse = await request.get(`/api/subscriptions/${created.id}`);
    expect(updatedResponse.ok()).toBeTruthy();
    expect((await updatedResponse.json()).cancellation_notice_days).toBe(14);

    // Clearing the notice period removes the badge
    const clearResponse = await request.put(`/api/subscriptions/${created.id}`, {
      form: { cancellation_notice_days: '0' },
    });
    expect(clearResponse.ok()).toBeTruthy();
    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');
    const clearedRow = page.locator('tbody tr', { hasText: name });
    await expect(clearedRow).toBeVisible();
    await expect(clearedRow.locator('[data-cancel-by]')).toHaveCount(0);
  } finally {
    await Promise.all(createdIDs.map(id => request.delete(`/api/subscriptions/${id}`)));
  }
});
