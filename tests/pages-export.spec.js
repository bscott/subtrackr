// @ts-check
// Data export: CSV, JSON, iCal and backup downloads, from the API and from the page links.
// cancellation-notice.spec.js already checks the CSV notice column; this covers the rest of the formats.
const fs = require('fs');
const { test, expect } = require('@playwright/test');
const { uniqueName, daysFromNow, isoDate, fixtures, csvRecords, icalEvents } = require('./helpers/pages');

async function seedExportRows(data) {
  const category = await data.category(uniqueName('Export category'));
  const cat = String(category.id);
  const quarterly = await data.subscription({
    name: uniqueName('Export "quoted", comma'), category_id: cat, cost: '30', schedule: 'Quarterly',
    schedule_interval: '2', original_currency: 'EUR', payment_method: 'Visa', account: 'me@example.com',
    start_date: '2026-01-15', renewal_date: isoDate(daysFromNow(45)), cancellation_notice_days: '14',
    notes: 'line one\nline two', usage: 'High', autopay: 'true',
  });
  const weekly = await data.subscription({ name: uniqueName('Export weekly'), category_id: cat, cost: '4.5', schedule: 'Weekly', renewal_date: isoDate(daysFromNow(5)) });
  const cancelled = await data.subscription({ name: uniqueName('Export cancelled'), category_id: cat, cost: '7', status: 'Cancelled', renewal_date: isoDate(daysFromNow(9)), cancellation_date: isoDate(daysFromNow(9)) });
  return { category, quarterly, weekly, cancelled };
}

test('CSV export is a quoted attachment with one record per subscription', async ({ request }) => {
  const data = fixtures(request);
  try {
    const { category, quarterly, weekly, cancelled } = await seedExportRows(data);
    const response = await request.get('/api/export/csv');
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toMatch(/^text\/csv/);
    expect(response.headers()['content-disposition']).toBe('attachment; filename=subscriptions.csv');

    const { header, records } = csvRecords(await response.text());
    expect(header).toEqual(['ID', 'Name', 'Category', 'Cost', 'Currency', 'Schedule', 'Schedule Interval', 'Status',
      'Payment Method', 'Autopay', 'Account', 'Start Date', 'Renewal Date', 'Cancellation Date',
      'Cancellation Notice Days', 'URL', 'Notes', 'Usage', 'Created At', 'To Cancel', 'To Cancel Reminder Days']);
    for (const record of records) expect(Object.keys(record)).toHaveLength(header.length);

    const row = records.find(r => r.ID === String(quarterly.id));
    expect(row).toMatchObject({
      Name: quarterly.name, Category: category.name, Cost: '30.00', Currency: 'EUR', Schedule: 'Every 2 Quarters',
      'Schedule Interval': '2', Status: 'Active', 'Payment Method': 'Visa', Autopay: 'true', Account: 'me@example.com',
      'Start Date': '2026-01-15', 'Renewal Date': quarterly.renewal_date.slice(0, 10), 'Cancellation Date': '',
      'Cancellation Notice Days': '14', Notes: 'line one\nline two', Usage: 'High',
    });
    expect(row?.['Created At']).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    expect(records.find(r => r.ID === String(weekly.id))).toMatchObject({ Name: weekly.name, Cost: '4.50', Schedule: 'Weekly', Autopay: 'unknown' });
    expect(records.find(r => r.ID === String(cancelled.id))).toMatchObject({ Status: 'Cancelled', 'Cancellation Date': isoDate(daysFromNow(9)) });
  } finally {
    await data.cleanup();
  }
});

