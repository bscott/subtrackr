// Every supported theme: choosing it on the settings page applies it at once,
// persists on the server and in localStorage, renders with the colours the
// theme promises on every page, follows the user to a fresh browser, and does
// so without console errors. Each theme is screenshotted on the dashboard and
// settings pages (test-results/ux-screenshots/) for visual review.
//
// The theme is global server state, so this spec runs on a private server
// (tests/helpers/settings-server.js).
const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');
const { saveScreenshot, collectConsole, backgroundColor } = require('./helpers/ux');

// Expected rendering per theme: the body text colour themes.css sets
// (dark-classic relies on Tailwind's dark variant instead), whether the
// Tailwind `dark` class must be on <html>, and whether the page is dark.
const THEMES = {
  default: { textColor: 'rgb(15, 23, 42)', tailwindDark: false, dark: false },
  dark: { textColor: 'rgb(228, 228, 231)', tailwindDark: false, dark: true },
  'dark-classic': { textColor: null, tailwindDark: true, dark: true },
  christmas: { textColor: 'rgb(31, 41, 55)', tailwindDark: false, dark: false },
  midnight: { textColor: 'rgb(233, 213, 255)', tailwindDark: false, dark: true },
  ocean: { textColor: 'rgb(12, 74, 110)', tailwindDark: false, dark: false },
};

function luminance(color) {
  const match = color.match(/rgba?\((\d+), (\d+), (\d+)/);
  if (!match) return null;
  const [r, g, b] = match.slice(1).map(Number);
  return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
}

/** Background the user actually sees behind the page: body, or html when body is transparent. */
async function pageBackground(page) {
  return page.evaluate(() => {
    const body = getComputedStyle(document.body);
    if (body.backgroundImage !== 'none') {
      // Gradients: sample the first colour stop.
      const stop = body.backgroundImage.match(/rgba?\([^)]+\)/);
      if (stop) return stop[0];
    }
    if (body.backgroundColor !== 'rgba(0, 0, 0, 0)') return body.backgroundColor;
    return getComputedStyle(document.documentElement).backgroundColor;
  });
}

async function expectTheme(page, name, label) {
  const expected = THEMES[name];
  await expect(page.locator('html'), `${label}: data-theme`).toHaveAttribute('data-theme', name);
  const hasDark = await page.evaluate(() => document.documentElement.classList.contains('dark'));
  expect(hasDark, `${label}: Tailwind dark class`).toBe(expected.tailwindDark);
  if (expected.textColor) {
    await expect.poll(() => page.locator('body').evaluate((el) => getComputedStyle(el).color), { message: `${label}: body text colour` })
      .toBe(expected.textColor);
  }
  // Pages fade between themes (transition-colors), and a slow browser can still
  // be fading long after network idle, so wait for the colour to arrive.
  await expect(async () => {
    const background = await pageBackground(page);
    const lum = luminance(background);
    expect(lum, `${label}: readable page background (${background})`).not.toBeNull();
    if (expected.dark) expect(lum, `${label}: dark background, got ${background}`).toBeLessThan(0.35);
    else expect(lum, `${label}: light background, got ${background}`).toBeGreaterThan(0.75);
  }).toPass({ timeout: 5_000 });
}

test.beforeAll(async ({ playwright, baseURL }) => {
  const request = await playwright.request.newContext({ baseURL });
  await seedSubscription(request, { name: 'Theme sample', cost: '12.50' });
  await request.dispose();
});

