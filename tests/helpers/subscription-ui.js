// UI helpers for the subscriptions list and form specs (tests/subscriptions-*.spec.js).
//
// The UI language is a global server setting that other specs switch while the
// suite runs in parallel, so text assertions accept the string in any locale.
const fs = require('fs');
const path = require('path');
const { expect } = require('@playwright/test');
const { seedSubscription } = require('./subscriptions');

const localesDir = path.join(__dirname, '..', '..', 'web', 'locales');
const locales = fs.readdirSync(localesDir)
  .filter(file => file.endsWith('.json'))
  .map(file => JSON.parse(fs.readFileSync(path.join(localesDir, file), 'utf8')));

const escapeRegExp = value => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Matches the translation of `key` in any locale; `%d` is replaced by `arg`. */
function anyLocale(key, arg) {
  const values = new Set(locales.map(locale => locale[key]).filter(Boolean)
    .map(value => (arg === undefined ? value : value.replace('%d', String(arg)))));
  expect(values.size, `translation key ${key}`).toBeGreaterThan(0);
  return new RegExp(`^\\s*(${[...values].map(escapeRegExp).join('|')})\\s*$`);
}

let counter = 0;
/** A name no other test (or rerun) will produce. */
function uniqueName(prefix) {
  counter += 1;
  return `${prefix} ${Date.now().toString(36)}${process.pid.toString(36)}${counter}`;
}

async function createCategory(request, name = uniqueName('Cat')) {
  const response = await request.post('/api/categories', { data: { name } });
  expect(response.ok()).toBeTruthy();
  return await response.json();
}

/** Seeds a subscription in its own category unless `category_id` is given. */
async function seed(request, fields) {
  const form = { ...fields };
  if (!form.category_id) form.category_id = String((await createCategory(request)).id);
  return await seedSubscription(request, form);
}

/** Current server state of the subscription with this exact name, or undefined. */
async function findSubscription(request, name) {
  const response = await request.get('/api/export/json');
  expect(response.ok()).toBeTruthy();
  const { subscriptions } = await response.json();
  return subscriptions.find(subscription => subscription.name === name);
}

const list = page => page.locator('#subscription-list');
const row = (page, name) => page.locator(`#subscription-list tbody tr[data-search-name="${name}"]`);
const sortButton = (page, field) => page.locator(`#subscription-list [data-sort-field="${field}"]`);

/** Names of the given subscriptions in the order the list currently shows them. */
async function visibleOrder(page, names) {
  const shown = await page.locator('#subscription-list tbody tr[data-search-name]')
    .evaluateAll(rows => rows.map(r => r.dataset.searchName));
  return shown.filter(name => names.includes(name));
}

async function expectOrder(page, names) {
  await expect.poll(() => visibleOrder(page, names)).toEqual(names);
}

/** Waits until the list reflects `spec` (as rendered by the server). */
async function expectSortSpec(page, spec) {
  await expect(list(page)).toHaveAttribute('data-sort-spec', spec);
}

async function savedSortSpec(page) {
  const stored = await page.evaluate(() => localStorage.getItem('subtrackr-sort'));
  return stored === null ? null : JSON.parse(stored).sortSpec;
}

async function gotoSubscriptions(page, query = '') {
  await page.goto(`/subscriptions${query}`);
  await page.waitForLoadState('networkidle');
}

const modal = page => page.locator('#modal');
const form = page => page.locator('#modal-content form');

async function openAddForm(page) {
  await page.locator('main button', { hasText: anyLocale('subscriptions.add') }).click();
  await expect(modal(page)).toBeVisible();
  await expect(form(page).locator('input[name="name"]')).toBeVisible();
}

async function openEditForm(page, name) {
  await row(page, name).getByTitle(anyLocale('common.edit')).click();
  await expect(modal(page)).toBeVisible();
  await expect(form(page).locator('input[name="name"]')).toHaveValue(name);
}

/** Marks the current document so a later full reload can be detected. */
async function markDocument(page) {
  await page.evaluate(() => { window.__subtrackrTestMark = true; });
}

async function expectReloaded(page) {
  // evaluate() throws while a navigation is in progress; treat that as "not yet".
  await expect.poll(() => page.evaluate(() => window.__subtrackrTestMark === undefined).catch(() => false))
    .toBe(true);
  await page.waitForLoadState('networkidle');
}

async function expectNotReloaded(page) {
  expect(await page.evaluate(() => window.__subtrackrTestMark)).toBe(true);
}

/** Submits the open form and waits for the reload that follows a successful save. */
async function submitAndWaitForReload(page) {
  await markDocument(page);
  await form(page).locator('button[type="submit"]').click();
  await expectReloaded(page);
  await expect(modal(page)).toBeHidden();
}

module.exports = {
  anyLocale, uniqueName, createCategory, seed, findSubscription,
  list, row, sortButton, visibleOrder, expectOrder, expectSortSpec, savedSortSpec, gotoSubscriptions,
  modal, form, openAddForm, openEditForm, markDocument, expectReloaded, expectNotReloaded, submitAndWaitForReload,
};
