// Settings > Data Management: clear all, backup, restore, and Wallos import.
// These replace or wipe every subscription, so they run in the `isolated`
// project against its own server (:8083), one test at a time (workers: 1).
const { test, expect } = require('@playwright/test');
const fs = require('fs');

// Same payload as sampleWallosExport in internal/service/wallos_import_test.go,
// except Netflix renews in the future: the app rolls past renewal dates forward.
const wallosExport = {
  success: true,
  subscriptions: [
    {
      Name: 'Netflix', 'Payment Cycle': 'Monthly', 'Next Payment': '2030-01-15', Renewal: 'Automatic',
      Category: 'Entertainment', 'Payment Method': 'Visa', 'Paid By': 'Alice', Price: '$15.99',
      Notes: 'Family plan', URL: 'https://netflix.com', State: 'Enabled', Notifications: 'Enabled',
      'Cancellation Date': '', Active: 'Yes',
    },
    {
      Name: 'Adobe CC', 'Payment Cycle': 'Yearly', 'Next Payment': '2026-06-01', Renewal: 'Manual',
      Category: 'Productivity', 'Payment Method': 'PayPal', 'Paid By': 'Bob', Price: '€59,99',
      Notes: '', URL: 'https://adobe.com', State: 'Disabled', Notifications: 'Disabled',
      'Cancellation Date': '2026-05-20', Active: 'No',
    },
    {
      Name: 'Storage Box', 'Payment Cycle': 'Every 3 Months', 'Next Payment': '2026-02-10', Renewal: 'Automatic',
      Category: 'Storage', 'Payment Method': 'SEPA', 'Paid By': 'Alice', Price: '£8.50',
      Notes: 'Hetzner', URL: '', State: 'Enabled', Notifications: 'Enabled',
      'Cancellation Date': '', Active: 'Yes',
    },
  ],
};

function jsonFile(name, value) {
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return { name, mimeType: 'application/json', buffer: Buffer.from(text) };
}

async function clearAll(request) {
  expect((await request.delete('/api/clear-all')).ok()).toBeTruthy();
}

async function categoryID(request, name) {
  const existing = (await (await request.get('/api/categories')).json()).find((c) => c.name === name);
  if (existing) return existing.id;
  const created = await request.post('/api/categories', { data: { name } });
  expect(created.ok()).toBeTruthy();
  return (await created.json()).id;
}

async function seed(request, fields) {
  const { category, ...rest } = fields;
  const response = await request.post('/api/subscriptions', { form: {
    cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
    category_id: String(await categoryID(request, category || 'General')),
    ...rest,
  } });
  expect(response.ok()).toBeTruthy();
}

async function allSubscriptions(request) {
  const response = await request.get('/api/export/json');
  expect(response.ok()).toBeTruthy();
  const subs = (await response.json()).subscriptions;
  return subs.sort((a, b) => a.name.localeCompare(b.name));
}

async function subscriptionNames(request) {
  return (await allSubscriptions(request)).map((s) => s.name);
}

test.beforeEach(async ({ request }) => {
  await clearAll(request);
});

test.describe('clear all data', () => {
  test('asks for confirmation and keeps data when cancelled', async ({ page, request }) => {
    await seed(request, { name: 'Keep Me' });
    await page.goto('/settings');

    const dialog = page.waitForEvent('dialog');
    const click = page.getByRole('button', { name: 'Clear Data' }).click();
    const prompt = await dialog;
    expect(prompt.type()).toBe('confirm');
    expect(prompt.message()).toBe('Are you sure you want to delete all subscription data? This action cannot be undone.');
    await prompt.dismiss();
    await click;

    await page.goto('/subscriptions');
    await expect(page.getByText('Keep Me')).toBeVisible();
    expect(await subscriptionNames(request)).toEqual(['Keep Me']);
  });

  test('deletes every subscription once confirmed', async ({ page, request }) => {
    await seed(request, { name: 'Doomed One' });
    await seed(request, { name: 'Doomed Two', category: 'Other' });
    await page.goto('/subscriptions');
    await expect(page.getByText('Doomed One')).toBeVisible();

    await page.goto('/settings');
    page.once('dialog', (dialog) => dialog.accept());
    const cleared = page.waitForResponse((r) => r.url().endsWith('/api/clear-all') && r.request().method() === 'DELETE');
    await page.getByRole('button', { name: 'Clear Data' }).click();
    const response = await cleared;
    expect(response.status()).toBe(200);
    expect(await response.json()).toMatchObject({ deleted_count: 2 });

    await page.goto('/subscriptions');
    await expect(page.getByText('No subscriptions yet')).toBeVisible();
    await expect(page.getByText('Doomed One')).toHaveCount(0);
    expect(await subscriptionNames(request)).toEqual([]);

    // Categories are not subscription data and survive.
    const categories = (await (await request.get('/api/categories')).json()).map((c) => c.name);
    expect(categories).toContain('Other');
  });
});

