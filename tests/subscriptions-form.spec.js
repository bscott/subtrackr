// @ts-check
// The subscription modal (templates/subscription-form.html): create, edit, cancel,
// delete with hx-confirm, client-side validation, server errors and reload-on-success.
const { test, expect } = require('@playwright/test');
const {
  anyLocale, uniqueName, createCategory, seed, findSubscription, row, modal, form,
  gotoSubscriptions, openAddForm, openEditForm, markDocument, expectReloaded, expectNotReloaded,
  submitAndWaitForReload,
} = require('./helpers/subscription-ui');

/** Records every create/update/delete request the page sends. */
function trackWrites(page) {
  const writes = [];
  page.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/subscriptions') && request.method() !== 'GET') {
      writes.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  });
  return writes;
}

const dateOnly = value => (value ? value.slice(0, 10) : value);

test.describe('subscription form', () => {
  test('creating with every field saves them and reloads into the list', async ({ page, request }) => {
    const name = uniqueName('Create Full');
    const category = await createCategory(request);
    await gotoSubscriptions(page);
    await openAddForm(page);

    const f = form(page);
    await f.locator('#name').fill(name);
    await f.locator('#label').fill('Family plan');
    await f.locator('#tags').fill('streaming, shared ');
    await f.locator('#category_id').selectOption(String(category.id));
    await f.locator('#cost').fill('12.34');
    await f.locator('#share_count').fill('2');
    await f.locator('#original_currency').selectOption('EUR');
    await expect(f.locator('#cost-currency-symbol')).toHaveText('€');
    await f.locator('#schedule_combo').selectOption('Annual_1');
    await f.locator('#status').selectOption('Trial');
    await f.locator('#payment_method').fill('Visa 4242');
    await f.locator('#account').fill('me@example.com');
    await f.locator('#start_date').fill('2030-01-15');
    await f.locator('#renewal_date').fill('2031-01-15');
    await f.locator('#cancellation_date').fill('2031-01-10');
    await f.locator('#cancellation_notice_days').fill('7');
    await f.locator('#usage').selectOption('Low');
    await f.locator('#notes').fill('Line one\nLine two');
    await f.locator('#autopay').check();
    await expect(f.locator('input[type="checkbox"][name="reminder_enabled"]')).toBeChecked();

    await submitAndWaitForReload(page);
    await expect(row(page, name)).toBeVisible();
    await expect(row(page, name)).toContainText('Family plan');
    await expect(row(page, name)).toContainText('#streaming');
    await expect(row(page, name).locator('[data-autopay-state="true"]')).toBeVisible();

    const saved = await findSubscription(request, name);
    expect(saved).toMatchObject({
      label: 'Family plan', category_id: category.id, cost: 12.34, share_count: 2, original_currency: 'EUR',
      schedule: 'Annual', schedule_interval: 1, status: 'Trial', payment_method: 'Visa 4242',
      account: 'me@example.com', cancellation_notice_days: 7, usage: 'Low', notes: 'Line one\nLine two',
      autopay: true, reminder_enabled: true,
    });
    expect(dateOnly(saved.start_date)).toBe('2030-01-15');
    expect(dateOnly(saved.renewal_date)).toBe('2031-01-15');
    expect(dateOnly(saved.cancellation_date)).toBe('2031-01-10');
    expect(saved.tags.map(tag => tag.name).sort()).toEqual(['shared', 'streaming']);
  });

  test('unchecking reminders on create saves them disabled', async ({ page, request }) => {
    // APP BUG (Go, unrelated to HTMX): the form posts reminder_enabled=false, but
    // SubscriptionRepository.Create uses GORM Create and the `default:true` tag on
    // Subscription.ReminderEnabled replaces the zero value, so reminders stay on.
    // Editing works (see the next test). Remove test.fail() once the server is fixed.
    test.fail();
    const name = uniqueName('Create No Reminder');
    const category = await createCategory(request);
    await gotoSubscriptions(page);
    await openAddForm(page);
    const f = form(page);
    await f.locator('#name').fill(name);
    await f.locator('#cost').fill('2');
    await f.locator('#category_id').selectOption(String(category.id));
    await f.locator('#schedule_combo').selectOption('Monthly_1');
    await f.locator('#status').selectOption('Active');
    await f.locator('input[type="checkbox"][name="reminder_enabled"]').uncheck();
    await submitAndWaitForReload(page);

    await expect(row(page, name)).toBeVisible();
    expect((await findSubscription(request, name)).reminder_enabled).toBe(false);
  });

  test('toggling reminders and autopay on edit is saved', async ({ page, request }) => {
    const name = uniqueName('Edit Toggles');
    await seed(request, { name, autopay: 'true' });
    await gotoSubscriptions(page);
    await expect(row(page, name).getByTitle(anyLocale('common.reminders_disabled'))).toHaveCount(0);

    await openEditForm(page, name);
    await form(page).locator('input[type="checkbox"][name="reminder_enabled"]').uncheck();
    await form(page).locator('#autopay').uncheck();
    await submitAndWaitForReload(page);

    await expect(row(page, name).getByTitle(anyLocale('common.reminders_disabled'))).toBeVisible();
    await expect(row(page, name).locator('[data-autopay-state="false"]')).toBeVisible();
    expect(await findSubscription(request, name)).toMatchObject({ reminder_enabled: false, autopay: false });
  });

  test('a category added inline is selected and used', async ({ page, request }) => {
    const name = uniqueName('Inline Cat');
    const categoryName = uniqueName('Inline Category');
    await gotoSubscriptions(page);
    await openAddForm(page);

    const f = form(page);
    await page.locator('#add-category-btn').click();
    await page.locator('#new-category-name').fill(categoryName);
    await page.locator('#new-category-name').press('Enter');
    await expect(page.locator('#new-category-container')).toBeHidden();
    await expect(f.locator('#category_id option:checked')).toHaveText(categoryName);

    await f.locator('#name').fill(name);
    await f.locator('#cost').fill('3');
    await f.locator('#schedule_combo').selectOption('Monthly_1');
    await f.locator('#status').selectOption('Active');
    await submitAndWaitForReload(page);

    await expect(row(page, name)).toContainText(categoryName);
    expect((await findSubscription(request, name)).category.name).toBe(categoryName);
  });

  test('edit form is pre-filled and saving updates only what changed', async ({ page, request }) => {
    const name = uniqueName('Edit Prefill');
    const renamed = `${name} renamed`;
    const created = await seed(request, {
      name, label: 'Old label', cost: '7.5', schedule: 'Weekly', schedule_interval: '2', status: 'Paused',
      payment_method: 'Amex', account: 'acct-1', start_date: '2030-02-01', renewal_date: '2030-03-01',
      cancellation_notice_days: '3', usage: 'High', notes: 'keep me', autopay: 'false', share_count: '3',
      tags: 'one, two',
    });
    await gotoSubscriptions(page);
    await openEditForm(page, name);

    const f = form(page);
    await expect(f.locator('#label')).toHaveValue('Old label');
    await expect(f.locator('#tags')).toHaveValue(/one.*two/);
    await expect(f.locator('#category_id')).toHaveValue(String(created.category_id));
    await expect(f.locator('#cost')).toHaveValue('7.5');
    await expect(f.locator('#share_count')).toHaveValue('3');
    await expect(f.locator('#original_currency')).toHaveValue('USD');
    await expect(f.locator('#schedule_combo')).toHaveValue('Weekly_2');
    await expect(f.locator('#status')).toHaveValue('Paused');
    await expect(f.locator('#payment_method')).toHaveValue('Amex');
    await expect(f.locator('#account')).toHaveValue('acct-1');
    await expect(f.locator('#start_date')).toHaveValue('2030-02-01');
    await expect(f.locator('#renewal_date')).toHaveValue('2030-03-01');
    await expect(f.locator('#cancellation_notice_days')).toHaveValue('3');
    await expect(f.locator('#usage')).toHaveValue('High');
    await expect(f.locator('#notes')).toHaveValue('keep me');
    await expect(f.locator('#autopay')).not.toBeChecked();
    await expect(f.locator('input[type="checkbox"][name="reminder_enabled"]')).toBeChecked();

    await f.locator('#name').fill(renamed);
    await f.locator('#cost').fill('8.25');
    await f.locator('#status').selectOption('Active');
    await f.locator('#renewal_date').fill('2030-04-01');
    await submitAndWaitForReload(page);

    await expect(row(page, renamed)).toBeVisible();
    await expect(row(page, name)).toHaveCount(0);
    const saved = await findSubscription(request, renamed);
    expect(saved).toMatchObject({
      id: created.id, label: 'Old label', cost: 8.25, status: 'Active', schedule: 'Weekly', schedule_interval: 2,
      payment_method: 'Amex', account: 'acct-1', notes: 'keep me', usage: 'High', share_count: 3,
      cancellation_notice_days: 3, autopay: false,
    });
    expect(dateOnly(saved.renewal_date)).toBe('2030-04-01');
    expect(saved.tags.map(tag => tag.name).sort()).toEqual(['one', 'two']);
  });

  test('cancel and clicking outside close the modal without saving', async ({ page, request }) => {
    const name = uniqueName('Edit Cancel');
    await seed(request, { name, cost: '5' });
    await gotoSubscriptions(page);
    const writes = trackWrites(page);

    await openEditForm(page, name);
    await form(page).locator('#name').fill(`${name} discarded`);
    await form(page).locator('button[type="button"]:visible', { hasText: anyLocale('common.cancel') }).click();
    await expect(modal(page)).toBeHidden();

    await openEditForm(page, name);
    await form(page).locator('#cost').fill('99');
    await page.mouse.click(5, 5); // the backdrop around the dialog
    await expect(modal(page)).toBeHidden();

    // Reopening loads the saved values again.
    await openEditForm(page, name);
    await expect(form(page).locator('#cost')).toHaveValue('5');
    expect(writes).toEqual([]);
    expect((await findSubscription(request, name)).cost).toBe(5);
    await expect(row(page, name)).toBeVisible();
  });

  test('delete asks for confirmation and removes the subscription when accepted', async ({ page, request }) => {
    const name = uniqueName('Delete Accept');
    const created = await seed(request, { name });
    await gotoSubscriptions(page);

    /** @type {string[]} */
    const dialogs = [];
    page.once('dialog', dialog => {
      dialogs.push(dialog.type(), dialog.message());
      dialog.accept();
    });
    await markDocument(page);
    await row(page, name).getByTitle(anyLocale('common.delete')).click();
    await expectReloaded(page);

    expect(dialogs[0]).toBe('confirm');
    expect(dialogs[1]).toMatch(anyLocale('subscriptions.delete_confirm'));
    await expect(row(page, name)).toHaveCount(0);
    expect((await request.get(`/api/subscriptions/${created.id}`)).status()).toBe(404);
  });

  test('dismissing the delete confirmation keeps the subscription', async ({ page, request }) => {
    const name = uniqueName('Delete Dismiss');
    const created = await seed(request, { name });
    await gotoSubscriptions(page);
    const writes = trackWrites(page);

    const dialogShown = page.waitForEvent('dialog');
    page.once('dialog', dialog => dialog.dismiss());
    await markDocument(page);
    await row(page, name).getByTitle(anyLocale('common.delete')).click();
    expect((await dialogShown).type()).toBe('confirm');
    await page.waitForTimeout(300);

    await expectNotReloaded(page);
    expect(writes).toEqual([]);
    await expect(row(page, name)).toBeVisible();
    expect((await request.get(`/api/subscriptions/${created.id}`)).ok()).toBeTruthy();
  });

  test('required fields and a negative cost block submission', async ({ page, request }) => {
    const name = uniqueName('Validation');
    const category = await createCategory(request);
    await gotoSubscriptions(page);
    await openAddForm(page);
    const writes = trackWrites(page);
    const f = form(page);
    const submit = f.locator('button[type="submit"]');
    const isValid = selector => f.locator(selector).evaluate(el => /** @type {HTMLInputElement} */ (el).validity.valid);

    await submit.click();
    expect(await isValid('#name')).toBe(false);

    await f.locator('#name').fill(name);
    await f.locator('#cost').fill('-1');
    await f.locator('#category_id').selectOption(String(category.id));
    await f.locator('#status').selectOption('Active');
    await submit.click();
    expect(await isValid('#cost')).toBe(false);
    expect(await isValid('#schedule_combo')).toBe(false);

    await f.locator('#cost').fill('4');
    await submit.click();
    expect(await isValid('#schedule_combo')).toBe(false);

    await page.waitForTimeout(300);
    expect(writes).toEqual([]);
    await expect(modal(page)).toBeVisible();
    expect(await findSubscription(request, name)).toBeUndefined();

    await f.locator('#schedule_combo').selectOption('Monthly_1');
    await submitAndWaitForReload(page);
    await expect(row(page, name)).toBeVisible();
  });
});

