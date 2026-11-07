// Keyboard operation and dialog semantics of the subscription form modal, the
// list sort controls and the theme picker. Runs in the desktop projects
// against the shared server; it only creates and deletes its own data.
//
const { test, expect } = require('@playwright/test');
const { uniqueName, findSubscription, seed, createCategory, gotoSubscriptions, openAddForm, openEditForm, modal, form, row, expectSortSpec } = require('./helpers/subscription-ui');
const { activeElement } = require('./helpers/ux');

test.describe('subscription form modal', () => {
  test('Escape closes the form', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    await page.keyboard.press('Escape');
    await expect(modal(page)).toBeHidden();
  });

  test('the Cancel button and the close icon close the form', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    await form(page).getByRole('button', { name: 'Cancel' }).click();
    await expect(modal(page)).toBeHidden();

    await openAddForm(page);
    await page.locator('#modal-content > div > div:first-child button').click();
    await expect(modal(page)).toBeHidden();
  });

  test('Enter in a text field submits the form', async ({ page, request }) => {
    const name = uniqueName('Keyboard add');
    // Create a category to exercise selection as well as the uncategorized default.
    const category = await createCategory(request);
    await gotoSubscriptions(page);
    await openAddForm(page);
    await form(page).locator('input[name="name"]').fill(name);
    await form(page).locator('input[name="cost"]').fill('3');
    await form(page).locator('select[name="category_id"]').selectOption(String(category.id));
    await form(page).locator('#schedule_combo').selectOption('Monthly_1');
    // Select explicit status and currency values for this test.
    await form(page).locator('#status').selectOption('Active');
    await form(page).locator('#original_currency').selectOption('USD');
    try {
      await form(page).locator('input[name="cost"]').press('Enter');
      await expect.poll(async () => (await findSubscription(request, name))?.cost, { timeout: 15_000 }).toBe(3);
    } finally {
      const saved = await findSubscription(request, name);
      if (saved) await request.delete(`/api/subscriptions/${saved.id}`);
      await request.delete(`/api/categories/${category.id}`);
    }
  });

  test('the category picker opens on Uncategorized, saves without a category, and can clear one on edit', async ({ page, request }) => {
    const name = uniqueName('Keyboard uncategorized');
    const category = await createCategory(request);
    let created;
    try {
      await gotoSubscriptions(page);
      await openAddForm(page);
      // No category is a supported state, so the picker needs no choice.
      await expect(form(page).locator('select[name="category_id"]')).toHaveValue('0');
      await expect(form(page).locator('select[name="category_id"] option[value="0"]')).toHaveText(/Uncategorized/);
      await form(page).locator('input[name="name"]').fill(name);
      await form(page).locator('input[name="cost"]').fill('6');
      await form(page).locator('#schedule_combo').selectOption('Monthly_1');
      await form(page).locator('#original_currency').selectOption('USD');
      await form(page).locator('input[name="cost"]').press('Enter');
      await expect.poll(async () => (await findSubscription(request, name))?.category_id, { timeout: 15_000 }).toBe(0);
      created = await findSubscription(request, name);
      await page.waitForLoadState('networkidle');
      await expect(row(page, name)).toContainText('Uncategorized');

      // Give it a category, then take it away again through the form.
      await openEditForm(page, name);
      await form(page).locator('select[name="category_id"]').selectOption(String(category.id));
      await form(page).locator('button[type="submit"]').click();
      await expect.poll(async () => (await findSubscription(request, name))?.category_id, { timeout: 15_000 }).toBe(category.id);
      await page.waitForLoadState('networkidle');
      await expect(row(page, name)).toContainText(category.name);

      await openEditForm(page, name);
      await expect(form(page).locator('select[name="category_id"]')).toHaveValue(String(category.id));
      await form(page).locator('select[name="category_id"]').selectOption('0');
      await form(page).locator('button[type="submit"]').click();
      await expect.poll(async () => (await findSubscription(request, name))?.category_id, { timeout: 15_000 }).toBe(0);
      await page.waitForLoadState('networkidle');
      await expect(row(page, name)).toContainText('Uncategorized');
    } finally {
      const saved = created || await findSubscription(request, name);
      if (saved) await request.delete(`/api/subscriptions/${saved.id}`);
      await request.delete(`/api/categories/${category.id}`);
    }
  });

  test('Tab walks the fields in visual order and Save can take focus', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    await form(page).locator('input[name="name"]').focus();
    const visited = [];
    // WebKit splits each date input into three focusable parts and, like Safari,
    // skips buttons when tabbing, so walk until the last field (notes) is reached.
    for (let i = 0; i < 80; i++) {
      await page.keyboard.press('Tab');
      const el = await activeElement(page);
      visited.push(el.name || el.id || el.text);
      if (el.name === 'notes') break;
    }
    const order = ['label', 'category_id', 'cost', 'original_currency', 'schedule_combo', 'status', 'start_date', 'renewal_date', 'notes'];
    const positions = order.map((name) => visited.indexOf(name));
    expect(positions, `fields visited in order; visited: ${JSON.stringify(visited)}`).toEqual([...positions].sort((a, b) => a - b));
    expect(positions.every((p) => p >= 0), `every field was reached; visited: ${JSON.stringify(visited)}`).toBe(true);
    await form(page).locator('button[type="submit"]').focus();
    expect((await activeElement(page)).tag).toBe('BUTTON');
  });

  test('the edit form opens from the row action with the row data', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Keyboard edit'), cost: '7' });
    try {
      await gotoSubscriptions(page);
      await openEditForm(page, sub.name);
      await expect(form(page).locator('input[name="cost"]')).toHaveValue('7');
      await page.keyboard.press('Escape');
      await expect(modal(page)).toBeHidden();
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });

  test('clicking the dimmed backdrop closes the form', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    await modal(page).click({ position: { x: 5, y: 5 } });
    await expect(modal(page)).toBeHidden();
  });

  test('focus moves into the form when it opens and returns to the opener when it closes', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    // The form arrives by htmx after the dialog opens; focus follows it in.
    await expect.poll(() => page.evaluate(() => document.getElementById('modal').contains(document.activeElement)),
      { message: 'focus is inside the modal after opening' }).toBe(true);
    expect((await activeElement(page)).name, 'the first field takes focus').toBe('name');
    await page.keyboard.press('Escape');
    await expect(modal(page)).toBeHidden();
    const opener = await activeElement(page);
    expect(opener.text, 'focus returned to the Add Subscription button').toMatch(/Add Subscription/);
  });

  test('Tab stays inside the open form', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    await form(page).locator('button[type="submit"]').focus();
    const inside = () => page.evaluate(() => document.getElementById('modal').contains(document.activeElement));
    // Firefox makes the scrollable modal panel itself focusable, so one Tab can
    // land there before leaving; the trap must hold for several presses.
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('Tab');
      expect(await inside(), `focus still inside the modal after ${i + 1} Tab(s) past Save`).toBe(true);
    }
    // And backwards from the first control.
    await form(page).locator('input[name="name"]').focus();
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('Shift+Tab');
      expect(await inside(), `focus still inside the modal after ${i + 1} Shift+Tab(s) before the first field`).toBe(true);
    }
  });

  test('the modal is announced as a dialog with a name and a labelled close button', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-modal', 'true');
    await expect(dialog).toHaveAccessibleName(/Add Subscription/);
    const close = dialog.getByRole('button', { name: /^close$/i });
    await expect(close).toBeVisible();
    await close.click();
    await expect(modal(page)).toBeHidden();
  });
});

