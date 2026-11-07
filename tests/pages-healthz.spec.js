// @ts-check
const { test, expect } = require('@playwright/test');

test('/healthz reports healthy JSON without authentication', async ({ playwright, baseURL }) => {
  // A fresh context carries no cookies, like a container health probe.
  const probe = await playwright.request.newContext({ baseURL });
  try {
    const response = await probe.get('/healthz');
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toMatch(/^application\/json/);
    expect(await response.json()).toEqual({ status: 'healthy' });
  } finally {
    await probe.dispose();
  }
});
