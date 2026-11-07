// Issue #137: "I plan to cancel this subscription" — a reminder of its own that
// counts down to the last day a subscription can still be cancelled (the
// cancel-by date when a notice period is set, otherwise the renewal date).
//
// Runs in the desktop projects against a private server; every test creates
// and deletes its own subscription and category. The CSV header itself is
// asserted in pages-export.spec.js and is not repeated here.
const { test, expect } = require('./helpers/settings-server');
const {
  uniqueName, findSubscription, createCategory, seed, gotoSubscriptions, openAddForm, openEditForm, modal, form, row,
  submitAndWaitForReload,
} = require('./helpers/subscription-ui');
const { isoDate, daysFromNow, csvRecords } = require('./helpers/pages');

const LEADS = [3, 5, 14];
const INVALID_LEAD_MESSAGE = /3, 5, or 14/;

const toCancel = (page) => form(page).locator('#to_cancel');
const lead = (page) => form(page).locator('#to_cancel_reminder_days');
const options = (page) => form(page).locator('#to-cancel-options');

async function current(request, name) {
  const sub = await findSubscription(request, name);
  expect(sub, `${name} exists on the server`).toBeTruthy();
  return sub;
}

async function cleanup(request, name, category) {
  const saved = await findSubscription(request, name);
  if (saved) await request.delete(`/api/subscriptions/${saved.id}`);
  if (category) await request.delete(`/api/categories/${category.id}`);
}

test.beforeEach(async ({ request }) => {
  const response = await request.post('/api/settings/date-format', { form: { date_format: 'YYYY-MM-DD' } });
  expect(response.ok()).toBeTruthy();
});

test('the reminder deadline follows the dotted date preference', async ({ page, request }) => {
  await request.post('/api/settings/date-format', { form: { date_format: 'DD.MM.YYYY' } });
  const renewal = isoDate(daysFromNow(40));
  const sub = await seed(request, { name: uniqueName('Dotted deadline'), renewal_date: renewal, to_cancel: 'true' });
  try {
    await gotoSubscriptions(page);
    await openEditForm(page, sub.name);
    await expect(form(page).locator('[data-to-cancel-deadline]')).toContainText(renewal.split('-').reverse().join('.'));
    await expect(form(page).locator('#renewal_date')).toHaveValue(renewal);
  } finally { await request.delete(`/api/subscriptions/${sub.id}`); }
});