test('JSON export is an attachment listing every subscription', async ({ request }) => {
  const data = fixtures(request);
  try {
    const { category, quarterly, cancelled } = await seedExportRows(data);
    const response = await request.get('/api/export/json');
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toMatch(/^application\/json/);
    expect(response.headers()['content-disposition']).toBe('attachment; filename=subscriptions.json');

    const body = await response.json();
    expect(body.total_count).toBe(body.subscriptions.length);
    expect(Math.abs(Date.parse(body.exported_at) - Date.now())).toBeLessThan(5 * 60 * 1000);
    expect(body.subscriptions.find(s => s.id === quarterly.id)).toMatchObject({
      name: quarterly.name, cost: 30, original_currency: 'EUR', schedule: 'Quarterly', schedule_interval: 2,
      status: 'Active', category_id: category.id, category: { name: category.name }, autopay: true,
      payment_method: 'Visa', cancellation_notice_days: 14, notes: 'line one\nline two',
    });
    expect(body.subscriptions.find(s => s.id === cancelled.id)).toMatchObject({ status: 'Cancelled' });
    await request.delete(`/api/subscriptions/${cancelled.id}`);
    const after = await (await request.get('/api/export/json')).json();
    expect(after.subscriptions.find(s => s.id === cancelled.id)).toBeUndefined();
  } finally {
    await data.cleanup();
  }
});

test('iCal export has a recurring event per active subscription and a cancel-by event', async ({ request }) => {
  const data = fixtures(request);
  try {
    const { quarterly, weekly, cancelled } = await seedExportRows(data);
    const response = await request.get('/api/export/ical');
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toBe('text/calendar; charset=utf-8');
    expect(response.headers()['content-disposition']).toBe('attachment; filename="subtrackr-renewals.ics"');

    const text = await response.text();
    expect(text.startsWith('BEGIN:VCALENDAR\r\nVERSION:2.0\r\n')).toBeTruthy();
    expect(text.endsWith('END:VCALENDAR\r\n')).toBeTruthy();
    expect(text.replace(/\r\n/g, '')).not.toContain('\n');
    const events = icalEvents(text);
    const uids = events.map(e => e.UID);
    expect(new Set(uids).size).toBe(uids.length);

    const compact = date => date.slice(0, 10).replace(/-/g, '');
    const renewal = events.find(e => e.UID?.startsWith(`subtrackr-${quarterly.id}-`));
    expect(renewal).toMatchObject({
      DTSTART: `${compact(quarterly.renewal_date)}T000000Z`,
      DTEND: `${compact(quarterly.renewal_date)}T010000Z`,
      RRULE: 'FREQ=MONTHLY;INTERVAL=6',
      STATUS: 'CONFIRMED',
    });
    expect(renewal?.DESCRIPTION).toContain('Cost: €30.00');
    expect(renewal?.DESCRIPTION).toContain('Schedule: Every 2 Quarters');

    const cancelDay = new Date(Date.parse(quarterly.renewal_date) - 14 * 24 * 60 * 60 * 1000).toISOString();
    const cancelBy = events.find(e => e.UID?.startsWith(`subtrackr-cancelby-${quarterly.id}-`));
    expect(cancelBy?.DTSTART).toBe(`${compact(cancelDay)}T000000Z`);
    expect(cancelBy?.RRULE).toBeUndefined();
    expect(cancelBy?.DESCRIPTION).toContain(`renews on ${quarterly.renewal_date.slice(0, 10)} (14 days notice required)`);

    const weeklyEvent = events.find(e => e.UID?.startsWith(`subtrackr-${weekly.id}-`));
    expect(weeklyEvent).toMatchObject({ SUMMARY: `${weekly.name} Renewal`, RRULE: 'FREQ=WEEKLY;INTERVAL=1' });
    expect(events.find(e => e.UID?.startsWith(`subtrackr-cancelby-${weekly.id}-`))).toBeUndefined();
    expect(events.find(e => e.UID?.includes(`-${cancelled.id}-`))).toBeUndefined();
  } finally {
    await data.cleanup();
  }
});

