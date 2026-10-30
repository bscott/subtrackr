// @ts-check
// Sorting on /subscriptions (web/static/js/sorting.js): per-column cycle, multi-column
// priority, localStorage persistence, ?sort / ?sorts URL params, and stale responses.
// Other specs add rows concurrently, so order assertions only compare this test's rows.
const { test, expect } = require('@playwright/test');
const {
  uniqueName, createCategory, seed, sortButton, list, expectOrder, expectSortSpec, savedSortSpec,
  gotoSubscriptions,
} = require('./helpers/subscription-ui');

/**
 * Three subscriptions whose ascending order differs for every column:
 * name A,B,C · category C,A,B · cost B,A,C · schedule B,C,A · status A,C,B · renewal C,B,A
 */
async function seedSortFixture(request) {
  const prefix = uniqueName('Sort');
  const categories = {};
  for (const letter of ['a', 'b', 'c']) {
    categories[letter] = await createCategory(request, `${prefix} cat-${letter}`);
  }
  const rows = [
    { name: `${prefix} A`, category: 'b', cost: '20', schedule: 'Weekly', status: 'Active', renewal_date: '2031-03-01' },
    { name: `${prefix} B`, category: 'c', cost: '10', schedule: 'Annual', status: 'Trial', renewal_date: '2031-02-01' },
    { name: `${prefix} C`, category: 'a', cost: '30', schedule: 'Monthly', status: 'Paused', renewal_date: '2031-01-01' },
  ];
  for (const { category, ...fields } of rows) {
    await seed(request, { ...fields, category_id: String(categories[category].id) });
  }
  const [A, B, C] = rows.map(r => r.name);
  return { A, B, C, names: [A, B, C] };
}

/** aria-pressed and the priority badge for one header button. */
async function expectIndicator(page, field, priority) {
  const button = sortButton(page, field);
  await expect(button).toHaveAttribute('aria-pressed', priority ? 'true' : 'false');
  const badge = button.locator('[data-sort-priority]');
  if (priority) {
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText(String(priority));
  } else {
    await expect(badge).toBeHidden();
  }
}

async function clickSort(page, field, expectedSpec) {
  await sortButton(page, field).click();
  await expectSortSpec(page, expectedSpec);
}

const ascendingOrders = {
  name: ['A', 'B', 'C'],
  category: ['C', 'A', 'B'],
  cost: ['B', 'A', 'C'],
  schedule: ['B', 'C', 'A'],
  status: ['A', 'C', 'B'],
  renewal_date: ['C', 'B', 'A'],
};