test.describe('plan to cancel: form', () => {
  test('the add form offers it switched off, with labelled controls and a 3-day lead by default', async ({ page }) => {
    await gotoSubscriptions(page);
    await openAddForm(page);
    const checkbox = form(page).getByLabel('I plan to cancel this subscription');
    await expect(checkbox).toBeVisible();
    await expect(checkbox).not.toBeChecked();
    await expect(checkbox).toBeEnabled();
    await expect(form(page).locator('#to-cancel-help')).toContainText('even when regular reminders are off');
    await expect(options(page)).toBeHidden();

    await checkbox.check();
    await expect(options(page)).toBeVisible();
    const select = form(page).getByLabel('Remind me');
    await expect(select).toHaveValue('3');
    await expect(select.locator('option')).toHaveText(['3 days before the deadline', '5 days before the deadline', '14 days before the deadline']);

    // Independent of the regular reminder toggle.
    await form(page).locator('input[type="checkbox"][name="reminder_enabled"]').uncheck();
    await expect(checkbox).toBeChecked();
    await expect(options(page)).toBeVisible();

    await checkbox.uncheck();
    await expect(options(page)).toBeHidden();
  });

  for (const days of LEADS) {
    test(`creating with a ${days}-day lead round-trips through the API, the list badge and the edit form`, async ({ page, request }) => {
      const name = uniqueName(`Plan cancel ${days}`);
      const category = await createCategory(request);
      const renewal = isoDate(daysFromNow(40));
      try {
        await gotoSubscriptions(page);
        await openAddForm(page);
        await form(page).locator('input[name="name"]').fill(name);
        await form(page).locator('input[name="cost"]').fill('9');
        await form(page).locator('select[name="category_id"]').selectOption(String(category.id));
        await form(page).locator('#schedule_combo').selectOption('Monthly_1');
        await form(page).locator('#original_currency').selectOption('USD');
        await form(page).locator('input[name="renewal_date"]').fill(renewal);
        await toCancel(page).check();
        await lead(page).selectOption(String(days));
        await submitAndWaitForReload(page);

        const created = await current(request, name);
        expect(created.to_cancel).toBe(true);
        expect(created.to_cancel_reminder_days).toBe(days);
        expect(created.reminder_enabled, 'the regular reminder toggle is untouched').toBe(true);

        const badge = row(page, name).locator('[data-to-cancel]');
        await expect(badge).toHaveText('To cancel');
        await expect(badge).toHaveAttribute('title', 'You plan to cancel this subscription');
        await expect(row(page, name).locator('[data-to-cancel-missed]')).toHaveCount(0);

        await openEditForm(page, name);
        await expect(toCancel(page)).toBeChecked();
        await expect(lead(page)).toHaveValue(String(days));
        const deadline = form(page).locator('[data-to-cancel-deadline]');
        await expect(deadline).toContainText(`Cancel by ${renewal}`);
        await expect(deadline).toContainText('No notice period is set, so the last day to cancel is the renewal date.');
        await expect(form(page).locator('[data-to-cancel-missed]')).toHaveCount(0);
      } finally {
        await cleanup(request, name, category);
      }
    });
  }

  test('the edit form explains a deadline that comes from the notice period', async ({ page, request }) => {
    const renewal = daysFromNow(40);
    const sub = await seed(request, {
      name: uniqueName('Plan cancel notice'), renewal_date: isoDate(renewal), cancellation_notice_days: '10', to_cancel: 'true', to_cancel_reminder_days: '5',
    });
    try {
      await gotoSubscriptions(page);
      await expect(row(page, sub.name).locator('[data-cancel-by]')).toBeVisible();
      await expect(row(page, sub.name).locator('[data-to-cancel]')).toBeVisible();
      await openEditForm(page, sub.name);
      const deadline = form(page).locator('[data-to-cancel-deadline]');
      await expect(deadline).toContainText(`Cancel by ${isoDate(daysFromNow(30))}`);
      await expect(deadline).toContainText(`That is 10 days before the renewal on ${isoDate(renewal)}, because of the notice period.`);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });

  test('editing changes the lead, and unticking withdraws the plan', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel edit'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true', to_cancel_reminder_days: '5' });
    try {
      await gotoSubscriptions(page);
      await openEditForm(page, sub.name);
      await expect(lead(page)).toHaveValue('5');
      await lead(page).selectOption('14');
      await submitAndWaitForReload(page);
      expect((await current(request, sub.name)).to_cancel_reminder_days).toBe(14);

      await openEditForm(page, sub.name);
      await toCancel(page).uncheck();
      await expect(options(page)).toBeHidden();
      await submitAndWaitForReload(page);
      const after = await current(request, sub.name);
      expect(after.to_cancel).toBe(false);
      expect(after.to_cancel_reminder_days, 'the chosen lead is kept for next time').toBe(14);
      await expect(row(page, sub.name).locator('[data-to-cancel]')).toHaveCount(0);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });

  test('an unsupported lead is rejected in the open form and by the API', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel invalid'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true', to_cancel_reminder_days: '3' });
    const category = await createCategory(request);
    try {
      // In the browser, through htmx: the error lands in the form and the dialog stays open.
      await gotoSubscriptions(page);
      await openEditForm(page, sub.name);
      await lead(page).evaluate((select) => {
        const option = document.createElement('option');
        option.value = '7';
        option.textContent = '7 days';
        select.appendChild(option);
        select.value = '7';
      });
      await form(page).locator('button[type="submit"]').click();
      await expect(page.locator('#form-errors')).toContainText(INVALID_LEAD_MESSAGE);
      await expect(modal(page)).toBeVisible();
      expect((await current(request, sub.name)).to_cancel_reminder_days).toBe(3);

      // Plain HTTP, create and update.
      const create = await request.post('/api/subscriptions', { form: {
        name: uniqueName('Plan cancel bad lead'), cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
        category_id: String(category.id), to_cancel: 'true', to_cancel_reminder_days: '7',
      } });
      expect(create.status()).toBe(400);
      expect((await create.json()).error).toMatch(INVALID_LEAD_MESSAGE);

      const update = await request.put(`/api/subscriptions/${sub.id}`, { form: { to_cancel_reminder_days: '2' } });
      expect(update.status()).toBe(400);
      expect((await update.json()).error).toMatch(INVALID_LEAD_MESSAGE);
      expect((await current(request, sub.name)).to_cancel_reminder_days).toBe(3);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
      await request.delete(`/api/categories/${category.id}`);
    }
  });

  test('an update that does not mention the plan keeps it as it is', async ({ request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel keep'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true', to_cancel_reminder_days: '14' });
    try {
      const renamed = uniqueName('Plan cancel kept');
      expect((await request.put(`/api/subscriptions/${sub.id}`, { form: { name: renamed } })).ok()).toBe(true);
      let after = await current(request, renamed);
      expect(after.to_cancel).toBe(true);
      expect(after.to_cancel_reminder_days).toBe(14);

      expect((await request.put(`/api/subscriptions/${sub.id}`, { form: { to_cancel_reminder_days: '' } })).ok()).toBe(true);
      after = await current(request, renamed);
      expect(after.to_cancel_reminder_days, 'an empty lead leaves the current one alone').toBe(14);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });
});

