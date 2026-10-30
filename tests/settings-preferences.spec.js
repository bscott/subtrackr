// @ts-check
// Settings page: notification preferences, currency, date format, language and theme.
// Every test restores the settings it changes.
const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');

test.describe.configure({ mode: 'serial' });

async function notificationSettings(request) {
  const response = await request.get('/api/settings/notifications');
  expect(response.ok()).toBeTruthy();
  return response.json();
}

/** Flip a boolean notification setting back if a test left it changed. */
async function restoreToggle(request, setting, field, wanted) {
  if ((await notificationSettings(request))[field] !== wanted) {
    await request.post(`/api/settings/notifications/${setting}`);
  }
  expect((await notificationSettings(request))[field]).toBe(wanted);
}

/** Change an hx-trigger="change" input the way a user does and wait for the save. */
async function changeInput(page, locator, value, path) {
  const saved = page.waitForResponse((r) => r.url().endsWith(path) && r.request().method() === 'POST');
  await locator.fill(value);
  await locator.press('Tab');
  return saved;
}

test.describe('notification preferences', () => {
  test('renewal reminder toggle switches on and off and persists', async ({ page, request }) => {
    const original = (await notificationSettings(request)).renewal_reminders;
    const toggle = page.locator('#renewal-toggle');
    try {
      await page.goto('/settings');
      for (const wanted of [!original, original]) {
        await toggle.click();
        await expect(toggle).toHaveClass(wanted ? /bg-primary/ : /bg-gray-200/);
        await expect.poll(async () => (await notificationSettings(request)).renewal_reminders).toBe(wanted);
        await page.reload();
        await expect(toggle).toHaveClass(wanted ? /bg-primary/ : /bg-gray-200/);
      }
    } finally {
      await restoreToggle(request, 'renewal', 'renewal_reminders', original);
    }
  });

  test('high-cost alert toggle switches off and on and persists', async ({ page, request }) => {
    const original = (await notificationSettings(request)).high_cost_alerts;
    const toggle = page.locator('#highcost-toggle');
    try {
      await page.goto('/settings');
      for (const wanted of [!original, original]) {
        await toggle.click();
        await expect(toggle).toHaveClass(wanted ? /bg-primary/ : /bg-gray-200/);
        await expect.poll(async () => (await notificationSettings(request)).high_cost_alerts).toBe(wanted);
        await page.reload();
        await expect(toggle).toHaveClass(wanted ? /bg-primary/ : /bg-gray-200/);
      }
    } finally {
      await restoreToggle(request, 'highcost', 'high_cost_alerts', original);
    }
  });

  test('cancellation reminder toggle is saved and shown after reload', async ({ page, request }) => {
    const original = (await notificationSettings(request)).cancellation_reminders;
    const toggle = page.locator('#cancellation-toggle');
    try {
      await page.goto('/settings');
      await toggle.click();
      await expect.poll(async () => (await notificationSettings(request)).cancellation_reminders).toBe(!original);
      await page.reload();
      await expect(toggle).toHaveClass(!original ? /bg-primary/ : /bg-gray-200/);
    } finally {
      await restoreToggle(request, 'cancellation', 'cancellation_reminders', original);
    }
  });

  test('cancellation reminder toggle updates as soon as it is clicked', async ({ page, request }) => {
    const original = (await notificationSettings(request)).cancellation_reminders;
    const toggle = page.locator('#cancellation-toggle');
    try {
      await page.goto('/settings');
      await toggle.click();
      await expect.poll(async () => (await notificationSettings(request)).cancellation_reminders).toBe(!original);
      await expect(toggle).toHaveClass(!original ? /bg-primary/ : /bg-gray-200/);
    } finally {
      await restoreToggle(request, 'cancellation', 'cancellation_reminders', original);
    }
  });

  test('high-cost threshold is saved on change', async ({ page, request }) => {
    const original = (await notificationSettings(request)).high_cost_threshold;
    const input = page.locator('input[name="high_cost_threshold"]');
    try {
      await page.goto('/settings');
      const response = await changeInput(page, input, '125.5', '/api/settings/notifications/threshold');
      expect(response.status()).toBe(200);
      expect((await notificationSettings(request)).high_cost_threshold).toBe(125.5);
      await page.reload();
      expect(Number(await input.inputValue())).toBe(125.5);
    } finally {
      await request.post('/api/settings/notifications/threshold', { form: { high_cost_threshold: String(original) } });
      expect((await notificationSettings(request)).high_cost_threshold).toBe(original);
    }
  });

  test('renewal reminder days are saved, and out-of-range values are rejected', async ({ page, request }) => {
    const original = (await notificationSettings(request)).reminder_days;
    const input = page.locator('input[name="reminder_days"]');
    try {
      await page.goto('/settings');
      expect((await changeInput(page, input, '12', '/api/settings/notifications/days')).status()).toBe(200);
      expect((await changeInput(page, input, '45', '/api/settings/notifications/days')).status()).toBe(400);
      await page.reload();
      await expect(input).toHaveValue('12');
      expect((await notificationSettings(request)).reminder_days).toBe(12);
    } finally {
      await request.post('/api/settings/notifications/days', { form: { reminder_days: String(original) } });
      expect((await notificationSettings(request)).reminder_days).toBe(original);
    }
  });

  test('cancellation reminder days are saved', async ({ page, request }) => {
    const original = (await notificationSettings(request)).cancellation_reminder_days;
    const input = page.locator('input[name="cancellation_reminder_days"]');
    try {
      await page.goto('/settings');
      expect((await changeInput(page, input, '4', '/api/settings/notifications/cancellation_days')).status()).toBe(200);
      await page.reload();
      await expect(input).toHaveValue('4');
      expect((await notificationSettings(request)).cancellation_reminder_days).toBe(4);
    } finally {
      await request.post('/api/settings/notifications/cancellation_days', { form: { cancellation_reminder_days: String(original) } });
      expect((await notificationSettings(request)).cancellation_reminder_days).toBe(original);
    }
  });

  for (const { name, field, path } of [
    { name: 'renewal', field: 'reminder_days_list', path: '/api/settings/notifications/days_list' },
    { name: 'cancellation', field: 'cancellation_reminder_days_list', path: '/api/settings/notifications/cancellation_days_list' },
  ]) {
    test(`${name} reminder windows are saved in canonical order`, async ({ page, request }) => {
      const input = page.locator(`input[name="${field}"]`);
      await page.goto('/settings');
      const original = await input.inputValue();
      try {
        // Duplicates and invalid entries are dropped; days are sorted descending.
        expect((await changeInput(page, input, '3, 14,0,3,x,-2', path)).status()).toBe(200);
        await page.reload();
        await expect(input).toHaveValue('14,3,0');

        // More than ten windows is rejected and the saved list is kept.
        expect((await changeInput(page, input, '1,2,3,4,5,6,7,8,9,10,11', path)).status()).toBe(400);
        await page.reload();
        await expect(input).toHaveValue('14,3,0');
      } finally {
        await request.post(path, { form: { [field]: original } });
        await page.reload();
        await expect(input).toHaveValue(original);
      }
    });
  }
});