test.describe('list sort controls', () => {
  test('sort headers are buttons that respond to Enter and Space and expose pressed state', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Keyboard sort'), cost: '2' });
    try {
      await gotoSubscriptions(page, '?sort=name:asc');
      const costHeader = page.locator('#subscription-list [data-sort-field="cost"]');
      await expect(costHeader).toHaveAttribute('aria-pressed', 'false');
      await costHeader.focus();
      await page.keyboard.press('Enter');
      await expectSortSpec(page, 'cost:asc');
      await expect(page.locator('#subscription-list [data-sort-field="cost"]')).toHaveAttribute('aria-pressed', 'true');
      await page.locator('#subscription-list [data-sort-field="cost"]').focus();
      await page.keyboard.press('Space');
      await expectSortSpec(page, 'cost:desc');
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });
});

test.describe('theme picker', () => {
  test('theme options can be reached and chosen with the keyboard', async ({ page, browserName }) => {
    // Only inspects focus and semantics; it does not change the saved theme
    // (themes.spec.js covers choosing, on a private server).
    await page.goto('/settings');
    const option = page.locator('.theme-option[data-theme="ocean"]');
    await expect(option).toBeVisible();
    await option.focus();
    const focused = await page.evaluate(() => document.activeElement?.getAttribute('data-theme'));
    expect(focused, 'theme option takes focus').toBe('ocean');
    await expect(option).toHaveRole('button');
    await expect(option).toHaveAccessibleName(/Ocean/);
    await expect(option).toHaveAttribute('aria-pressed', /true|false/);
    // Every card is a real button, so tabbing walks through all of them.
    // WebKit follows Safari's convention of skipping buttons on plain Tab;
    // Option+Tab is how Safari users reach them.
    const tab = browserName === 'webkit' ? 'Alt+Tab' : 'Tab';
    await expect(page.locator('.theme-option')).toHaveCount(6);
    await page.locator('.theme-option').first().focus();
    const seen = [];
    for (let i = 0; i < 6; i++) {
      seen.push(await page.evaluate(() => document.activeElement?.getAttribute('data-theme')));
      await page.keyboard.press(tab);
    }
    expect(seen).toEqual(['default', 'dark', 'dark-classic', 'christmas', 'midnight', 'ocean']);
  });
});
