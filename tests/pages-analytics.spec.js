// @ts-check
// /analytics: overview cards, category breakdown, status counts and cost analysis.
const { test, expect } = require('@playwright/test');
const {
  uniqueName, daysFromNow, isoDate, fixtures, withStableSnapshot, totalsByCurrency,
  upcomingRenewalCount, expectSpendCard, expectAmount,
} = require('./helpers/pages');

test('analytics renders totals, status counts and upcoming renewals that match the data', async ({ page, request }) => {
  const data = fixtures(request);
  try {
    const category = await data.category(uniqueName('Analytics totals'));
    const cat = String(category.id);
    await data.subscription({ name: uniqueName('Analytics soon'), category_id: cat, cost: '8', renewal_date: isoDate(daysFromNow(3)) });
    await data.subscription({ name: uniqueName('Analytics later'), category_id: cat, cost: '9', schedule: 'Weekly', renewal_date: isoDate(daysFromNow(20)) });
    await data.subscription({ name: uniqueName('Analytics paused soon'), category_id: cat, cost: '4', status: 'Paused', renewal_date: isoDate(daysFromNow(2)) });
    await data.subscription({ name: uniqueName('Analytics cancelled'), category_id: cat, cost: '60', schedule: 'Annual', status: 'Cancelled' });

    await withStableSnapshot(request, async () => {
      const response = await page.goto('/analytics');
      expect(response?.status()).toBe(200);
      const overview = page.locator('main .grid').first().locator(':scope > div');
      await expect(overview).toHaveCount(3);
      const lines = async i => (await overview.nth(i).locator('p.text-2xl').allTextContents()).map(t => t.trim());
      return {
        monthly: await lines(0),
        annual: await lines(1),
        saved: await lines(2),
        active: (await page.locator('span.text-lg.text-success').textContent())?.trim(),
        cancelled: (await page.locator('span.text-lg.text-danger').textContent())?.trim(),
        upcoming: (await page.locator('span.text-lg.text-warning').textContent())?.trim(),
        now: Date.now(),
      };
    }, (subscriptions, shown) => {
      const totals = totalsByCurrency(subscriptions);
      expectSpendCard(shown.monthly, totals, 'monthly', 'active', 'monthly spend');
      expectSpendCard(shown.annual, totals, 'annual', 'active', 'annual spend');
      expectSpendCard(shown.saved, totals, 'saved', 'cancelled', 'annual savings');
      expect(shown.active).toBe(String(subscriptions.filter(s => s.status === 'Active').length));
      expect(shown.cancelled).toBe(String(subscriptions.filter(s => s.status === 'Cancelled').length));
      expect(shown.upcoming).toBe(String(upcomingRenewalCount(subscriptions, shown.now)));
    });
  } finally {
    await data.cleanup();
  }
});

test('analytics category breakdown shows monthly spend per category for active subscriptions only', async ({ page, request }) => {
  const data = fixtures(request);
  try {
    const streaming = await data.category(uniqueName('Analytics streaming'));
    const tools = await data.category(uniqueName('Analytics tools'));
    const onlyInactive = await data.category(uniqueName('Analytics inactive'));
    // streaming: 15 + 240/12 = 35.00; tools: 2 * 4.33 + 36/3 = 20.66
    await data.subscription({ name: uniqueName('Stream monthly'), category_id: String(streaming.id), cost: '15' });
    await data.subscription({ name: uniqueName('Stream annual'), category_id: String(streaming.id), cost: '240', schedule: 'Annual' });
    await data.subscription({ name: uniqueName('Stream cancelled'), category_id: String(streaming.id), cost: '100', status: 'Cancelled' });
    await data.subscription({ name: uniqueName('Tools weekly'), category_id: String(tools.id), cost: '2', schedule: 'Weekly' });
    await data.subscription({ name: uniqueName('Tools quarterly'), category_id: String(tools.id), cost: '36', schedule: 'Quarterly' });
    await data.subscription({ name: uniqueName('Tools trial'), category_id: String(tools.id), cost: '50', status: 'Trial' });
    await data.subscription({ name: uniqueName('Inactive paused'), category_id: String(onlyInactive.id), cost: '11', status: 'Paused' });

    await page.goto('/analytics');
    const rowFor = name => page.locator('div.space-y-4 > div', { hasText: name });
    for (const [category, expected] of [[streaming, 35], [tools, 20.66]]) {
      const row = rowFor(category.name);
      await expect(row).toHaveCount(1);
      expectAmount((await row.textContent() || '').replace(category.name, ''), expected, category.name);
    }
    await expect(rowFor(onlyInactive.name)).toHaveCount(0);

    // The JSON stats agree with the page.
    const stats = await (await request.get('/api/stats')).json();
    if (stats.conversion_complete) {
      expect(stats.category_spending[streaming.name]).toBeCloseTo(35, 6);
      expect(stats.category_spending[tools.name]).toBeCloseTo(20.66, 6);
      expect(stats.category_spending[onlyInactive.name]).toBeUndefined();
    }
    expect(stats.category_spending_by_currency[streaming.name]).toEqual({ USD: expect.closeTo(35, 6) });
    expect(stats.category_spending_by_currency[tools.name]).toEqual({ USD: expect.closeTo(20.66, 6) });
  } finally {
    await data.cleanup();
  }
});