test.describe('currency', () => {
  test('choosing a currency updates the settings page and the dashboard', async ({ page, request }) => {
    await page.goto('/settings');
    const original = await page.locator('input[name="currency"]:checked').getAttribute('value');
    expect(original).toBe('USD');
    try {
      const saved = page.waitForResponse((r) => r.url().endsWith('/api/settings/currency'));
      await page.locator('input[name="currency"][value="EUR"]').check();
      expect((await saved).status()).toBe(200);

      await page.reload();
      await expect(page.locator('input[name="currency"][value="EUR"]')).toBeChecked();
      await expect(page.locator('input[name="high_cost_threshold"]').locator('xpath=preceding-sibling::span')).toHaveText('€');

      await page.goto('/');
      await expect(page.getByText('€0.00').first()).toBeVisible();
      await expect(page.getByText('$0.00')).toHaveCount(0);
    } finally {
      await request.post('/api/settings/currency', { form: { currency: String(original) } });
    }
    await page.goto('/settings');
    await expect(page.locator('input[name="currency"][value="USD"]')).toBeChecked();
  });
});

test.describe('date format', () => {
  test('choosing a date format changes how renewal dates are shown', async ({ page, request }) => {
    const sub = await seedSubscription(request, { name: `Date format ${Date.now()}`, renewal_date: '2027-03-04' });
    const row = page.locator('tr', { hasText: sub.name });
    await page.goto('/settings');
    const original = await page.locator('input[name="date_format"]:checked').getAttribute('value');
    try {
      await page.goto('/subscriptions');
      await expect(row).toContainText('03/04/2027');

      for (const [format, shown] of [['DD/MM/YYYY', '04/03/2027'], ['YYYY-MM-DD', '2027-03-04']]) {
        await page.goto('/settings');
        const saved = page.waitForResponse((r) => r.url().endsWith('/api/settings/date-format'));
        await page.locator(`input[name="date_format"][value="${format}"]`).check();
        expect((await saved).status()).toBe(200);
        await page.reload();
        await expect(page.locator(`input[name="date_format"][value="${format}"]`)).toBeChecked();
        await page.goto('/subscriptions');
        await expect(row).toContainText(shown);
      }
    } finally {
      await request.post('/api/settings/date-format', { form: { date_format: String(original) } });
      await request.delete(`/api/subscriptions/${sub.id}`);
    }
    await page.goto('/settings');
    await expect(page.locator(`input[name="date_format"][value="${original}"]`)).toBeChecked();
  });
});

