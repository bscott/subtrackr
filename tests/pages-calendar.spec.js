// @ts-check
// /calendar: month navigation, renewal and cancel-by events, and the edit modal opened from an event.
// Escaping of event names and icons is covered by calendar-escaping.spec.js.
const { test, expect } = require('@playwright/test');
const { uniqueName, isoDate, utcDate, fixtures } = require('./helpers/pages');

// A month comfortably in the future so renewal dates are not rolled forward on read.
function futureMonth(monthsAhead) {
  const now = new Date();
  const first = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthsAhead, 1));
  return { year: first.getUTCFullYear(), month: first.getUTCMonth() + 1 };
}

const daysIn = (year, month) => new Date(Date.UTC(year, month, 0)).getUTCDate();
const firstWeekday = (year, month) => new Date(Date.UTC(year, month - 1, 1)).getUTCDay();

// The grid always has 42 cells: trailing days of the previous month, then this month, then the next.
function dayCell(page, year, month, day) {
  return page.locator('#calendar-grid > div').nth(firstWeekday(year, month) + day - 1);
}

// The day number is the cell's first line; other specs may have events on the same days.
const dayNumber = cell => cell.locator(':scope > div').first();

test('calendar grid lays out the requested month and navigates across year boundaries', async ({ page }) => {
  await page.goto('/calendar?year=2026&month=12');
  const cells = page.locator('#calendar-grid > div');
  await expect(cells).toHaveCount(42);
  await expect(page.locator('main h1')).toContainText('2026');
  // December 2026 starts on a Tuesday and has 31 days.
  expect(firstWeekday(2026, 12)).toBe(2);
  await expect(dayNumber(dayCell(page, 2026, 12, 1))).toHaveText('1');
  await expect(dayNumber(dayCell(page, 2026, 12, 31))).toHaveText('31');
  await expect(dayNumber(cells.nth(2 + 31))).toHaveText('1');

  await page.locator('a[href="/calendar?year=2027&month=1"]').click();
  await expect(page).toHaveURL(/\/calendar\?year=2027&month=1$/);
  await expect(page.locator('main h1')).toContainText('2027');
  await expect(dayNumber(dayCell(page, 2027, 1, 1))).toHaveText('1');
  await expect(dayNumber(dayCell(page, 2027, 1, daysIn(2027, 1)))).toHaveText(String(daysIn(2027, 1)));

  await page.locator('a[href="/calendar?year=2026&month=12"]').click();
  await page.locator('a[href="/calendar?year=2026&month=11"]').click();
  await expect(page).toHaveURL(/\/calendar\?year=2026&month=11$/);
  await expect(page.locator('main h1')).toContainText('2026');
  await expect(dayNumber(dayCell(page, 2026, 11, 30))).toHaveText('30');

  // "Today" returns to the current month.
  await page.locator('main a[href="/calendar"]').click();
  await expect(page).toHaveURL(/\/calendar$/);
  const now = new Date();
  await expect(page.locator('main h1')).toContainText(String(now.getFullYear()));
  const prev = new Date(now.getFullYear(), now.getMonth() - 1, 1);
  await expect(page.locator(`a[href="/calendar?year=${prev.getFullYear()}&month=${prev.getMonth() + 1}"]`)).toHaveCount(1);
});

test('leading cells show the last days of the previous month', async ({ page }) => {
  // BUG: calendar.html takes the previous month's length from `new Date(year, month - 2, 0)`,
  // which is two months back, so December 2026 starts "30 31" (October) instead of "29 30" (November).
  test.fail();
  await page.goto('/calendar?year=2026&month=12');
  const cells = page.locator('#calendar-grid > div');
  await expect(dayNumber(cells.nth(0))).toHaveText('29');
  await expect(dayNumber(cells.nth(1))).toHaveText('30');
});