test.describe('backup and restore', () => {
  async function seedBackupFixtures(request) {
    await seed(request, {
      name: 'Backup Alpha', label: 'family', cost: '12.34', schedule: 'Monthly', schedule_interval: '2',
      status: 'Active', original_currency: 'EUR', category: 'Backup Cat A', payment_method: 'Visa',
      account: 'alice@example.test', url: 'https://alpha.example', notes: 'Line one', renewal_date: '2027-03-15',
      share_count: '3',
    });
    await seed(request, {
      name: 'Backup Beta', cost: '45', schedule: 'Annual', status: 'Cancelled', category: 'Backup Cat B',
      cancellation_date: '2026-12-01',
    });
  }

  async function downloadBackup(page, testInfo) {
    await page.goto('/settings');
    const download = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Create Backup' }).click();
    const file = await download;
    expect(file.suggestedFilename()).toBe('subtrackr-backup.json');
    const target = testInfo.outputPath('backup.json');
    await file.saveAs(target);
    return target;
  }

  async function restore(page, filePayload, mode) {
    await page.goto('/settings');
    const form = page.locator('#restore-form');
    await form.locator('input[type="file"]').setInputFiles(filePayload);
    await form.locator('select[name="mode"]').selectOption(mode);
    await form.getByRole('button', { name: 'Restore' }).click();
  }

  test('Create Backup downloads every subscription as JSON', async ({ page, request }, testInfo) => {
    await seedBackupFixtures(request);
    const backup = JSON.parse(fs.readFileSync(await downloadBackup(page, testInfo), 'utf8'));

    expect(backup.version).toBe('1.0');
    expect(backup.total_count).toBe(2);
    const names = backup.subscriptions.map((s) => s.name).sort();
    expect(names).toEqual(['Backup Alpha', 'Backup Beta']);
    const alpha = backup.subscriptions.find((s) => s.name === 'Backup Alpha');
    expect(alpha).toMatchObject({ cost: 12.34, original_currency: 'EUR', category: { name: 'Backup Cat A' } });
    expect(backup.stats).toBeTruthy();
  });

  test('restoring a backup after clearing brings the data back', async ({ page, request }, testInfo) => {
    await seedBackupFixtures(request);
    const before = await allSubscriptions(request);
    const backupPath = await downloadBackup(page, testInfo);
    await clearAll(request);

    page.once('dialog', (dialog) => {
      expect(dialog.message()).toBe('This will replace all existing subscription data with the backup. Continue?');
      dialog.accept();
    });
    await restore(page, backupPath, 'replace');
    await expect(page.locator('#restore-message')).toHaveText('Successfully imported 2 subscriptions');

    // The page reloads itself after a successful restore.
    await page.waitForEvent('load');
    await page.goto('/subscriptions');
    await expect(page.getByText('Backup Alpha')).toBeVisible();
    await expect(page.getByText('Backup Beta')).toBeVisible();

    const after = await allSubscriptions(request);
    const fields = ['name', 'label', 'cost', 'schedule', 'schedule_interval', 'status', 'original_currency',
      'payment_method', 'account', 'url', 'notes', 'share_count', 'renewal_date', 'cancellation_date'];
    const pick = (s) => ({ ...Object.fromEntries(fields.map((f) => [f, s[f]])), category: s.category.name });
    expect(after.map(pick)).toEqual(before.map(pick));
  });

  test('replace mode removes subscriptions that are not in the backup', async ({ page, request }, testInfo) => {
    await seedBackupFixtures(request);
    const backupPath = await downloadBackup(page, testInfo);
    await seed(request, { name: 'Added After Backup' });

    page.once('dialog', (dialog) => dialog.accept());
    await restore(page, backupPath, 'replace');
    await expect(page.locator('#restore-message')).toHaveText('Successfully imported 2 subscriptions');
    expect(await subscriptionNames(request)).toEqual(['Backup Alpha', 'Backup Beta']);
  });

  test('cancelling the replace confirmation leaves data alone', async ({ page, request }, testInfo) => {
    await seedBackupFixtures(request);
    const backupPath = await downloadBackup(page, testInfo);
    await seed(request, { name: 'Added After Backup' });

    page.once('dialog', (dialog) => dialog.dismiss());
    await restore(page, backupPath, 'replace');
    await expect(page.locator('#restore-message')).toBeEmpty();
    expect(await subscriptionNames(request)).toEqual(['Added After Backup', 'Backup Alpha', 'Backup Beta']);
  });

  test('merge mode adds the backup next to existing data without asking', async ({ page, request }, testInfo) => {
    await seedBackupFixtures(request);
    const backupPath = await downloadBackup(page, testInfo);
    await clearAll(request);
    await seed(request, { name: 'Already Here' });

    let dialogs = 0;
    page.on('dialog', (dialog) => { dialogs += 1; dialog.dismiss(); });
    await restore(page, backupPath, 'merge');
    await expect(page.locator('#restore-message')).toHaveText('Successfully imported 2 subscriptions');
    expect(dialogs).toBe(0);
    expect(await subscriptionNames(request)).toEqual(['Already Here', 'Backup Alpha', 'Backup Beta']);
  });

  test('restoring keeps reminders turned off', async ({ page, request }) => {
    // BUG: same GORM `default:true` problem as the Wallos import: a backup
    // entry with "reminder_enabled": false is restored with reminders on.
    // Remove test.fail() once restored reminders stay off.
    test.fail();
    const backup = { version: '1.0', subscriptions: [{
      name: 'Quiet Sub', cost: 3, schedule: 'Monthly', status: 'Active', original_currency: 'USD',
      reminder_enabled: false, category: { name: 'General' },
    }] };
    await restore(page, jsonFile('backup.json', backup), 'merge');
    await expect(page.locator('#restore-message')).toHaveText('Successfully imported 1 subscriptions');
    const [quiet] = await allSubscriptions(request);
    expect(quiet).toMatchObject({ name: 'Quiet Sub', reminder_enabled: false });
  });

  test('a file that is not a backup shows an error and changes nothing', async ({ page, request }) => {
    await seed(request, { name: 'Untouched' });

    await restore(page, jsonFile('broken.json', '{ not json'), 'merge');
    await expect(page.locator('#restore-message')).toHaveText('Invalid backup file format');

    await restore(page, jsonFile('empty.json', { version: '1.0', subscriptions: [] }), 'merge');
    await expect(page.locator('#restore-message')).toHaveText('Backup file contains no subscriptions');

    expect(await subscriptionNames(request)).toEqual(['Untouched']);
  });
});