test.describe('subscription form server errors', () => {
  // When a save fails, CreateSubscription/UpdateSubscription answer an HTMX request with
  // 400, `HX-Retarget: #form-errors` and templates/form-errors.html. A real failure cannot
  // be triggered reliably: the only one reachable from the form is a deleted category, and
  // SQLite enforces that foreign key on only one pooled connection (PRAGMA foreign_keys is
  // run once in internal/database/database.go). So the submit is answered with that exact
  // response instead, and these tests check what the user sees.
  const serverError = 'FOREIGN KEY constraint failed';

  async function failNextSave(page) {
    await page.route(url => url.pathname.startsWith('/api/subscriptions'), async route => {
      if (route.request().method() === 'GET') return route.fallback();
      await route.fulfill({
        status: 400,
        headers: { 'Content-Type': 'text/html; charset=utf-8', 'HX-Retarget': '#form-errors' },
        body: `<div class="bg-red-50 border border-red-200 rounded-lg p-4 mb-4"><div class="flex"><div>
<h3 class="text-sm font-medium text-red-800">Error</h3>
<p class="text-sm text-red-700 mt-1">${serverError}</p></div></div></div>`,
      });
    });
  }

  async function openFilledCreate(page, request) {
    const name = uniqueName('Create Error');
    const category = await createCategory(request);
    await gotoSubscriptions(page);
    await openAddForm(page);
    const f = form(page);
    await f.locator('#name').fill(name);
    await f.locator('#cost').fill('6.5');
    await f.locator('#category_id').selectOption(String(category.id));
    await f.locator('#schedule_combo').selectOption('Monthly_1');
    await f.locator('#status').selectOption('Active');
    return name;
  }

  async function openChangedEdit(page, request) {
    const name = uniqueName('Edit Error');
    await seed(request, { name, cost: '2' });
    await gotoSubscriptions(page);
    await openEditForm(page, name);
    await form(page).locator('#cost').fill('3');
    return name;
  }

  async function submitFailing(page) {
    await failNextSave(page);
    const response = page.waitForResponse(r => new URL(r.url()).pathname.startsWith('/api/subscriptions')
      && r.request().method() !== 'GET');
    await markDocument(page);
    await form(page).locator('button[type="submit"]').click();
    expect((await response).status()).toBe(400);
    await page.waitForTimeout(300);
  }

  test('a failed create keeps the modal open with the entered values', async ({ page, request }) => {
    const name = await openFilledCreate(page, request);
    await submitFailing(page);

    await expectNotReloaded(page);
    await expect(modal(page)).toBeVisible();
    await expect(form(page).locator('#name')).toHaveValue(name);
    await expect(form(page).locator('#cost')).toHaveValue('6.5');
    expect(await findSubscription(request, name)).toBeUndefined();
  });

  test('a failed create shows the server error in the form', async ({ page, request }) => {
    // BUG with HTMX 1.9.10: 4xx responses are not swapped, so the error fragment is
    // dropped and the user sees nothing happen. HTMX 4 swaps 4xx/5xx by default, so this
    // should start passing after the upgrade; then remove test.fail().
    test.fail();
    await openFilledCreate(page, request);
    await submitFailing(page);
    await expect(page.locator('#form-errors')).toContainText(serverError, { timeout: 2000 });
    await expect(modal(page)).toBeVisible();
  });

  test('a failed edit keeps the modal open and the saved subscription unchanged', async ({ page, request }) => {
    const name = await openChangedEdit(page, request);
    await submitFailing(page);

    await expectNotReloaded(page);
    await expect(modal(page)).toBeVisible();
    await expect(form(page).locator('#cost')).toHaveValue('3');
    expect((await findSubscription(request, name)).cost).toBe(2);
  });

  test('a failed edit shows the server error in the form', async ({ page, request }) => {
    // BUG with HTMX 1.9.10: see "a failed create shows the server error in the form".
    test.fail();
    await openChangedEdit(page, request);
    await submitFailing(page);
    await expect(page.locator('#form-errors')).toContainText(serverError, { timeout: 2000 });
    await expect(modal(page)).toBeVisible();
  });
});
