const { expect } = require('@playwright/test');

async function seedSubscription(request, fields = {}) {
  const response = await request.post('/api/subscriptions', { form: {
    name: `Browser fixture ${Date.now()}`,
    cost: '1', schedule: 'Monthly', status: 'Active', original_currency: 'USD',
    ...fields,
  } });
  expect(response.ok()).toBeTruthy();
  return await response.json();
}

module.exports = { seedSubscription };