test.describe('Wallos import', () => {
  async function importWallos(page, filePayload, mode) {
    await page.goto('/settings');
    const form = page.locator('#wallos-form');
    await form.locator('input[type="file"]').setInputFiles(filePayload);
    await form.locator('select[name="mode"]').selectOption(mode);
    await form.getByRole('button', { name: 'Import' }).click();
  }

  test('merges a Wallos export and maps its fields', async ({ page, request }) => {
    await seed(request, { name: 'Existing Sub' });

    await importWallos(page, jsonFile('wallos.json', wallosExport), 'merge');
    await expect(page.locator('#wallos-message')).toHaveText('Successfully imported 3 subscriptions from Wallos');
    await page.waitForEvent('load');
    await page.goto('/subscriptions');
    for (const name of ['Existing Sub', 'Netflix', 'Adobe CC', 'Storage Box']) {
      await expect(page.getByText(name, { exact: true })).toBeVisible();
    }

    const subs = Object.fromEntries((await allSubscriptions(request)).map((s) => [s.name, s]));
    expect(subs.Netflix).toMatchObject({
      cost: 15.99, schedule: 'Monthly', schedule_interval: 1, status: 'Active', original_currency: 'USD',
      payment_method: 'Visa', account: 'Alice', notes: 'Family plan', url: 'https://netflix.com',
      reminder_enabled: true, category: { name: 'Entertainment' },
    });
    expect(subs.Netflix.renewal_date).toMatch(/^2030-01-15/);
    expect(subs['Adobe CC']).toMatchObject({
      cost: 59.99, schedule: 'Annual', status: 'Cancelled', original_currency: 'EUR',
      category: { name: 'Productivity' },
    });
    expect(subs['Adobe CC'].cancellation_date).toMatch(/^2026-05-20/);
    expect(subs['Storage Box']).toMatchObject({
      cost: 8.5, schedule: 'Monthly', schedule_interval: 3, original_currency: 'GBP', category: { name: 'Storage' },
    });

    // New categories are created for the import.
    const categories = (await (await request.get('/api/categories')).json()).map((c) => c.name);
    expect(categories).toEqual(expect.arrayContaining(['Entertainment', 'Productivity', 'Storage']));
  });

  test('entries with notifications disabled import with reminders off', async ({ page, request }) => {
    // BUG: the Wallos parser maps "Notifications": "Disabled" to
    // ReminderEnabled=false, but the model's `gorm:"default:true"` makes GORM
    // insert true for the false zero value, so reminders end up on.
    // Remove test.fail() once imported reminders stay off.
    test.fail();
    await importWallos(page, jsonFile('wallos.json', wallosExport), 'merge');
    await expect(page.locator('#wallos-message')).toHaveText('Successfully imported 3 subscriptions from Wallos');
    const adobe = (await allSubscriptions(request)).find((s) => s.name === 'Adobe CC');
    expect(adobe.reminder_enabled).toBe(false);
  });

  test('replace mode asks first and then swaps in the Wallos data', async ({ page, request }) => {
    await seed(request, { name: 'Existing Sub' });

    page.once('dialog', (dialog) => {
      expect(dialog.message()).toBe('This will replace all existing subscription data with the Wallos import. Continue?');
      dialog.accept();
    });
    await importWallos(page, jsonFile('wallos.json', wallosExport), 'replace');
    await expect(page.locator('#wallos-message')).toHaveText('Successfully imported 3 subscriptions from Wallos');
    expect(await subscriptionNames(request)).toEqual(['Adobe CC', 'Netflix', 'Storage Box']);
  });

  test('entries without a name or price are skipped and reported', async ({ page, request }) => {
    const payload = {
      subscriptions: [
        wallosExport.subscriptions[0],
        { ...wallosExport.subscriptions[2], Name: '' },
        { ...wallosExport.subscriptions[1], Price: 'free' },
      ],
    };
    await importWallos(page, jsonFile('wallos.json', payload), 'merge');
    await expect(page.locator('#wallos-message')).toHaveText(
      'Successfully imported 1 subscriptions from Wallos (2 skipped/failed)',
    );
    expect(await subscriptionNames(request)).toEqual(['Netflix']);
  });

  test('a file that is not a Wallos export shows an error and changes nothing', async ({ page, request }) => {
    await seed(request, { name: 'Untouched' });

    await importWallos(page, jsonFile('broken.json', 'nope'), 'merge');
    await expect(page.locator('#wallos-message')).toContainText('invalid Wallos export: not valid JSON');

    await importWallos(page, jsonFile('other.json', { items: [] }), 'merge');
    await expect(page.locator('#wallos-message')).toHaveText('invalid Wallos export: missing "subscriptions" array');

    expect(await subscriptionNames(request)).toEqual(['Untouched']);
  });
});
