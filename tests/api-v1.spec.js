// @ts-check
// Public API (/api/v1/*) with API key authentication.
const { test, expect } = require('@playwright/test');
const { uniqueName, daysFromNow, isoDate, fixtures, csvRecords, createAPIKey, deleteAPIKey, DAY_MS } = require('./helpers/pages');

// Each test gets its own key and category, and removes them along with any subscriptions it created.
const v1 = test.extend({
  api: async ({ request }, use) => {
    const { key, id } = await createAPIKey(request);
    const data = fixtures(request);
    const category = await data.category(uniqueName('API v1 category'));
    const headers = { 'X-API-Key': key };
    const api = {
      key, category, data,
      get: (path, options = {}) => request.get(`/api/v1${path}`, { ...options, headers: { ...headers, ...options.headers } }),
      post: (path, options = {}) => request.post(`/api/v1${path}`, { ...options, headers: { ...headers, ...options.headers } }),
      put: (path, options = {}) => request.put(`/api/v1${path}`, { ...options, headers: { ...headers, ...options.headers } }),
      async update(id, form) {
        const response = await api.put(`/subscriptions/${id}`, { form });
        expect(response.status(), await response.text()).toBe(200);
      },
      delete: (path, options = {}) => request.delete(`/api/v1${path}`, { ...options, headers: { ...headers, ...options.headers } }),
      async create(fields) {
        const response = await api.post('/subscriptions', { form: { category_id: String(category.id), cost: '9.99', schedule: 'Monthly', status: 'Active', original_currency: 'USD', ...fields } });
        expect(response.status(), await response.text()).toBe(201);
        const created = await response.json();
        data.track(created.id);
        return created;
      },
    };
    try {
      await use(api);
    } finally {
      await data.cleanup();
      await deleteAPIKey(request, id);
    }
  },
});

const ENDPOINTS = [
  ['get', '/api/v1/subscriptions'],
  ['post', '/api/v1/subscriptions'],
  ['get', '/api/v1/subscriptions/1'],
  ['put', '/api/v1/subscriptions/1'],
  ['delete', '/api/v1/subscriptions/1'],
  ['post', '/api/v1/subscriptions/1/duplicate'],
  ['get', '/api/v1/stats'],
  ['get', '/api/v1/export/csv'],
  ['get', '/api/v1/export/json'],
];

test('every /api/v1 endpoint rejects requests without a valid API key', async ({ request }) => {
  const name = uniqueName('API v1 unauthorised');
  for (const [method, path] of ENDPOINTS) {
    const missing = await request[method](path, { form: { name, cost: '1', schedule: 'Monthly', status: 'Active' } });
    expect(missing.status(), `${method} ${path} without key`).toBe(401);
    expect(await missing.json()).toEqual({ error: 'API key required' });

    const wrong = await request[method](path, { headers: { 'X-API-Key': 'sk_not_a_real_key' }, form: { name, cost: '1' } });
    expect(wrong.status(), `${method} ${path} with wrong key`).toBe(401);
    expect(await wrong.json()).toEqual({ error: 'Invalid API key' });

    const notBearer = await request[method](path, { headers: { Authorization: 'Basic abc' } });
    expect(notBearer.status(), `${method} ${path} with Basic auth`).toBe(401);
  }
  const all = await (await request.get('/api/export/json')).json();
  expect(all.subscriptions.find(s => s.name === name)).toBeUndefined();
});

v1('accepts the key as X-API-Key or a Bearer token, and stops accepting it once deleted', async ({ request }) => {
  const { key, id } = await createAPIKey(request);
  try {
    expect((await request.get('/api/v1/subscriptions', { headers: { 'X-API-Key': key } })).status()).toBe(200);
    expect((await request.get('/api/v1/subscriptions', { headers: { Authorization: `Bearer ${key}` } })).status()).toBe(200);
  } finally {
    await deleteAPIKey(request, id);
  }
  const revoked = await request.get('/api/v1/subscriptions', { headers: { Authorization: `Bearer ${key}` } });
  expect(revoked.status()).toBe(401);
  expect(await revoked.json()).toEqual({ error: 'Invalid API key' });
});