for (const name of Object.keys(THEMES)) {
  test(`${name}: applies at once and renders on every page`, async ({ page }, testInfo) => {
    const console = collectConsole(page);
    await page.goto('/settings');
    await page.locator(`.theme-option[data-theme="${name}"]`).click();
    await expectTheme(page, name, 'settings after click');
    expect(await page.evaluate(() => localStorage.getItem('subtrackr-theme'))).toBe(name);

    await expect(page.locator(`.theme-option[data-theme="${name}"]`)).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.theme-option[aria-pressed="true"]')).toHaveCount(1);

    await page.reload();
    await expectTheme(page, name, 'settings after reload');
    await expect(page.locator(`.theme-option[data-theme="${name}"]`)).toHaveClass(/active/);
    await saveScreenshot(page, `theme-${name}-${testInfo.project.name}-settings`);

    for (const path of ['/', '/subscriptions', '/analytics', '/calendar']) {
      await page.goto(path);
      await page.waitForLoadState('networkidle');
      await expectTheme(page, name, path);
      if (path === '/') await saveScreenshot(page, `theme-${name}-${testInfo.project.name}-dashboard`);
    }
    if (name === 'christmas') {
      await expect(page.locator('.snowflake').first(), 'christmas theme shows snowfall').toBeAttached();
    } else {
      await expect(page.locator('.snowflake')).toHaveCount(0);
    }
    expect(console.pageErrors, `uncaught errors with the ${name} theme`).toEqual([]);
    expect(console.errors.filter((e) => !e.includes('/api/settings/theme')), `console errors with the ${name} theme`).toEqual([]);
  });

  test(`${name}: is saved on the server and follows the user to a new browser`, async ({ page, request, browser, baseURL }) => {
    const saved = page.waitForResponse((r) => r.url().endsWith('/api/settings/theme') && r.request().method() === 'POST');
    await page.goto('/settings');
    await page.locator(`.theme-option[data-theme="${name}"]`).click();
    const response = await saved;
    expect(response.status(), `POST /api/settings/theme for ${name}: ${await response.text()}`).toBe(200);
    await expect.poll(async () => (await (await request.get('/api/settings/theme')).json()).theme).toBe(name);

    // A browser that has never seen this app gets the server-side preference.
    const fresh = await browser.newContext();
    const freshPage = await fresh.newPage();
    try {
      await freshPage.goto(`${baseURL}/`);
      await freshPage.waitForLoadState('networkidle');
      await expectTheme(freshPage, name, 'fresh browser');
    } finally {
      await fresh.close();
    }
  });
}

test('an unknown theme name is rejected by the API and the last valid theme stays', async ({ page, request }) => {
  const before = (await (await request.get('/api/settings/theme')).json()).theme;
  const response = await request.post('/api/settings/theme', { data: { theme: 'neon' } });
  expect(response.status()).toBe(400);
  expect((await (await request.get('/api/settings/theme')).json()).theme).toBe(before);
  await page.goto('/');
  await expectTheme(page, before, 'after rejected theme');
});

test('a plain page view does not rewrite the theme preference on the server', async ({ page }) => {
  const writes = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().endsWith('/api/settings/theme')) writes.push(request.url());
  });
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  await page.goto('/subscriptions');
  await page.waitForLoadState('networkidle');
  await page.goto('/settings');
  await page.waitForLoadState('networkidle');
  expect(writes, 'theme writes during plain navigation').toEqual([]);
});

test('a newer choice made in another browser wins over the theme this browser remembers', async ({ page, request, browser, baseURL }) => {
  // The server copy is the shared preference. A browser paints its remembered
  // theme first (no flash), then follows the server; it never writes its stale
  // local value back over a choice made elsewhere.
  await page.goto('/settings');
  await page.locator('.theme-option[data-theme="ocean"]').click();
  await expect.poll(async () => (await (await request.get('/api/settings/theme')).json()).theme).toBe('ocean');

  const other = await browser.newContext();
  const otherPage = await other.newPage();
  try {
    await otherPage.goto(`${baseURL}/settings`);
    await otherPage.locator('.theme-option[data-theme="midnight"]').click();
    await expect.poll(async () => (await (await request.get('/api/settings/theme')).json()).theme).toBe('midnight');

    const writes = [];
    page.on('request', (r) => {
      if (r.method() === 'POST' && r.url().endsWith('/api/settings/theme')) writes.push(r.url());
    });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await expectTheme(page, 'midnight', 'first browser follows the newer server choice');
    expect(await page.evaluate(() => localStorage.getItem('subtrackr-theme')), 'local copy updated').toBe('midnight');
    expect(writes, 'the first browser must not write its stale theme back').toEqual([]);
    expect((await (await request.get('/api/settings/theme')).json()).theme).toBe('midnight');
  } finally {
    await other.close();
  }
});

test('a browser with no stored theme follows the server on page load another theme after load', async ({ page, request, browser, baseURL }) => {
  await request.post('/api/settings/theme', { data: { theme: 'christmas' } });
  const fresh = await browser.newContext();
  const freshPage = await fresh.newPage();
  try {
    await freshPage.goto(`${baseURL}/`);
    await freshPage.waitForLoadState('networkidle');
    await expectTheme(freshPage, 'christmas', 'fresh browser');
    // Having followed the server once, the browser remembers it for the next first paint.
    expect(await freshPage.evaluate(() => localStorage.getItem('subtrackr-theme'))).toBe('christmas');
  } finally {
    await fresh.close();
  }
});