test('active subscriptions appear on their renewal day, with a cancel-by event for notice periods', async ({ page, request }) => {
  const { year, month } = futureMonth(2);
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Calendar events'));
    const cat = String(category.id);
    const renewing = await data.subscription({ name: uniqueName('Cal renews'), category_id: cat, cost: '12.34', renewal_date: isoDate(utcDate(year, month, 20)), cancellation_notice_days: '5' });
    const plain = await data.subscription({ name: uniqueName('Cal plain'), category_id: cat, cost: '3', schedule: 'Annual', renewal_date: isoDate(utcDate(year, month, 20)) });
    const cancelled = await data.subscription({ name: uniqueName('Cal cancelled'), category_id: cat, cost: '4', status: 'Cancelled', renewal_date: isoDate(utcDate(year, month, 12)) });
    const paused = await data.subscription({ name: uniqueName('Cal paused'), category_id: cat, cost: '4', status: 'Paused', renewal_date: isoDate(utcDate(year, month, 12)) });

    await page.goto(`/calendar?year=${year}&month=${month}`);
    const renewalCell = dayCell(page, year, month, 20);
    await expect(dayNumber(renewalCell)).toHaveText('20');
    const renewalEvent = renewalCell.locator('button', { hasText: renewing.name });
    await expect(renewalEvent).toHaveCount(1);
    await expect(renewalEvent).toContainText('12.34');
    await expect(renewalEvent).not.toContainText('⏰');
    await expect(renewalCell.locator('button', { hasText: plain.name })).toHaveCount(1);

    const cancelByCell = dayCell(page, year, month, 15);
    const cancelByEvent = cancelByCell.locator('button', { hasText: renewing.name });
    await expect(cancelByEvent).toHaveCount(1);
    await expect(cancelByEvent).toContainText('⏰');
    await expect(cancelByEvent).toContainText('12.34');
    // No cancel-by event without a notice period.
    await expect(page.locator('#calendar-grid button', { hasText: plain.name })).toHaveCount(1);
    // Each event appears exactly once per day it belongs to.
    await expect(page.locator('#calendar-grid button', { hasText: renewing.name })).toHaveCount(2);

    // Only active subscriptions are shown.
    await expect(page.locator('#calendar-grid button', { hasText: cancelled.name })).toHaveCount(0);
    await expect(page.locator('#calendar-grid button', { hasText: paused.name })).toHaveCount(0);
  } finally {
    await data.cleanup();
  }
});

test('a cancel-by deadline in the previous month shows on that month', async ({ page, request }) => {
  const { year, month } = futureMonth(3);
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Calendar crossing'));
    const sub = await data.subscription({ name: uniqueName('Cal crossing'), category_id: String(category.id), cost: '9', renewal_date: isoDate(utcDate(year, month, 3)), cancellation_notice_days: '10' });
    const cancelBy = utcDate(year, month, 3 - 10);
    const cy = cancelBy.getUTCFullYear();
    const cm = cancelBy.getUTCMonth() + 1;

    await page.goto(`/calendar?year=${year}&month=${month}`);
    await expect(dayCell(page, year, month, 3).locator('button', { hasText: sub.name })).not.toContainText('⏰');
    await expect(page.locator('#calendar-grid button', { hasText: sub.name })).toHaveCount(1);

    await page.locator(`a[href="/calendar?year=${cy}&month=${cm}"]`).click();
    const event = dayCell(page, cy, cm, cancelBy.getUTCDate()).locator('button', { hasText: sub.name });
    await expect(event).toHaveCount(1);
    await expect(event).toContainText('⏰');
  } finally {
    await data.cleanup();
  }
});

test('clicking a renewal or cancel-by event opens that subscription in the edit modal', async ({ page, request }) => {
  const { year, month } = futureMonth(2);
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Calendar modal'));
    const sub = await data.subscription({ name: uniqueName('Cal modal'), category_id: String(category.id), cost: '21', renewal_date: isoDate(utcDate(year, month, 24)), cancellation_notice_days: '7', notes: 'calendar modal note' });
    const other = await data.subscription({ name: uniqueName('Cal modal other'), category_id: String(category.id), cost: '5', renewal_date: isoDate(utcDate(year, month, 24)) });

    await page.goto(`/calendar?year=${year}&month=${month}`);
    const modal = page.locator('#modal');
    await expect(modal).toBeHidden();

    await dayCell(page, year, month, 24).locator('button', { hasText: sub.name }).click();
    await expect(modal).toBeVisible();
    await expect(page.locator('#modal-content #name')).toHaveValue(sub.name);
    await expect(page.locator('#modal-content #cancellation_notice_days')).toHaveValue('7');
    await expect(page.locator('#modal-content textarea[name="notes"]')).toHaveValue('calendar modal note');

    // Clicking the backdrop closes the modal.
    await modal.click({ position: { x: 5, y: 5 } });
    await expect(modal).toBeHidden();

    // The cancel-by event opens the same subscription.
    await dayCell(page, year, month, 17).locator('button', { hasText: sub.name }).click();
    await expect(modal).toBeVisible();
    await expect(page.locator('#modal-content #name')).toHaveValue(sub.name);
    await modal.click({ position: { x: 5, y: 5 } });
    await expect(modal).toBeHidden();

    // A second event on the same day replaces the modal content.
    await dayCell(page, year, month, 24).locator('button', { hasText: other.name }).click();
    await expect(modal).toBeVisible();
    await expect(page.locator('#modal-content #name')).toHaveValue(other.name);
  } finally {
    await data.cleanup();
  }
});

test('calendar export link downloads the iCal file', async ({ page }) => {
  await page.goto('/calendar');
  const downloadPromise = page.waitForEvent('download');
  await page.locator('a[href="/api/export/ical"]').click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe('subtrackr-renewals.ics');
});
