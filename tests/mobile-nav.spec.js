// Mobile navigation and form layout. Runs in the mobile-chromium (Pixel 7) and
// mobile-webkit (iPhone 13) projects only; see playwright.config.js.
//
// These specs only read pages and create/delete their own subscription, so
// they share the main test server with the desktop projects.
const { test, expect } = require('@playwright/test');
const { uniqueName, findSubscription, createCategory, seed } = require('./helpers/subscription-ui');
const { isoDate, daysFromNow } = require('./helpers/pages');
const { saveScreenshot, collectConsole, expectNoHorizontalOverflow } = require('./helpers/ux');

const PAGES = ['/', '/subscriptions', '/analytics', '/calendar', '/settings'];

const menu = (page) => page.locator('#mobile-menu');
const hamburger = (page) => page.locator('#mobile-menu-button');

async function openMenu(page) {
  await hamburger(page).click();
  await expect(menu(page)).toBeVisible();
}

test('the hamburger replaces the desktop navigation on small screens', async ({ page }, testInfo) => {
  await page.goto('/');
  await expect(hamburger(page)).toBeVisible();
  await expect(page.locator('header nav')).toBeHidden();
  await expect(menu(page)).toBeHidden();
  // The desktop "Add" button is hidden; only the settings gear stays in the header.
  await expect(page.locator('header button[hx-get="/form/subscription"]')).toBeHidden();
  await expect(page.locator('header a[href="/settings"]:visible')).toHaveCount(1);
  await saveScreenshot(page, `mobile-${testInfo.project.name}-dashboard`);
});

test('the menu opens, locks page scroll, and closes on Escape, backdrop and the close button', async ({ page }) => {
  await page.goto('/');

  await openMenu(page);
  for (const label of ['Dashboard', 'Subscriptions', 'Analytics', 'Calendar']) {
    await expect(menu(page).getByRole('link', { name: label })).toBeVisible();
  }
  await expect(menu(page).getByRole('button', { name: 'Add Subscription' })).toBeVisible();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('hidden');

  await page.keyboard.press('Escape');
  await expect(menu(page)).toBeHidden();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('');

  await openMenu(page);
  await menu(page).getByRole('button', { name: 'Close menu' }).click();
  await expect(menu(page)).toBeHidden();

  await openMenu(page);
  // The panel is 16rem wide on the left; tap the backdrop to the right of it.
  const { width, height } = page.viewportSize();
  await page.mouse.click(width - 10, Math.round(height / 2));
  await expect(menu(page)).toBeHidden();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('');
});

test('menu links navigate and the next page starts with the menu closed and scrolling restored', async ({ page }, testInfo) => {
  await page.goto('/');
  await openMenu(page);
  await saveScreenshot(page, `mobile-${testInfo.project.name}-menu-open`, { fullPage: false });
  await menu(page).getByRole('link', { name: 'Subscriptions' }).click();
  await expect(page).toHaveURL(/\/subscriptions$/);
  await expect(menu(page)).toBeHidden();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('');

  await openMenu(page);
  await menu(page).getByRole('link', { name: 'Calendar' }).click();
  await expect(page).toHaveURL(/\/calendar$/);
  await expect(menu(page)).toBeHidden();
});

test('menu items are large enough to tap', async ({ page }) => {
  await page.goto('/');
  await openMenu(page);
  const boxes = await menu(page).locator('nav a, nav button').evaluateAll((els) =>
    els.map((el) => ({ text: el.textContent.trim(), height: el.getBoundingClientRect().height })));
  expect(boxes.length).toBeGreaterThanOrEqual(5);
  for (const box of boxes) {
    expect(box.height, `${box.text} tap target height`).toBeGreaterThanOrEqual(44);
  }
  const buttonBox = await hamburger(page).boundingBox();
  expect(buttonBox.width, 'hamburger tap target width').toBeGreaterThanOrEqual(40);
  expect(buttonBox.height, 'hamburger tap target height').toBeGreaterThanOrEqual(40);
});