test.describe('plan to cancel: status changes', () => {
  test('cancelling the subscription clears the plan and disables the option', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel cancelled'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true' });
    try {
      await gotoSubscriptions(page);
      await openEditForm(page, sub.name);
      await expect(toCancel(page)).toBeChecked();
      await form(page).locator('#status').selectOption('Cancelled');
      // The form reacts at once: nothing left to remind about.
      await expect(toCancel(page)).toBeDisabled();
      await expect(toCancel(page)).not.toBeChecked();
      await expect(form(page).locator('#to-cancel-cancelled-note')).toBeVisible();
      await expect(options(page)).toBeHidden();
      await submitAndWaitForReload(page);

      const after = await current(request, sub.name);
      expect(after.status).toBe('Cancelled');
      expect(after.to_cancel).toBe(false);
      await expect(row(page, sub.name).locator('[data-to-cancel]')).toHaveCount(0);

      await openEditForm(page, sub.name);
      await expect(toCancel(page)).toBeDisabled();
      await expect(form(page).locator('#to-cancel-cancelled-note')).toContainText('already cancelled');
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });

  test('cancelling through the API clears the plan too', async ({ request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel api cancelled'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true' });
    try {
      expect((await request.put(`/api/subscriptions/${sub.id}`, { form: { status: 'Cancelled' } })).ok()).toBe(true);
      expect((await current(request, sub.name)).to_cancel).toBe(false);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });

  test('pausing keeps the plan and says the reminder is on hold', async ({ page, request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel paused'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true', to_cancel_reminder_days: '5' });
    try {
      expect((await request.put(`/api/subscriptions/${sub.id}`, { form: { status: 'Paused' } })).ok()).toBe(true);
      const paused = await current(request, sub.name);
      expect(paused.status).toBe('Paused');
      expect(paused.to_cancel).toBe(true);
      expect(paused.to_cancel_reminder_days).toBe(5);

      await gotoSubscriptions(page);
      await expect(row(page, sub.name).locator('[data-to-cancel]')).toBeVisible();
      await expect(row(page, sub.name).locator('[data-to-cancel-paused]')).toContainText('on hold');
      await openEditForm(page, sub.name);
      await expect(toCancel(page)).toBeChecked();
      await expect(toCancel(page)).toBeEnabled();
      await expect(form(page).locator('[data-to-cancel-paused]')).toContainText('no reminders are sent');

      // Back to Active: the plan was never lost.
      await form(page).locator('#status').selectOption('Active');
      await submitAndWaitForReload(page);
      const active = await current(request, sub.name);
      expect(active.to_cancel).toBe(true);
      await expect(row(page, sub.name).locator('[data-to-cancel-paused]')).toHaveCount(0);
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });
});

test.describe('plan to cancel: deadline already passed', () => {
  test('the list, the dashboard and the form warn that no reminder will be sent', async ({ page, request }) => {
    // The renewal is still ahead (a past renewal date is rolled forward by the
    // server), but the 10-day notice period puts the last day to cancel a week ago.
    const sub = await seed(request, { name: uniqueName('Plan cancel overdue'), renewal_date: isoDate(daysFromNow(3)), cancellation_notice_days: '10', to_cancel: 'true' });
    try {
      const saved = await current(request, sub.name);
      expect(saved.to_cancel).toBe(true);
      expect(saved.renewal_date.slice(0, 10)).toBe(isoDate(daysFromNow(3)));

      await gotoSubscriptions(page);
      await expect(row(page, sub.name).locator('[data-to-cancel]')).toBeVisible();
      await expect(row(page, sub.name).locator('[data-to-cancel-missed]')).toContainText('no reminder will be sent');

      await openEditForm(page, sub.name);
      const warning = form(page).locator('[data-to-cancel-missed]');
      await expect(warning).toHaveRole('status');
      await expect(warning).toContainText('already passed');
      await expect(warning).toContainText('No reminder will be sent');
      await page.keyboard.press('Escape');

      await page.goto('/');
      await page.waitForLoadState('networkidle');
      const card = page.locator('main h3', { hasText: sub.name }).locator('..');
      await expect(card.locator('[data-to-cancel]')).toBeVisible();
      await expect(card.locator('[data-to-cancel-missed]')).toBeVisible();
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
  });
});

test.describe('plan to cancel: exports', () => {
  test('CSV and JSON exports carry the plan and its lead', async ({ request }) => {
    const sub = await seed(request, { name: uniqueName('Plan cancel export'), renewal_date: isoDate(daysFromNow(20)), to_cancel: 'true', to_cancel_reminder_days: '14' });
    const plain = await seed(request, { name: uniqueName('Plan cancel export off') });
    try {
      const csv = await request.get('/api/export/csv');
      expect(csv.ok()).toBe(true);
      const { records } = csvRecords(await csv.text());
      const record = records.find((r) => r.Name === sub.name);
      expect(record).toBeTruthy();
      expect(record['To Cancel']).toBe('true');
      expect(record['To Cancel Reminder Days']).toBe('14');
      const other = records.find((r) => r.Name === plain.name);
      expect(other['To Cancel']).toBe('false');
      expect(other['To Cancel Reminder Days']).toBe('3');

      const json = await (await request.get('/api/export/json')).json();
      const exported = json.subscriptions.find((s) => s.name === sub.name);
      expect(exported).toMatchObject({ to_cancel: true, to_cancel_reminder_days: 14 });
      expect(json.subscriptions.find((s) => s.name === plain.name)).toMatchObject({ to_cancel: false, to_cancel_reminder_days: 3 });
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
      await request.delete(`/api/subscriptions/${plain.id}`);
    }
  });
});
