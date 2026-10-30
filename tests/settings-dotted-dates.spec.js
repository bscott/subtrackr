// A private server keeps this global preference independent of other specs.
const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');
const { csvRecords, icalEvents } = require('./helpers/pages');

test('dotted European dates persist without changing date inputs or export contracts', async ({ page, request }) => {
  const year = new Date().getUTCFullYear() + 2;
  const renewal = `${year}-02-24`;
  const sub = await seedSubscription(request, {
    name: 'Dotted date round trip', start_date: `${year - 1}-02-24`,
    renewal_date: renewal, cancellation_notice_days: '14',
  });
  await page.goto('/settings');
  const original = await page.locator('input[name="date_format"]:checked').inputValue();
  try {
    const saved = page.waitForResponse(r => r.url().endsWith('/api/settings/date-format') && r.request().method() === 'POST');
    await page.getByRole('radio', { name: 'DD.MM.YYYY (24.02.2026)', exact: true }).check();
    expect((await saved).status()).toBe(200);
    await page.reload();
    await expect(page.locator('input[value="DD.MM.YYYY"]')).toBeChecked();

    const rejected = await request.post('/api/settings/date-format', { form: { date_format: 'DD-MM-YYYY' } });
    expect(rejected.status()).toBe(400);
    await page.reload();
    await expect(page.locator('input[value="DD.MM.YYYY"]')).toBeChecked();

    await page.goto('/subscriptions');
    const row = page.locator('tr', { hasText: sub.name });
    await expect(row).toContainText(`24.02.${year}`);
    await expect(row).toContainText(`10.02.${year}`);

    await page.goto(`/form/subscription/${sub.id}`);
    await expect(page.locator('input[name="renewal_date"]')).toHaveValue(renewal);
    await expect(page.locator('input[name="start_date"]')).toHaveValue(`${year - 1}-02-24`);

    const csv = csvRecords(await (await request.get('/api/export/csv')).text());
    expect(csv.records.find(r => r.ID === String(sub.id))['Renewal Date']).toBe(renewal);
    const json = await (await request.get('/api/export/json')).json();
    expect(json.subscriptions.find(s => s.id === sub.id).renewal_date).toMatch(new RegExp(`^${renewal}T`));
    const events = icalEvents(await (await request.get('/api/export/ical')).text());
    expect(events.find(e => e.UID?.startsWith(`subtrackr-${sub.id}-`)).DTSTART).toBe(`${year}0224T000000Z`);
  } finally {
    await request.post('/api/settings/date-format', { form: { date_format: original } });
    await request.delete(`/api/subscriptions/${sub.id}`);
  }
});