test('Add Subscription from the menu opens a form that fits the screen and saves', async ({ page, request }, testInfo) => {
  const name = uniqueName('Mobile add');
  // Create a category to exercise selection as well as the uncategorized default.
  const category = await createCategory(request);
  const console = collectConsole(page);
  await page.goto('/subscriptions');
  await openMenu(page);
  await menu(page).getByRole('button', { name: 'Add Subscription' }).click();

  const modal = page.locator('#modal');
  await expect(menu(page)).toBeHidden();
  await expect(modal).toBeVisible();
  const nameInput = page.locator('#modal-content input[name="name"]');
  await expect(nameInput).toBeVisible();
  await expectNoHorizontalOverflow(page, 'subscription form on a phone');

  // The modal panel must not be wider than the screen, and the submit button must be reachable.
  const panel = page.locator('#modal > div');
  const panelBox = await panel.boundingBox();
  const { width } = page.viewportSize();
  expect(panelBox.width, 'modal panel width').toBeLessThanOrEqual(width);
  const submit = page.locator('#modal-content form button[type="submit"]');
  await submit.scrollIntoViewIfNeeded();
  await expect(submit).toBeVisible();
  await saveScreenshot(page, `mobile-${testInfo.project.name}-add-form`, { fullPage: false });

  await nameInput.fill(name);
  await page.locator('#modal-content input[name="cost"]').fill('4.50');
  await page.locator('#modal-content select[name="category_id"]').selectOption(String(category.id));
  await page.locator('#modal-content #schedule_combo').selectOption('Monthly_1');
  // Select explicit status and currency values for this test.
  await page.locator('#modal-content').locator('#status').selectOption('Active');
  await page.locator('#modal-content').locator('#original_currency').selectOption('USD');
  try {
    await submit.click();
    await expect.poll(async () => (await findSubscription(request, name))?.cost, { timeout: 15_000 }).toBe(4.5);
    await page.waitForLoadState('networkidle');
    await expect(page.locator(`#subscription-list tbody tr[data-search-name="${name}"]`)).toBeVisible();
    expect(console.pageErrors, 'uncaught errors while adding on mobile').toEqual([]);
  } finally {
    const saved = await findSubscription(request, name);
    if (saved) await request.delete(`/api/subscriptions/${saved.id}`);
    await request.delete(`/api/categories/${category.id}`);
  }
});

for (const path of PAGES) {
  test(`${path} fits a phone screen without sideways scrolling`, async ({ page }, testInfo) => {
    const console = collectConsole(page);
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await saveScreenshot(page, `mobile-${testInfo.project.name}-${path === '/' ? 'home' : path.slice(1)}`);
    await expectNoHorizontalOverflow(page, path);
    expect(console.pageErrors, `uncaught errors on ${path}`).toEqual([]);
  });
}

test('a very long subscription name wraps inside the dashboard card instead of widening the page', async ({ page, request }) => {
  // A name without spaces is the worst case: it cannot wrap at word boundaries.
  const name = `${uniqueName('Mobilelong')}-${'Supercalifragilistic'.repeat(3)}`;
  const category = await createCategory(request);
  let saved;
  try {
    const response = await request.post('/api/subscriptions', {
      form: { name, cost: '3', schedule: 'Monthly', status: 'Active', original_currency: 'USD', category_id: String(category.id) },
    });
    expect(response.ok(), await response.text()).toBe(true);
    saved = await findSubscription(request, name);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await expect(page.locator('main h3', { hasText: name })).toBeVisible();
    await expectNoHorizontalOverflow(page, '/ with a long subscription name');
  } finally {
    if (saved) await request.delete(`/api/subscriptions/${saved.id}`);
    await request.delete(`/api/categories/${category.id}`);
  }
});

