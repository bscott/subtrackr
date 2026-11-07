const { test, expect } = require('./helpers/settings-server');
const { openAddForm } = require('./helpers/subscription-ui');

test('blurring a scrolled form retains its scroll and focuses the dialog', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 700 });
  await page.goto('/subscriptions');
  await openAddForm(page);
  const before = await page.locator('#modal').evaluate(modal => {
    const input = modal.querySelector('#notes');
    input.focus({ preventScroll: true });
    const panel = modal.querySelector('.overflow-y-auto');
    panel.scrollTop = 500;
    input.blur();
    return panel.scrollTop;
  });
  expect(before).toBeGreaterThan(0);
  await expect(page.locator('#modal')).toBeFocused();
  expect(await page.locator('#modal .overflow-y-auto').evaluate(el => el.scrollTop)).toBe(before);
});

test('inline category create and cancel return focus to useful controls', async ({ page }) => {
  await page.goto('/subscriptions');
  await openAddForm(page);
  await page.locator('#add-category-btn').click();
  await page.locator('#new-category-name').fill(`Focus category ${Date.now()}`);
  await page.locator('#new-category-name').press('Enter');
  await expect(page.locator('#category_id')).toBeFocused();
  await expect(page.locator('#modal')).toBeVisible();
  await page.locator('#add-category-btn').click();
  await page.locator('#new-category-container button[onclick="hideNewCategoryInput()"]').click();
  await expect(page.locator('#add-category-btn')).toBeFocused();
});

test('failed theme save shows actionable feedback', async ({ page }) => {
  await page.goto('/settings');
  await page.route('**/api/settings/theme', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"internal"}' }) : route.continue());
  await page.locator('.theme-option[data-theme="ocean"]').click();
  await expect(page.getByRole('alert')).toContainText('Unable to save your theme');
});

test('a delayed saved-theme read cannot override a new choice', async ({ page }) => {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  let seen;
  const requested = new Promise(resolve => { seen = resolve; });
  await page.route('**/api/settings/theme', async route => {
    if (route.request().method() !== 'GET') return route.continue();
    seen();
    await gate;
    await route.fulfill({ json: { theme: 'midnight', saved: true } });
  });
  await page.goto('/settings', { waitUntil: 'domcontentloaded' });
  await requested;
  await page.locator('.theme-option[data-theme="ocean"]').click();
  release();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'ocean');
});
