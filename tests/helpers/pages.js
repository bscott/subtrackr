// Helpers for the read-only page, export and public API specs (pages-*.spec.js, api-v1*.spec.js).
// They talk to the app only through HTTP and rendered text, so they do not depend on htmx.
const { expect } = require('@playwright/test');

const DAY_MS = 24 * 60 * 60 * 1000;

function uniqueName(prefix) {
  return `${prefix} ${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

// YYYY-MM-DD for a UTC date, which is how the app stores and keys renewal dates.
function isoDate(date) {
  return date.toISOString().slice(0, 10);
}

function utcDate(year, month, day) {
  return new Date(Date.UTC(year, month - 1, day));
}

function daysFromNow(days) {
  const now = new Date();
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + days));
}

async function createCategory(request, name = uniqueName('Pages category')) {
  const response = await request.post('/api/categories', { data: { name } });
  expect(response.ok()).toBeTruthy();
  return await response.json();
}

// Creates a subscription through the HTMX form endpoint (no HX-Request header, so it returns JSON).
async function createSubscription(request, fields) {
  const response = await request.post('/api/subscriptions', { form: {
    cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
    ...fields,
  } });
  expect(response.status(), await response.text()).toBe(201);
  return await response.json();
}

// Returns a tracker that deletes everything it created, subscriptions before categories.
function fixtures(request) {
  const subscriptionIDs = [];
  const categoryIDs = [];
  return {
    async category(name) {
      const category = await createCategory(request, name);
      categoryIDs.push(category.id);
      return category;
    },
    async subscription(fields) {
      const subscription = await createSubscription(request, fields);
      subscriptionIDs.push(subscription.id);
      return subscription;
    },
    track(id) {
      subscriptionIDs.push(id);
    },
    async cleanup() {
      await Promise.all(subscriptionIDs.map(id => request.delete(`/api/subscriptions/${id}`)));
      for (const id of categoryIDs) await request.delete(`/api/categories/${id}`);
    },
  };
}

// Independent restatement of the documented schedule conversions:
// a month is 4.33 weeks or 30.44 days, a quarter is 3 months, and costs are split by share_count.
function monthlyCost(sub) {
  const interval = sub.schedule_interval > 0 ? sub.schedule_interval : 1;
  const share = sub.share_count > 0 ? sub.share_count : 1;
  const perPeriod = { Annual: 1 / 12, Quarterly: 1 / 3, Monthly: 1, Weekly: 4.33, Daily: 30.44 }[sub.schedule] ?? 1;
  return sub.cost * perPeriod / interval / share;
}

function annualCost(sub) {
  const interval = sub.schedule_interval > 0 ? sub.schedule_interval : 1;
  const share = sub.share_count > 0 ? sub.share_count : 1;
  const perYear = { Annual: 1, Quarterly: 4, Monthly: 12, Weekly: 52, Daily: 365 }[sub.schedule] ?? 12;
  return sub.cost * perYear / interval / share;
}

async function allSubscriptions(request) {
  const response = await request.get('/api/export/json');
  expect(response.ok()).toBeTruthy();
  return (await response.json()).subscriptions;
}

function snapshotKey(subscriptions) {
  return JSON.stringify(subscriptions.map(s => [
    s.id, s.updated_at, s.status, s.cost, s.schedule, s.schedule_interval, s.share_count,
    s.original_currency, s.renewal_date, s.category && s.category.name,
  ]).sort());
}

// Runs `observe` between two reads of every subscription and retries until no other test changed
// the data in between, so global totals can be checked against a consistent snapshot.
async function withStableSnapshot(request, observe, check) {
  await expect(async () => {
    const before = await allSubscriptions(request);
    const observed = await observe();
    const after = await allSubscriptions(request);
    expect(snapshotKey(after), 'data changed while the page loaded; retrying').toBe(snapshotKey(before));
    check(before, observed);
  }).toPass({ timeout: 60_000, intervals: [250, 500, 1000] });
}

// Totals per original currency, computed from raw subscriptions.
function totalsByCurrency(subscriptions) {
  const totals = {};
  for (const sub of subscriptions) {
    const currency = sub.original_currency;
    totals[currency] ??= { monthly: 0, annual: 0, active: 0, cancelled: 0, saved: 0, monthlySaved: 0 };
    const t = totals[currency];
    if (sub.status === 'Active') {
      t.monthly += monthlyCost(sub);
      t.annual += annualCost(sub);
      t.active++;
    } else if (sub.status === 'Cancelled') {
      t.saved += annualCost(sub);
      t.monthlySaved += monthlyCost(sub);
      t.cancelled++;
    }
  }
  return totals;
}

function upcomingRenewalCount(subscriptions, now = Date.now()) {
  return subscriptions.filter(s => s.status === 'Active' && s.renewal_date &&
    Date.parse(s.renewal_date) >= now && Date.parse(s.renewal_date) <= now + 7 * DAY_MS).length;
}

function parseAmount(text) {
  const match = text.replace(/\s+/g, ' ').trim().match(/-?\d+(?:\.\d+)?/);
  expect(match, `no amount in "${text}"`).not.toBeNull();
  const decimals = (match[0].split('.')[1] || '').length;
  return { value: Number(match[0]), tolerance: 0.5 * 10 ** -decimals + 1e-6 };
}

function expectAmount(text, expected, label) {
  const { value, tolerance } = parseAmount(text);
  expect(Math.abs(value - expected), `${label}: shown "${text.trim()}", expected ≈ ${expected}`).toBeLessThanOrEqual(tolerance);
}

// A spend card shows one converted total ("$12.34") when every currency converts, otherwise one
// "12.34 USD" line per currency that has subscriptions counted by `countField`.
function expectSpendCard(lines, totals, field, countField, label) {
  const perCurrency = lines.map(line => line.trim().match(/^(-?\d+(?:\.\d+)?) ([A-Z]{3})$/));
  if (lines.length > 0 && perCurrency.every(Boolean)) {
    const shown = Object.fromEntries(perCurrency.map(m => [m[2], m[0]]));
    const expectedCurrencies = Object.keys(totals).filter(c => totals[c][countField] > 0).sort();
    expect(Object.keys(shown).sort(), `${label}: currencies`).toEqual(expectedCurrencies);
    for (const currency of expectedCurrencies) expectAmount(shown[currency], totals[currency][field], `${label} ${currency}`);
    return;
  }
  expect(lines, `${label}: expected a single converted total`).toHaveLength(1);
  // Without exchange rates the app only shows one converted total when all data shares a currency.
  expect(Object.keys(totals).length, `${label}: single total with mixed currencies`).toBeLessThanOrEqual(1);
  const expected = Object.values(totals).reduce((sum, t) => sum + t[field], 0);
  expectAmount(lines[0], expected, label);
}

// Minimal RFC 4180 parser so tests can check quoting rather than substring matches.
function parseCSV(text) {
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"' && text[i + 1] === '"') { field += '"'; i++; }
      else if (ch === '"') quoted = false;
      else field += ch;
    } else if (ch === '"') quoted = true;
    else if (ch === ',') { row.push(field); field = ''; }
    else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      row.push(field); rows.push(row); row = []; field = '';
    } else field += ch;
  }
  if (field !== '' || row.length > 0) { row.push(field); rows.push(row); }
  return rows;
}

function csvRecords(text) {
  const [header, ...rows] = parseCSV(text);
  return { header, records: rows.map(r => Object.fromEntries(header.map((h, i) => [h, r[i]]))) };
}

// Unfolds an iCalendar body into VEVENT property maps.
function icalEvents(text) {
  const lines = text.replace(/\r\n[ \t]/g, '').split('\r\n');
  const events = [];
  let current = null;
  for (const line of lines) {
    if (line === 'BEGIN:VEVENT') current = {};
    else if (line === 'END:VEVENT') { events.push(current); current = null; }
    else if (current) {
      const idx = line.indexOf(':');
      current[line.slice(0, idx)] = line.slice(idx + 1);
    }
  }
  return events;
}

// Creates an API key through the settings endpoint and returns its value and ID.
async function createAPIKey(request, name = uniqueName('Pages API key')) {
  const response = await request.post('/api/settings/apikeys', { form: { name } });
  expect(response.ok()).toBeTruthy();
  const html = await response.text();
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const block = html.match(new RegExp(`>${escaped}</h5>[\\s\\S]*?(sk_[0-9a-f]{64})[\\s\\S]*?hx-delete="/api/settings/apikeys/(\\d+)"`));
  expect(block, 'new API key not shown in the response').not.toBeNull();
  return { name, key: block[1], id: Number(block[2]) };
}

async function deleteAPIKey(request, id) {
  await request.delete(`/api/settings/apikeys/${id}`);
}

module.exports = {
  DAY_MS, uniqueName, isoDate, utcDate, daysFromNow, createCategory, createSubscription, fixtures,
  monthlyCost, annualCost, allSubscriptions, withStableSnapshot, totalsByCurrency, upcomingRenewalCount,
  parseAmount, expectAmount, expectSpendCard, parseCSV, csvRecords, icalEvents, createAPIKey, deleteAPIKey,
};
