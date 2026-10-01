const { expect } = require('@playwright/test');

async function ensureCategory(request) {
  const response = await request.get('/api/categories');
  expect(response.ok()).toBeTruthy();
  const categories = await response.json();
  if (categories.length > 0) return categories[0].id;

  const created = await request.post('/api/categories', {
    data: { name: `Browser fixture ${Date.now()}` },
  });
  expect(created.ok()).toBeTruthy();
  return (await created.json()).id;
}

async function seedSubscription(request, fields = {}) {
  const categoryID = await ensureCategory(request);
  const response = await request.post('/api/subscriptions', { form: {
    name: `Browser fixture ${Date.now()}`,
    cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
    category_id: String(categoryID),
    ...fields,
  } });
  expect(response.ok()).toBeTruthy();
  return await response.json();
}

module.exports = { ensureCategory, seedSubscription };