test.describe('subscription list sorting', () => {
  for (const [field, letters] of Object.entries(ascendingOrders)) {
    test(`${field} column cycles ascending → descending → unsorted`, async ({ page, request }) => {
      const fixture = await seedSortFixture(request);
      const ascending = letters.map(letter => fixture[letter]);
      await gotoSubscriptions(page);
      await expectSortSpec(page, '');
      await expectIndicator(page, field, null);

      await clickSort(page, field, `${field}:asc`);
      await expectOrder(page, ascending);
      await expectIndicator(page, field, 1);
      expect(await savedSortSpec(page)).toBe(`${field}:asc`);

      await clickSort(page, field, `${field}:desc`);
      await expectOrder(page, [...ascending].reverse());
      await expectIndicator(page, field, 1);
      expect(await savedSortSpec(page)).toBe(`${field}:desc`);

      await clickSort(page, field, '');
      await expectIndicator(page, field, null);
      // Default order is newest first; the fixture rows were created A, B, C.
      await expectOrder(page, [fixture.C, fixture.B, fixture.A]);
      expect(await savedSortSpec(page)).toBe('');
    });
  }

  test('multiple columns combine in click order and keep their priority', async ({ page, request }) => {
    const prefix = uniqueName('Multi');
    const category = await createCategory(request);
    const rows = [
      [`${prefix} 1`, 'Paused'], [`${prefix} 2`, 'Active'], [`${prefix} 3`, 'Paused'], [`${prefix} 4`, 'Active'],
    ];
    for (const [name, status] of rows) {
      await seed(request, { name, status, category_id: String(category.id) });
    }
    const [p1, a2, p3, a4] = rows.map(([name]) => name);

    await gotoSubscriptions(page);

    await clickSort(page, 'status', 'status:asc');
    await clickSort(page, 'name', 'status:asc,name:asc');
    await expectOrder(page, [a2, a4, p1, p3]);
    await expectIndicator(page, 'status', 1);
    await expectIndicator(page, 'name', 2);

    // Toggling the secondary column keeps it secondary.
    await clickSort(page, 'name', 'status:asc,name:desc');
    await expectOrder(page, [a4, a2, p3, p1]);

    await clickSort(page, 'status', 'status:desc,name:desc');
    await expectOrder(page, [p3, p1, a4, a2]);
    await expectIndicator(page, 'status', 1);
    await expectIndicator(page, 'name', 2);
    expect(await savedSortSpec(page)).toBe('status:desc,name:desc');

    // Removing the primary column promotes the next one.
    await clickSort(page, 'status', 'name:desc');
    await expectOrder(page, [a4, p3, a2, p1]);
    await expectIndicator(page, 'status', null);
    await expectIndicator(page, 'name', 1);
  });

  test('saved sort is re-applied after a reload', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);
    await clickSort(page, 'cost', 'cost:asc');
    await clickSort(page, 'cost', 'cost:desc');
    await clickSort(page, 'name', 'cost:desc,name:asc');
    expect(await savedSortSpec(page)).toBe('cost:desc,name:asc');

    await page.reload();
    await expectSortSpec(page, 'cost:desc,name:asc');
    await expectOrder(page, [fixture.C, fixture.A, fixture.B]);
    await expectIndicator(page, 'cost', 1);
    await expectIndicator(page, 'name', 2);

    // A fresh visit (not just a reload) uses the preference too.
    await page.goto('/dashboard');
    await gotoSubscriptions(page);
    await expectSortSpec(page, 'cost:desc,name:asc');
    await expectOrder(page, [fixture.C, fixture.A, fixture.B]);
  });

  test('a preference saved by the older single-column UI is migrated', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);
    await page.evaluate(() => localStorage.setItem('subtrackr-sort',
      JSON.stringify({ sortBy: 'cost', order: 'asc', statusOrder: 'desc' })));

    await page.reload();
    await expectSortSpec(page, 'status:desc,cost:asc');
    await expectOrder(page, [fixture.B, fixture.C, fixture.A]);
  });

  test('?sorts= URL parameter sorts the page and wins over the saved preference', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);
    await clickSort(page, 'name', 'name:asc');

    await gotoSubscriptions(page, '?sorts=status:desc,cost:asc');
    await expectSortSpec(page, 'status:desc,cost:asc');
    await expectOrder(page, [fixture.B, fixture.C, fixture.A]);
    await expectIndicator(page, 'status', 1);
    await expectIndicator(page, 'cost', 2);
    await expectIndicator(page, 'name', null);
    expect(await savedSortSpec(page)).toBe('name:asc');

    // Header clicks build on the URL's sort.
    await clickSort(page, 'name', 'status:desc,cost:asc,name:asc');
    await expectIndicator(page, 'name', 3);
  });

  test('legacy ?sort=&order= URL parameters still work', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page, '?sort=renewal_date&order=asc');
    await expectSortSpec(page, 'renewal_date:asc');
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);

    // Without an order the legacy parameter sorts descending.
    await gotoSubscriptions(page, '?sort=renewal_date');
    await expectSortSpec(page, 'renewal_date:desc');
    await expectOrder(page, [fixture.A, fixture.B, fixture.C]);
  });

  test('invalid sort parameters fall back to the default order', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page, '?sorts=bogus:asc,name:sideways,cost:asc,cost:desc');
    await expectSortSpec(page, 'cost:asc');
    await expectOrder(page, [fixture.B, fixture.A, fixture.C]);

    await gotoSubscriptions(page, '?sort=password&order=asc');
    await expectSortSpec(page, '');
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);
  });

  test('a slow earlier sort response does not overwrite a newer one', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);

    let releaseFirst = () => {};
    const firstHeld = new Promise(resolve => { releaseFirst = resolve; });
    const sortRequests = [];
    await page.route(url => url.pathname === '/api/subscriptions' && url.searchParams.has('sorts'), async route => {
      sortRequests.push(route.request());
      if (sortRequests.length === 1) {
        const response = await route.fetch();
        await firstHeld;
        await route.fulfill({ response });
      } else {
        await route.continue();
      }
    });

    // First click (name:asc) hangs; the second click must build on it (name:desc).
    await sortButton(page, 'name').click();
    await expect.poll(() => sortRequests.length).toBe(1);
    const staleFinished = page.waitForEvent('requestfinished', r => r === sortRequests[0]);
    const newerFinished = page.waitForEvent('requestfinished',
      r => new URL(r.url()).searchParams.get('sorts') === 'name:desc');
    await sortButton(page, 'name').click();

    // HTMX 1.9 queues the newer request until the first finishes; a client that sends
    // both at once gets the newer response first. Release the stale one either way.
    await Promise.race([newerFinished, page.waitForTimeout(1000)]);
    releaseFirst();
    await Promise.all([staleFinished, newerFinished]);
    // Give a stale response time to (wrongly) swap in before asserting it did not.
    await page.waitForTimeout(500);
    await expectSortSpec(page, 'name:desc');
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);
    await expectIndicator(page, 'name', 1);
    expect(await savedSortSpec(page)).toBe('name:desc');

    // The next click continues from what is shown.
    await clickSort(page, 'name', '');
  });

  test('a failed sort request leaves the list usable', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);

    await page.route(url => url.pathname === '/api/subscriptions' && url.searchParams.has('sorts'),
      route => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }),
      { times: 1 });
    const failed = page.waitForResponse(r => new URL(r.url()).searchParams.has('sorts') && r.status() === 500);
    await sortButton(page, 'name').click();
    await failed;
    await page.waitForTimeout(300);

    // The list is not replaced by the error body and still shows the previous sort.
    await expect(list(page).locator('tbody')).toBeVisible();
    await expect(page.getByText('boom')).toHaveCount(0);
    await expectSortSpec(page, '');
    await expectIndicator(page, 'name', null);
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);

    // Retrying starts from the shown (unsorted) state, so it sorts ascending.
    await clickSort(page, 'name', 'name:asc');
    await expectOrder(page, [fixture.A, fixture.B, fixture.C]);
  });

  test('a sort request that fails on the network does not leave a pending sort behind', async ({ page, request }) => {
    // HTMX 4 fires htmx:after:request only for responses; a dropped connection or
    // timeout is reported as htmx:error. If sorting.js missed it, the click below
    // would build on the lost name:asc request and ask for name:desc instead.
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);

    await page.route(url => url.pathname === '/api/subscriptions' && url.searchParams.has('sorts'),
      route => route.abort('connectionrefused'), { times: 1 });
    const failed = page.waitForEvent('requestfailed', r => new URL(r.url()).searchParams.has('sorts'));
    await sortButton(page, 'name').click();
    await failed;
    await page.waitForTimeout(300);

    await expect(list(page).locator('tbody')).toBeVisible();
    await expectSortSpec(page, '');
    await expectIndicator(page, 'name', null);
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);

    await clickSort(page, 'name', 'name:asc');
    await expectOrder(page, [fixture.A, fixture.B, fixture.C]);
    await expectIndicator(page, 'name', 1);
  });

  test('a third quick click wins over a queued second one', async ({ page, request }) => {
    const fixture = await seedSortFixture(request);
    await gotoSubscriptions(page);

    let releaseFirst = () => {};
    const firstHeld = new Promise(resolve => { releaseFirst = resolve; });
    const sent = [];
    await page.route(url => url.pathname === '/api/subscriptions' && url.searchParams.has('sorts'), async route => {
      sent.push(new URL(route.request().url()).searchParams.get('sorts'));
      if (sent.length === 1) {
        const response = await route.fetch();
        await firstHeld;
        await route.fulfill({ response });
      } else {
        await route.continue();
      }
    });

    // name:asc hangs, then name:desc and unsorted are clicked while it is in flight.
    await sortButton(page, 'name').click();
    await expect.poll(() => sent.length).toBe(1);
    await sortButton(page, 'name').click();
    await sortButton(page, 'name').click();
    releaseFirst();

    await expectSortSpec(page, '');
    await expectIndicator(page, 'name', null);
    await expectOrder(page, [fixture.C, fixture.B, fixture.A]);
    expect(await savedSortSpec(page)).toBe('');
    // The queued name:desc request was superseded rather than sent after the newest one.
    await expect.poll(() => sent[sent.length - 1]).toBe('');
    expect(sent).toEqual(['name:asc', '']);
  });
});
