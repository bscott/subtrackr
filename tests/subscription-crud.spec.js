// @ts-check
const { test, expect } = require('@playwright/test');
const { ensureCategory } = require('./helpers/subscriptions');

// These tests assume a clean-ish state with the dev DB (at least one category
// exists — the default Entertainment / Productivity etc. seeded categories).
// Each test creates a uniquely-named subscription so re-runs don't collide.

test.describe('Subscription CRUD Operations', () => {
  test('can create a new subscription', async ({ page, request }) => {
    const uniqueName = `CRUD Test ${Date.now()}`;
    const categoryID = await ensureCategory(request);

    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');

    // Open Add Subscription via the desktop nav (avoid matching mobile-menu duplicates)
    await page.locator('header button:has-text("Add"), header button:has-text("Añadir")').first().click();
    await page.waitForSelector('input[name="name"]', { timeout: 5000 });

    // Fill required fields
    await page.fill('input[name="name"]', uniqueName);
    await page.fill('input[name="cost"]', '9.99');

    // Pick the first non-empty category (any real category in the DB)
    await page.selectOption('select[name="category_id"]', String(categoryID));

    // Pick Monthly from the schedule combo (the form's onchange wires this into hidden schedule + schedule_interval)
    await page.selectOption('select#schedule_combo', 'Monthly_1');

    // Status
    await page.selectOption('select[name="status"]', 'Active');

    // Submit (button labelled "Add" in English or its translation)
    await page.locator('form button[type="submit"]').click();
    await page.waitForLoadState('networkidle');

    // The subscription should appear in the list
    await expect(page.getByText(uniqueName)).toBeVisible();
    await expect(page.getByText('$9.99').first()).toBeVisible();
  });

  test('can edit an existing subscription', async ({ page }) => {
    const baseName = `Edit Test ${Date.now()}`;
    const updatedName = `${baseName} Updated`;

    // Seed a subscription via the API so the test doesn't depend on previous test order
    const categoryID = await ensureCategory(page.request);

    await page.request.post('/api/subscriptions', {
      form: {
        name: baseName,
        cost: '4.99',
        schedule: 'Monthly',
        status: 'Active',
        original_currency: 'USD',
        category_id: String(categoryID),
      },
    });

    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');

    // Find the row whose name cell is exactly baseName and click its Edit button
    const row = page.locator('tr', { hasText: baseName }).first();
    await row.locator('button[title*="Edit"], button[title*="Editar"], button[title*="Bearbeiten"], button[title*="Bewerken"]').click();

    await page.waitForSelector('input[name="name"]', { timeout: 5000 });
    await page.fill('input[name="name"]', updatedName);
    await page.fill('input[name="cost"]', '14.99');

    await page.locator('form button[type="submit"]').click();
    await page.waitForLoadState('networkidle');

    await expect(page.getByText(updatedName)).toBeVisible();
    await expect(page.getByText('$14.99').first()).toBeVisible();
  });

  test('displays correct currency formatting', async ({ page }) => {
    await page.goto('/subscriptions');
    await page.waitForLoadState('networkidle');

    // All visible cost cells should match the $X.XX pattern (or a non-US currency symbol)
    const costCells = await page.locator('tbody td .text-sm.font-medium').all();
    expect(costCells.length).toBeGreaterThan(0);
    for (const cell of costCells) {
      const text = (await cell.textContent())?.trim() ?? '';
      if (/^[\$€£¥₹]/.test(text)) {
        expect(text).toMatch(/^[\$€£¥₹][\d,]+\.\d{2}$/);
      }
    }
  });
});
