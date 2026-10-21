// @ts-check
// Client-side search on /subscriptions (web/static/js/subscription-search.js): filtering
// by name and the "no matches" state, both of which must survive the list being swapped
// by sorting.
const { test, expect } = require('@playwright/test');
const {
  uniqueName, createCategory, seed, row, sortButton, expectSortSpec, expectOrder, gotoSubscriptions,
} = require('./helpers/subscription-ui');

async function seedSearchFixture(request) {
  const prefix = uniqueName('Find');
  const category = await createCategory(request);
  const names = [`${prefix} Music Alpha`, `${prefix} Video Beta`, `${prefix} Music Gamma`];
  for (const [index, name] of names.entries()) {
    await seed(request, { name, cost: String(index + 1), category_id: String(category.id) });
  }
  return { prefix, alpha: names[0], beta: names[1], gamma: names[2] };
}

const search = page => page.locator('#subscription-search');
const emptyState = page => page.locator('#subscription-search-empty');

async function expectShown(page, shown, hidden) {
  for (const name of shown) await expect(row(page, name)).toBeVisible();
  for (const name of hidden) await expect(row(page, name)).toBeHidden();
}

test.describe('subscription search', () => {
  test('filters by case-insensitive name substring and restores rows when cleared', async ({ page, request }) => {
    const { prefix, alpha, beta, gamma } = await seedSearchFixture(request);
    await gotoSubscriptions(page);

    await search(page).fill(`${prefix} music`.toUpperCase());
    await expectShown(page, [alpha, gamma], [beta]);
    await expect(emptyState(page)).toBeHidden();

    await search(page).fill(`${prefix} video`);
    await expectShown(page, [beta], [alpha, gamma]);

    await search(page).fill(`${prefix} nothing here`);
    await expectShown(page, [], [alpha, beta, gamma]);
    await expect(emptyState(page)).toBeVisible();

    await search(page).fill('');
    await expectShown(page, [alpha, beta, gamma], []);
    await expect(emptyState(page)).toBeHidden();
  });

  test('the filter is re-applied after every sort swap', async ({ page, request }) => {
    const { prefix, alpha, beta, gamma } = await seedSearchFixture(request);
    await gotoSubscriptions(page);
    await search(page).fill(`${prefix} music`);
    await expectShown(page, [alpha, gamma], [beta]);

    for (const [field, spec, order] of [
      ['cost', 'cost:asc', [alpha, gamma]],
      ['cost', 'cost:desc', [gamma, alpha]],
      ['name', 'cost:desc,name:asc', [gamma, alpha]],
      ['cost', 'name:asc', [alpha, gamma]],
    ]) {
      await sortButton(page, field).click();
      await expectSortSpec(page, spec);
      await expect(search(page)).toHaveValue(`${prefix} music`);
      await expectShown(page, [alpha, gamma], [beta]);
      await expectOrder(page, order);
    }

    // Clearing the search after swaps shows the hidden row again, in sorted position.
    await search(page).fill('');
    await expectOrder(page, [alpha, gamma, beta]);
    await expectShown(page, [alpha, beta, gamma], []);
  });

  test('the "no matches" message survives a sort swap', async ({ page, request }) => {
    // APP BUG with HTMX 1.9.10: subscription-search.js un-hides #subscription-search-empty
    // in htmx:afterSwap, but HTMX's settle step then copies the server-rendered
    // class="hidden" back onto that id-matched element (observed: visible at afterSwap,
    // hidden at afterSettle). Rows stay filtered, so the user sees an empty table with no
    // message. HTMX 4 changes swap/settle, so this may start passing after the upgrade;
    // if Playwright then reports "expected to fail but passed", remove test.fail().
    test.fail();
    const { prefix } = await seedSearchFixture(request);
    await gotoSubscriptions(page);
    await search(page).fill(`${prefix} nothing here`);
    await expect(emptyState(page)).toBeVisible();

    await sortButton(page, 'name').click();
    await expectSortSpec(page, 'name:asc');
    await expect(emptyState(page)).toBeVisible();
    await expect(page.locator('#subscription-list tbody tr[data-search-name]:visible')).toHaveCount(0);
  });

  test('a search typed before the saved sort is re-applied on load still filters', async ({ page, request }) => {
    const { prefix, alpha, beta, gamma } = await seedSearchFixture(request);
    await gotoSubscriptions(page);
    await sortButton(page, 'cost').click();
    await expectSortSpec(page, 'cost:asc');

    // Hold the sort request that the reload issues so the search is typed before it swaps.
    let release = () => {};
    const held = new Promise(resolve => { release = resolve; });
    await page.route(url => url.pathname === '/api/subscriptions' && url.searchParams.has('sorts'), async route => {
      const response = await route.fetch();
      await held;
      await route.fulfill({ response });
    }, { times: 1 });
    const swapped = page.waitForResponse(r => new URL(r.url()).searchParams.get('sorts') === 'cost:asc');

    await page.reload();
    await search(page).fill(`${prefix} video`);
    release();
    await swapped;
    await expectSortSpec(page, 'cost:asc');
    await expectShown(page, [beta], [alpha, gamma]);
  });
});
