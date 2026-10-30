// @ts-check
// Every schedule option and status in the subscription form, created through the modal:
// the renewal date the form proposes, what is saved, how the list labels it, and that the
// edit form selects it again.
const { test, expect } = require('@playwright/test');
const {
  anyLocale, uniqueName, createCategory, findSubscription, row, form,
  gotoSubscriptions, openAddForm, openEditForm, submitAndWaitForReload,
} = require('./helpers/subscription-ui');

const pad = n => String(n).padStart(2, '0');
const isoDate = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** Today plus `months`, clamped to the last day of the target month (Jan 31 + 1 → Feb 28/29). */
function plusMonths(today, months) {
  const first = new Date(today.getFullYear(), today.getMonth() + months, 1);
  const lastDay = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
  return new Date(first.getFullYear(), first.getMonth(), Math.min(today.getDate(), lastDay));
}

const plusDays = (today, days) => new Date(today.getFullYear(), today.getMonth(), today.getDate() + days);

// [combo value, saved schedule, saved interval, list label key, expected renewal from today]
const schedules = [
  ['Daily_1', 'Daily', 1, ['schedule.daily'], t => plusDays(t, 1)],
  ['Weekly_1', 'Weekly', 1, ['schedule.weekly'], t => plusDays(t, 7)],
  ['Weekly_2', 'Weekly', 2, ['schedule.every_n_weeks', 2], t => plusDays(t, 14)],
  ['Monthly_1', 'Monthly', 1, ['schedule.monthly'], t => plusMonths(t, 1)],
  ['Monthly_2', 'Monthly', 2, ['schedule.every_n_months', 2], t => plusMonths(t, 2)],
  ['Quarterly_1', 'Quarterly', 1, ['schedule.quarterly'], t => plusMonths(t, 3)],
  ['Monthly_6', 'Monthly', 6, ['schedule.every_n_months', 6], t => plusMonths(t, 6)],
  ['Annual_1', 'Annual', 1, ['schedule.annual'], t => plusMonths(t, 12)],
  ['Annual_2', 'Annual', 2, ['schedule.every_n_years', 2], t => plusMonths(t, 24)],
  ['Annual_3', 'Annual', 3, ['schedule.every_n_years', 3], t => plusMonths(t, 36)],
  ['Annual_5', 'Annual', 5, ['schedule.every_n_years', 5], t => plusMonths(t, 60)],
  ['Annual_10', 'Annual', 10, ['schedule.every_n_years', 10], t => plusMonths(t, 120)],
];

const statuses = [
  ['Active', 'status.active', 'bg-success'],
  ['Cancelled', 'status.cancelled', 'bg-danger'],
  ['Paused', 'status.paused', 'bg-warning'],
  ['Trial', 'status.trial', 'bg-warning'],
];

/** Creates a category (before the form loads, so it is in the dropdown) and opens the Add form. */
async function openAddWithCategory(page, request) {
  const category = await createCategory(request);
  await gotoSubscriptions(page);
  await openAddForm(page);
  return category;
}

async function fillRequired(page, category, name, { combo = 'Monthly_1', status = 'Active' } = {}) {
  const f = form(page);
  await f.locator('#name').fill(name);
  await f.locator('#cost').fill('4.2');
  await f.locator('#category_id').selectOption(String(category.id));
  await f.locator('#schedule_combo').selectOption(combo);
  await f.locator('#status').selectOption(status);
}

const scheduleCell = (page, name) => row(page, name).locator('td').nth(3);
const statusBadge = (page, name) => row(page, name).locator('td').nth(4).locator('span');

test.describe('schedules', () => {
  for (const [combo, schedule, interval, [labelKey, labelArg], renewal] of schedules) {
    test(`${combo} proposes the renewal date, saves and is shown`, async ({ page, request }) => {
      const name = uniqueName(`Schedule ${combo}`);
      const category = await openAddWithCategory(page, request);
      await fillRequired(page, category, name, { combo });

      const expectedRenewal = isoDate(renewal(new Date()));
      await expect(form(page).locator('#renewal_date')).toHaveValue(expectedRenewal);
      await submitAndWaitForReload(page);

      await expect(scheduleCell(page, name)).toHaveText(anyLocale(labelKey, labelArg));
      const saved = await findSubscription(request, name);
      expect(saved).toMatchObject({ schedule, schedule_interval: interval });
      expect(saved.renewal_date.slice(0, 10)).toBe(expectedRenewal);

      await openEditForm(page, name);
      await expect(form(page).locator('#schedule_combo')).toHaveValue(combo);
      await expect(form(page).locator('#schedule')).toHaveValue(schedule);
      await expect(form(page).locator('#schedule_interval')).toHaveValue(String(interval));
    });
  }

  test('changing the schedule while editing saves the new schedule and renewal date', async ({ page, request }) => {
    const name = uniqueName('Schedule change');
    const category = await openAddWithCategory(page, request);
    await fillRequired(page, category, name, { combo: 'Weekly_1' });
    await submitAndWaitForReload(page);

    await openEditForm(page, name);
    await form(page).locator('#schedule_combo').selectOption('Annual_2');
    const expectedRenewal = isoDate(plusMonths(new Date(), 24));
    await expect(form(page).locator('#renewal_date')).toHaveValue(expectedRenewal);
    await submitAndWaitForReload(page);

    await expect(scheduleCell(page, name)).toHaveText(anyLocale('schedule.every_n_years', 2));
    const saved = await findSubscription(request, name);
    expect(saved).toMatchObject({ schedule: 'Annual', schedule_interval: 2 });
    expect(saved.renewal_date.slice(0, 10)).toBe(expectedRenewal);
  });
});

test.describe('statuses', () => {
  for (const [status, labelKey, colourClass] of statuses) {
    test(`${status} is saved, badged in the list and pre-selected on edit`, async ({ page, request }) => {
      const name = uniqueName(`Status ${status}`);
      const category = await openAddWithCategory(page, request);
      await fillRequired(page, category, name, { status });
      await submitAndWaitForReload(page);

      await expect(statusBadge(page, name)).toHaveText(anyLocale(labelKey));
      await expect(statusBadge(page, name)).toHaveClass(new RegExp(colourClass));
      expect((await findSubscription(request, name)).status).toBe(status);

      await openEditForm(page, name);
      await expect(form(page).locator('#status')).toHaveValue(status);
    });
  }

  test('changing the status while editing updates the badge', async ({ page, request }) => {
    const name = uniqueName('Status change');
    const category = await openAddWithCategory(page, request);
    await fillRequired(page, category, name, { status: 'Trial' });
    await submitAndWaitForReload(page);

    await openEditForm(page, name);
    await form(page).locator('#status').selectOption('Cancelled');
    await submitAndWaitForReload(page);

    await expect(statusBadge(page, name)).toHaveText(anyLocale('status.cancelled'));
    await expect(statusBadge(page, name)).toHaveClass(/bg-danger/);
    expect((await findSubscription(request, name)).status).toBe('Cancelled');
  });
});