v1('lists and gets subscriptions as JSON', async ({ api }) => {
  const created = await api.create({ name: uniqueName('API v1 list'), cost: '11.5', schedule: 'Annual', renewal_date: isoDate(daysFromNow(90)), notes: 'listed', tags: 'alpha, beta' });

  const list = await api.get('/subscriptions');
  expect(list.status()).toBe(200);
  expect(list.headers()['content-type']).toMatch(/^application\/json/);
  const all = await list.json();
  expect(Array.isArray(all)).toBeTruthy();
  const listed = all.find(s => s.id === created.id);
  expect(listed).toMatchObject({ name: created.name, cost: 11.5, schedule: 'Annual', status: 'Active', notes: 'listed', category: { id: api.category.id, name: api.category.name } });
  expect(listed.tags.map(t => t.name).sort()).toEqual(['alpha', 'beta']);

  const one = await api.get(`/subscriptions/${created.id}`);
  expect(one.status()).toBe(200);
  const fetched = await one.json();
  expect(fetched).toMatchObject({ id: created.id, name: created.name, cost: 11.5, renewal_date: `${isoDate(daysFromNow(90))}T00:00:00Z`, category: { name: api.category.name } });

  const missing = await api.get('/subscriptions/999999999');
  expect(missing.status()).toBe(404);
  expect(await missing.json()).toEqual({ error: 'Subscription not found' });
  const invalid = await api.get('/subscriptions/not-a-number');
  expect(invalid.status()).toBe(400);
  expect(await invalid.json()).toEqual({ error: 'Invalid ID' });
});

v1('creates a subscription from form fields and fills in defaults', async ({ api }) => {
  const name = uniqueName('API v1 create');
  const before = Date.now();
  const created = await api.create({ name, cost: '4.25', schedule: 'Monthly', payment_method: 'PayPal', account: 'acct', url: '', notes: 'n', usage: 'Low', label: 'family', cancellation_notice_days: '3' });
  expect(created).toMatchObject({
    name, cost: 4.25, schedule: 'Monthly', status: 'Active', category_id: api.category.id, original_currency: 'USD',
    payment_method: 'PayPal', account: 'acct', notes: 'n', usage: 'Low', label: 'family', cancellation_notice_days: 3,
    schedule_interval: 1, share_count: 1, reminder_enabled: true, autopay: null,
  });
  expect(created.id).toBeGreaterThan(0);
  // An active subscription without a renewal date renews one month from now.
  const renewal = Date.parse(created.renewal_date);
  expect(renewal).toBeGreaterThan(before + 27 * DAY_MS);
  expect(renewal).toBeLessThan(Date.now() + 32 * DAY_MS);

  const fetched = await (await api.get(`/subscriptions/${created.id}`)).json();
  expect(fetched).toMatchObject({ name, cost: 4.25, label: 'family', category: { name: api.category.name } });
});

v1('create response includes the subscription\'s category', async ({ api }) => {
  // BUG: POST returns the subscription before reloading it, so `category` is an empty
  // {"id":0,"name":""} object while GET returns the real category.
  v1.fail();
  const created = await api.create({ name: uniqueName('API v1 create category') });
  expect(created.category).toMatchObject({ id: api.category.id, name: api.category.name });
});

v1('creates a subscription from the JSON body shown in the README', async ({ api }) => {
  // BUG: README documents `Content-Type: application/json` bodies for POST/PUT /api/v1/subscriptions,
  // but the handlers only read form fields, so a JSON body is ignored and the request fails with
  // 400 "FOREIGN KEY constraint failed" (or updates nothing).
  v1.fail();
  const name = uniqueName('API v1 json');
  const response = await api.post('/subscriptions', { data: { name, cost: 15.99, schedule: 'Monthly', status: 'Active', category_id: api.category.id } });
  if (response.status() === 201) api.data.track((await response.json()).id);
  expect(response.status()).toBe(201);
  expect(await response.json()).toMatchObject({ name, cost: 15.99 });
});