test('iCal export escapes commas and semicolons in names', async ({ request }) => {
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Export ical escaping'));
    const sub = await data.subscription({ name: uniqueName('Gym, Pool; Spa'), category_id: String(category.id), renewal_date: isoDate(daysFromNow(30)) });
    const events = icalEvents(await (await request.get('/api/export/ical')).text());
    const event = events.find(e => e.UID?.startsWith(`subtrackr-${sub.id}-`));
    expect(event?.SUMMARY).toBe(`${sub.name.replace(/[,;]/g, m => `\\${m}`)} Renewal`);
  } finally {
    await data.cleanup();
  }
});

test('iCal export keeps line breaks and backslashes inside text values', async ({ request }) => {
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Calendar text'));
    const sub = await data.subscription({
      name: 'Gym\\Pool\r\nSUMMARY:Injected', category_id: String(category.id),
      renewal_date: isoDate(daysFromNow(30)), cancellation_notice_days: '3',
    });
    const text = await (await request.get('/api/export/ical')).text();
    expect(text).not.toContain('\r\nSUMMARY:Injected');
    const events = icalEvents(text);
    expect(events.find(e => e.UID?.startsWith(`subtrackr-${sub.id}-`)).SUMMARY)
      .toBe('Gym\\\\Pool\\nSUMMARY:Injected Renewal');
    expect(events.find(e => e.UID?.startsWith(`subtrackr-cancelby-${sub.id}-`)).SUMMARY)
      .toBe('Cancel Gym\\\\Pool\\nSUMMARY:Injected by today');
  } finally {
    await data.cleanup();
  }
});

test('backup is an attachment with subscriptions and current stats', async ({ request }) => {
  const data = fixtures(request);
  try {
    const { category, quarterly, weekly } = await seedExportRows(data);
    const response = await request.get('/api/backup');
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toMatch(/^application\/json/);
    expect(response.headers()['content-disposition']).toBe('attachment; filename=subtrackr-backup.json');

    const backup = await response.json();
    expect(backup.version).toBe('1.0');
    expect(Math.abs(Date.parse(backup.backup_date) - Date.now())).toBeLessThan(5 * 60 * 1000);
    expect(backup.total_count).toBe(backup.subscriptions.length);
    expect(backup.subscriptions.find(s => s.id === quarterly.id)).toMatchObject({ name: quarterly.name, category: { name: category.name } });
    expect(backup.subscriptions.find(s => s.id === weekly.id)).toMatchObject({ name: weekly.name, cost: 4.5 });
    // The handler reads subscriptions and stats separately, so retry if another test wrote in between.
    await expect(async () => {
      const b = await (await request.get('/api/backup')).json();
      const active = b.subscriptions.filter(s => s.status === 'Active');
      expect(b.stats.active_subscriptions).toBe(active.length);
      expect(b.stats.totals_by_currency.USD.active_subscriptions).toBe(active.filter(s => s.original_currency === 'USD').length);
    }).toPass({ timeout: 30_000 });
    expect(backup.stats.category_spending_by_currency[category.name]).toEqual({ EUR: expect.closeTo(30 / 6, 6), USD: expect.closeTo(4.5 * 4.33, 6) });
  } finally {
    await data.cleanup();
  }
});

test('settings page export and backup links download files', async ({ page }) => {
  await page.goto('/settings');
  const downloads = [
    ['/api/export/csv', 'subscriptions.csv', (body) => expect(body).toMatch(/^ID,Name,Category,/)],
    ['/api/export/json', 'subscriptions.json', (body) => expect(Array.isArray(JSON.parse(body).subscriptions)).toBe(true)],
    ['/api/backup', 'subtrackr-backup.json', (body) => expect(JSON.parse(body).version).toBe('1.0')],
  ];
  for (const [href, filename, checkBody] of downloads) {
    const link = page.locator(`a[href="${href}"]`);
    await expect(link).toHaveCount(1);
    const downloadPromise = page.waitForEvent('download');
    await link.click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe(filename);
    checkBody(fs.readFileSync(await download.path(), 'utf8'));
    // The file is saved; the browser must not navigate away to show it.
    await expect(page).toHaveURL(/\/settings$/);
  }
});