test.describe('language', () => {
  test('the language selector reloads the page in the chosen language', async ({ page, request }) => {
    await page.goto('/settings');
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    try {
      await Promise.all([
        page.waitForEvent('load'),
        page.locator('#language-select').selectOption('es'),
      ]);
      await expect(page.locator('html')).toHaveAttribute('lang', 'es');
      await expect(page.locator('#language-select')).toHaveValue('es');
      await expect(page.getByRole('link', { name: /Panel/ }).first()).toBeVisible();
    } finally {
      await request.post('/api/settings/language', { data: { lang: 'en' } });
    }
    await page.goto('/settings');
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  });
});

test.describe('theme', () => {
  async function serverTheme(request) {
    return (await (await request.get('/api/settings/theme')).json()).theme;
  }

  async function selectTheme(page, theme) {
    const saved = page.waitForResponse((r) => r.url().endsWith('/api/settings/theme') && r.request().method() === 'POST');
    await page.locator(`.theme-option[data-theme="${theme}"]`).click();
    return saved;
  }

  test('a chosen theme applies at once and follows the user to a new browser', async ({ page, request, browser, baseURL }) => {
    const original = await serverTheme(request);
    try {
      await page.goto('/settings');
      expect((await selectTheme(page, 'ocean')).status()).toBe(200);
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'ocean');
      await expect(page.locator('.theme-option[data-theme="ocean"]')).toHaveClass(/active/);
      await expect(page.locator('.theme-option.active')).toHaveCount(1);

      const fresh = await browser.newContext({ baseURL });
      try {
        const other = await fresh.newPage();
        await other.goto('/');
        await expect(other.locator('html')).toHaveAttribute('data-theme', 'ocean');
        await other.goto('/settings');
        await expect(other.locator('html')).toHaveAttribute('data-theme', 'ocean');
      } finally {
        await fresh.close();
      }
    } finally {
      await request.post('/api/settings/theme', { data: { theme: original } });
    }
  });

  test('the settings page highlights the saved theme after reload', async ({ page, request }) => {
    // App bug, unrelated to HTMX: the DOMContentLoaded handler in settings.html
    // marks document.querySelector('[data-theme="ocean"]') as active, which is
    // <html data-theme="ocean"> rather than the theme card, so no card is
    // highlighted once the theme is applied.
    test.fail();
    const original = await serverTheme(request);
    try {
      await page.goto('/settings');
      expect((await selectTheme(page, 'ocean')).status()).toBe(200);
      await page.reload();
      await expect(page.locator('.theme-option[data-theme="ocean"]')).toHaveClass(/active/);
    } finally {
      await request.post('/api/settings/theme', { data: { theme: original } });
    }
  });

  test('Dark Classic turns dark mode on and Default turns it off', async ({ page, request }) => {
    const original = await serverTheme(request);
    try {
      await page.goto('/settings');
      await page.locator('.theme-option[data-theme="dark-classic"]').click();
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark-classic');
      await expect(page.locator('html')).toHaveClass(/\bdark\b/);
      await page.reload();
      await expect(page.locator('html')).toHaveClass(/\bdark\b/);

      expect((await selectTheme(page, 'default')).status()).toBe(200);
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'default');
      await expect(page.locator('html')).not.toHaveClass(/\bdark\b/);
      await page.reload();
      await expect(page.locator('html')).not.toHaveClass(/\bdark\b/);
    } finally {
      await request.post('/api/settings/theme', { data: { theme: original } });
    }
  });

  test('Dark Classic follows the user to a new browser', async ({ page, request, browser, baseURL }) => {
    // App bug, unrelated to HTMX: SetTheme in internal/handlers/settings.go
    // rejects "dark-classic" (400 Invalid theme name), so the choice is only
    // kept in this browser's localStorage.
    test.fail();
    const original = await serverTheme(request);
    try {
      await page.goto('/settings');
      await page.locator('.theme-option[data-theme="dark-classic"]').click();
      await expect.poll(() => serverTheme(request)).toBe('dark-classic');

      const fresh = await browser.newContext({ baseURL });
      try {
        const other = await fresh.newPage();
        await other.goto('/');
        await expect(other.locator('html')).toHaveAttribute('data-theme', 'dark-classic');
        await expect(other.locator('html')).toHaveClass(/\bdark\b/);
      } finally {
        await fresh.close();
      }
    } finally {
      await request.post('/api/settings/theme', { data: { theme: original } });
    }
  });
});