const INVALID_CREATES = [
  ['an empty name', { name: '' }],
  ['a zero cost', { cost: '0' }],
  ['a negative cost', { cost: '-5' }],
  ['an unknown schedule', { schedule: 'Fortnightly' }],
  ['an unknown status', { status: 'Bogus' }],
  ['an unknown usage', { usage: 'Sometimes' }],
];
for (const [label, fields] of INVALID_CREATES) {
  v1(`rejects creating a subscription with ${label}`, async ({ api }) => {
    // BUG: models.Subscription declares `validate` rules (required name, cost > 0, schedule/status/usage
    // enums) but nothing enforces them, so these are stored and skew totals and reminders.
    v1.fail();
    const response = await api.post('/subscriptions', { form: { name: uniqueName('API v1 invalid'), cost: '5', schedule: 'Monthly', status: 'Active', category_id: String(api.category.id), ...fields } });
    if (response.status() === 201) api.data.track((await response.json()).id);
    expect(response.status()).toBe(400);
  });
}

v1('rejects updating a subscription to an unknown status', async ({ api }) => {
  // BUG: same missing validation on update; status "Bogus" is saved and the subscription
  // silently drops out of every Active/Cancelled total.
  v1.fail();
  const created = await api.create({ name: uniqueName('API v1 invalid update') });
  const response = await api.put(`/subscriptions/${created.id}`, { form: { status: 'Bogus' } });
  expect(response.status()).toBe(400);
  expect((await (await api.get(`/subscriptions/${created.id}`)).json()).status).toBe('Active');
});

v1('updates all fields, or only the fields sent', async ({ api, request }) => {
  const other = await api.data.category(uniqueName('API v1 other category'));
  const created = await api.create({
    name: uniqueName('API v1 update'), cost: '10', schedule: 'Monthly', renewal_date: isoDate(daysFromNow(20)),
    notes: 'keep me', payment_method: 'Visa', account: 'a@b.c', usage: 'High', tags: 'one',
  });

  // Partial update: only cost changes.
  await api.update(created.id, { cost: '12.5' });
  let current = await (await api.get(`/subscriptions/${created.id}`)).json();
  expect(current).toMatchObject({
    name: created.name, cost: 12.5, schedule: 'Monthly', status: 'Active', notes: 'keep me', payment_method: 'Visa',
    account: 'a@b.c', usage: 'High', category_id: api.category.id, renewal_date: created.renewal_date,
  });
  expect(current.tags.map(t => t.name)).toEqual(['one']);

  // Sending a field empty clears it; omitted fields stay.
  await api.update(created.id, { notes: '', tags: '' });
  current = await (await api.get(`/subscriptions/${created.id}`)).json();
  expect(current).toMatchObject({ notes: '', cost: 12.5, payment_method: 'Visa' });
  expect(current.tags).toEqual([]);

  // Full update.
  const newName = uniqueName('API v1 updated');
  const renewal = isoDate(daysFromNow(200));
  await api.update(created.id, {
    name: newName, cost: '99', schedule: 'Annual', status: 'Cancelled', category_id: String(other.id),
    original_currency: 'GBP', renewal_date: renewal, cancellation_date: isoDate(daysFromNow(1)), notes: 'changed',
    payment_method: 'Cash', account: 'x', usage: 'Low', cancellation_notice_days: '30', share_count: '2',
  });
  current = await (await api.get(`/subscriptions/${created.id}`)).json();
  expect(current).toMatchObject({
    name: newName, cost: 99, schedule: 'Annual', status: 'Cancelled', category: { id: other.id, name: other.name },
    original_currency: 'GBP', renewal_date: `${renewal}T00:00:00Z`, notes: 'changed', payment_method: 'Cash',
    account: 'x', usage: 'Low', cancellation_notice_days: 30, share_count: 2,
  });

  // The change is visible through the session API as well.
  const viaWeb = await (await request.get(`/api/subscriptions/${created.id}`)).json();
  expect(viaWeb.name).toBe(newName);

  const missing = await api.put('/subscriptions/999999999', { form: { cost: '1' } });
  expect(missing.status()).toBe(404);
  expect(await missing.json()).toEqual({ error: 'Subscription not found' });
});