test('the plan-to-cancel option fits a phone screen and its badge and warning show in the lists', async ({ page, request }, testInfo) => {
  // Renewal ahead, but the notice period puts the last day to cancel in the past.
  const sub = await seed(request, { name: uniqueName('Mobile plan cancel'), renewal_date: isoDate(daysFromNow(3)), cancellation_notice_days: '10', to_cancel: 'true' });
  try {
    await page.goto('/subscriptions');
    await openMenu(page);
    await menu(page).getByRole('button', { name: 'Add Subscription' }).click();
    const form = page.locator('#modal-content form');
    await expect(form.locator('input[name="name"]')).toBeVisible();
    const checkbox = form.getByLabel('I plan to cancel this subscription');
    await checkbox.scrollIntoViewIfNeeded();
    await checkbox.check();
    await expect(form.getByLabel('Remind me')).toBeVisible();
    await expectNoHorizontalOverflow(page, 'subscription form with the plan-to-cancel options open');
    await saveScreenshot(page, `mobile-${testInfo.project.name}-to-cancel-form`, { fullPage: false });
    await page.keyboard.press('Escape');

    const row = page.locator(`#subscription-list tbody tr[data-search-name="${sub.name}"]`);
    await expect(row.locator('[data-to-cancel]')).toBeVisible();
    await expect(row.locator('[data-to-cancel-missed]')).toBeAttached();
    await expectNoHorizontalOverflow(page, '/subscriptions with a plan-to-cancel badge');

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    const card = page.locator('main h3', { hasText: sub.name }).locator('..');
    await expect(card.locator('[data-to-cancel]')).toBeVisible();
    await expect(card.locator('[data-to-cancel-missed]')).toBeVisible();
    await expectNoHorizontalOverflow(page, '/ with a plan-to-cancel badge and warning');
  } finally {
    await request.delete(`/api/subscriptions/${sub.id}`);
  }
});

test('the subscriptions table scrolls inside its own container on a phone', async ({ page }) => {
  await page.goto('/subscriptions');
  await page.waitForLoadState('networkidle');
  // A wide table is acceptable only when its wrapper scrolls, not the page.
  const list = page.locator('#subscription-list');
  const overflow = await list.evaluate((el) => getComputedStyle(el).overflowX);
  expect(['auto', 'scroll']).toContain(overflow);
  await expectNoHorizontalOverflow(page, '/subscriptions');
});

test('the hamburger button tells assistive technology whether the menu is open', async ({ page }) => {
  await page.goto('/');
  await expect(hamburger(page)).toHaveAttribute('aria-expanded', 'false');
  await expect(hamburger(page)).toHaveAttribute('aria-controls', 'mobile-menu');
  await openMenu(page);
  await expect(hamburger(page)).toHaveAttribute('aria-expanded', 'true');
  const drawer = menu(page).locator('[role="dialog"]');
  await expect(drawer).toBeVisible();
  await expect(drawer).toHaveAttribute('aria-modal', 'true');
  await expect(drawer).toHaveAccessibleName(/menu/i);
  await page.keyboard.press('Escape');
  await expect(hamburger(page)).toHaveAttribute('aria-expanded', 'false');
});

test('opening the menu moves keyboard focus into it, Tab stays inside, and closing gives it back', async ({ page }) => {
  await page.goto('/');
  await openMenu(page);
  const focusedInsideMenu = () => page.evaluate(() => {
    const menu = document.getElementById('mobile-menu');
    return !!document.activeElement && menu.contains(document.activeElement);
  });
  expect(await focusedInsideMenu(), 'focus moved into the open menu').toBe(true);

  // More Tabs than the drawer has controls: focus must wrap, never reach the page behind.
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press('Tab');
    expect(await focusedInsideMenu(), `focus still inside the menu after ${i + 1} Tab(s)`).toBe(true);
  }
  await page.keyboard.press('Shift+Tab');
  expect(await focusedInsideMenu(), 'focus still inside the menu after Shift+Tab').toBe(true);

  await page.keyboard.press('Escape');
  await expect(menu(page)).toBeHidden();
  await expect(hamburger(page)).toBeFocused();
});
