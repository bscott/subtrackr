// Every page loads without browser console errors, uncaught exceptions or
// failed requests, in each desktop engine. Warnings are attached to the test
// as annotations so the report shows them without failing the build.
//
// Runs against the shared server and only opens pages and the add form, so it
// is safe next to the other specs.
const { test, expect } = require('@playwright/test');
const { collectConsole } = require('./helpers/ux');
const { openAddForm, gotoSubscriptions, modal, seed, uniqueName } = require('./helpers/subscription-ui');

const PAGES = ['/', '/subscriptions', '/analytics', '/calendar', '/settings', '/login', '/forgot-password'];

// Tailwind is loaded from its CDN build, which warns about production use on
// every page. The warning is recorded, not treated as a failure.
const KNOWN_WARNING = /cdn\.tailwindcss\.com should not be used in production/;

function attach(testInfo, report) {
  for (const warning of report.warnings) {
    testInfo.annotations.push({ type: KNOWN_WARNING.test(warning) ? 'known-warning' : 'console-warning', description: warning });
  }
}

for (const path of PAGES) {
  test(`${path} renders without console errors, uncaught exceptions or failed requests`, async ({ page }, testInfo) => {
    const console = collectConsole(page);
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    attach(testInfo, console.report());
    expect(console.pageErrors, 'uncaught exceptions').toEqual([]);
    expect(console.errors, 'console.error output').toEqual([]);
    expect(console.failedRequests, 'failed or 4xx/5xx sub-requests').toEqual([]);
  });
}

test('opening and closing the subscription form is clean', async ({ page }, testInfo) => {
  const console = collectConsole(page);
  await gotoSubscriptions(page);
  await openAddForm(page);
  await page.keyboard.press('Escape');
  await expect(modal(page)).toBeHidden();
  attach(testInfo, console.report());
  expect(console.pageErrors).toEqual([]);
  expect(console.errors).toEqual([]);
  expect(console.failedRequests).toEqual([]);
});

test('typing in the subscriptions search and sorting is clean', async ({ page, request }, testInfo) => {
  // Sort headers only render when the list has rows.
  const sub = await seed(request, { name: uniqueName('Console sort'), cost: '1' });
  const console = collectConsole(page);
  try {
    await gotoSubscriptions(page);
    await page.locator('#subscription-list [data-sort-field="cost"]').click();
    await page.waitForLoadState('networkidle');
    await page.locator('#subscription-search').fill('zz-no-such-subscription');
    await expect(page.locator('#subscription-search-empty')).toBeVisible();
  } finally {
    await request.delete(`/api/subscriptions/${sub.id}`);
  }
  attach(testInfo, console.report());
  expect(console.pageErrors).toEqual([]);
  expect(console.errors).toEqual([]);
  expect(console.failedRequests).toEqual([]);
});
