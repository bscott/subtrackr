// @ts-check
// Dashboard (/ and /dashboard): stats cards, spending by category and the subscription list.
// Assertions avoid translated labels because other specs switch the UI language on the shared server.
const { test, expect } = require('@playwright/test');
const {
  uniqueName, daysFromNow, isoDate, fixtures, monthlyCost, withStableSnapshot, totalsByCurrency,
  expectSpendCard, expectAmount,
} = require('./helpers/pages');

// Mixed schedules whose monthly costs are easy to check by hand.
const MIXED_SCHEDULES = [
  { cost: '10', schedule: 'Monthly' },                         // 10.00 / month
  { cost: '120', schedule: 'Annual' },                         // 10.00
  { cost: '5', schedule: 'Weekly' },                           // 21.65 (4.33 weeks)
  { cost: '1', schedule: 'Daily' },                            // 30.44 (30.44 days)
  { cost: '30', schedule: 'Quarterly' },                       // 10.00
  { cost: '20', schedule: 'Monthly', schedule_interval: '2' }, // 10.00
  { cost: '30', schedule: 'Monthly', share_count: '3' },       // 10.00 (my share)
];
const CATEGORY_MONTHLY = 102.09;

async function seedMixedCategory(data) {
  const category = await data.category(uniqueName('Dash category'));
  const subs = [];
  for (const [i, fields] of MIXED_SCHEDULES.entries()) {
    subs.push(await data.subscription({ name: uniqueName(`Dash ${fields.schedule} ${i}`), category_id: String(category.id), ...fields }));
  }
  // Neither of these counts towards spend.
  subs.push(await data.subscription({ name: uniqueName('Dash cancelled'), category_id: String(category.id), cost: '50', status: 'Cancelled' }));
  subs.push(await data.subscription({ name: uniqueName('Dash paused'), category_id: String(category.id), cost: '99', status: 'Paused' }));
  return { category, subs };
}

async function readCards(page) {
  const cards = page.locator('main .grid').first().locator(':scope > div');
  await expect(cards).toHaveCount(4);
  const lines = async i => (await cards.nth(i).locator('p.text-3xl').allTextContents()).map(t => t.trim());
  return {
    monthly: await lines(0),
    annual: await lines(1),
    active: await lines(2),
    savings: await lines(3),
  };
}

for (const path of ['/', '/dashboard']) {
  test(`${path} stats cards match every subscription's schedule-adjusted cost`, async ({ page, request }) => {
    const data = fixtures(request);
    try {
      await seedMixedCategory(data);
      await withStableSnapshot(request, async () => {
        const response = await page.goto(path);
        expect(response?.status()).toBe(200);
        return readCards(page);
      }, (subscriptions, cards) => {
        const totals = totalsByCurrency(subscriptions);
        expectSpendCard(cards.monthly, totals, 'monthly', 'active', 'monthly spend');
        expectSpendCard(cards.annual, totals, 'annual', 'active', 'annual spend');
        const cancelled = Object.values(totals).reduce((n, t) => n + t.cancelled, 0);
        if (cancelled > 0) expectSpendCard(cards.savings, totals, 'monthlySaved', 'cancelled', 'monthly savings');
        else expectAmount(cards.savings.join(' '), 0, 'monthly savings');
        expect(cards.active).toEqual([String(subscriptions.filter(s => s.status === 'Active').length)]);
      });
    } finally {
      await data.cleanup();
    }
  });
}

test('dashboard spending by category sums active subscriptions across mixed schedules', async ({ page, request }) => {
  const data = fixtures(request);
  try {
    const { category, subs } = await seedMixedCategory(data);
    // The helper's independent formula agrees with the hand-computed figure.
    const active = subs.filter(s => s.status === 'Active');
    expect(active.reduce((sum, s) => sum + monthlyCost(s), 0)).toBeCloseTo(CATEGORY_MONTHLY, 6);

    await page.goto('/dashboard');
    const row = page.locator('div.space-y-4 > div', { hasText: category.name });
    await expect(row).toHaveCount(1);
    const amount = (await row.textContent() || '').replace(category.name, '');
    expectAmount(amount, CATEGORY_MONTHLY, 'category spend');
  } finally {
    await data.cleanup();
  }
});

test('dashboard lists each subscription with its category and cost', async ({ page, request }) => {
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Dash list'));
    const active = await data.subscription({ name: uniqueName('Dash list active'), category_id: String(category.id), cost: '12.34', schedule: 'Annual', renewal_date: isoDate(daysFromNow(40)) });
    const cancelled = await data.subscription({ name: uniqueName('Dash list cancelled'), category_id: String(category.id), cost: '7.5', status: 'Cancelled' });

    await page.goto('/');
    for (const [sub, cost] of [[active, '12.34'], [cancelled, '7.50']]) {
      const row = page.locator('main .divide-y > div', { has: page.locator('h3', { hasText: sub.name }) });
      await expect(row).toHaveCount(1);
      await expect(row).toContainText(category.name);
      await expect(row).toContainText(cost);
    }
  } finally {
    await data.cleanup();
  }
});

test('deleting a subscription removes it from the dashboard', async ({ page, request }) => {
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Dash removal'));
    const sub = await data.subscription({ name: uniqueName('Dash removal'), category_id: String(category.id), cost: '3' });
    await page.goto('/');
    await expect(page.locator('main h3', { hasText: sub.name })).toHaveCount(1);
    await expect(page.locator('div.space-y-4 > div', { hasText: category.name })).toHaveCount(1);

    expect((await request.delete(`/api/subscriptions/${sub.id}`)).ok()).toBeTruthy();
    await page.reload();
    await expect(page.locator('main h3', { hasText: sub.name })).toHaveCount(0);
    await expect(page.locator('div.space-y-4 > div', { hasText: category.name })).toHaveCount(0);
  } finally {
    await data.cleanup();
  }
});