v1('duplicates a subscription with its fields and tags', async ({ api }) => {
  const original = await api.create({ name: uniqueName('API v1 dup'), cost: '7.77', schedule: 'Weekly', notes: 'dup notes', tags: 'red, blue', cancellation_notice_days: '2', share_count: '2' });
  const response = await api.post(`/subscriptions/${original.id}/duplicate`);
  expect(response.status()).toBe(201);
  const copy = await response.json();
  api.data.track(copy.id);
  expect(copy.id).not.toBe(original.id);
  expect(copy).toMatchObject({ name: `${original.name} (Copy)`, cost: 7.77, schedule: 'Weekly', notes: 'dup notes', cancellation_notice_days: 2, share_count: 2, category_id: api.category.id });

  const fetched = await (await api.get(`/subscriptions/${copy.id}`)).json();
  expect(fetched.tags.map(t => t.name).sort()).toEqual(['blue', 'red']);
  // The original is unchanged.
  expect((await (await api.get(`/subscriptions/${original.id}`)).json()).name).toBe(original.name);

  expect((await api.post('/subscriptions/999999999/duplicate')).status()).toBeGreaterThanOrEqual(400);
});

v1('deletes a subscription', async ({ api }) => {
  const created = await api.create({ name: uniqueName('API v1 delete') });
  const response = await api.delete(`/subscriptions/${created.id}`);
  expect(response.status()).toBe(200);
  expect((await api.get(`/subscriptions/${created.id}`)).status()).toBe(404);
  const all = await (await api.get('/subscriptions')).json();
  expect(all.find(s => s.id === created.id)).toBeUndefined();
  expect((await api.delete('/subscriptions/not-a-number')).status()).toBe(400);
});

v1('stats reflect the subscriptions created through the API', async ({ api }) => {
  // 20/month + 60/year (5/month) + cancelled 10/month (saved) + paused ignored
  await api.create({ name: uniqueName('API v1 stats monthly'), cost: '20' });
  await api.create({ name: uniqueName('API v1 stats annual'), cost: '60', schedule: 'Annual' });
  await api.create({ name: uniqueName('API v1 stats cancelled'), cost: '10', status: 'Cancelled' });
  await api.create({ name: uniqueName('API v1 stats paused'), cost: '1000', status: 'Paused' });

  const response = await api.get('/stats');
  expect(response.status()).toBe(200);
  const stats = await response.json();
  for (const field of ['total_monthly_spend', 'total_annual_spend', 'active_subscriptions', 'cancelled_subscriptions', 'total_saved', 'monthly_saved', 'upcoming_renewals']) {
    expect(typeof stats[field], field).toBe('number');
  }
  expect(typeof stats.conversion_complete).toBe('boolean');
  expect(stats.category_spending_by_currency[api.category.name]).toEqual({ USD: expect.closeTo(25, 6) });
  if (stats.conversion_complete) expect(stats.category_spending[api.category.name]).toBeCloseTo(25, 6);
  expect(stats.totals_by_currency.USD.active_subscriptions).toBeGreaterThanOrEqual(2);
  expect(stats.totals_by_currency.USD.cancelled_subscriptions).toBeGreaterThanOrEqual(1);
  expect(stats.totals_by_currency.USD.monthly_saved).toBeGreaterThanOrEqual(10 - 1e-9);
});

v1('exports CSV and JSON with an API key', async ({ api }) => {
  const created = await api.create({ name: uniqueName('API v1 export, "quoted"'), cost: '3.3', schedule: 'Daily' });

  const csv = await api.get('/export/csv');
  expect(csv.status()).toBe(200);
  expect(csv.headers()['content-type']).toMatch(/^text\/csv/);
  expect(csv.headers()['content-disposition']).toBe('attachment; filename=subscriptions.csv');
  const { records } = csvRecords(await csv.text());
  expect(records.find(r => r.ID === String(created.id))).toMatchObject({ Name: created.name, Cost: '3.30', Schedule: 'Daily', Category: api.category.name });

  const json = await api.get('/export/json');
  expect(json.status()).toBe(200);
  expect(json.headers()['content-disposition']).toBe('attachment; filename=subscriptions.json');
  const body = await json.json();
  expect(body.total_count).toBe(body.subscriptions.length);
  expect(body.subscriptions.find(s => s.id === created.id)).toMatchObject({ name: created.name, cost: 3.3 });

  // iCal export and backup are not part of the public API.
  expect((await api.get('/export/ical')).status()).toBe(404);
});
