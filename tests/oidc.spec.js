// @ts-check
const { test, expect } = require('@playwright/test');

test('settings page saves generic OIDC configuration without rendering the secret', async ({ page }) => {
  await page.goto('/settings');

  await expect(page.getByRole('heading', { name: 'OpenID Connect' })).toBeVisible();
  await expect(page.getByLabel('Enable OIDC login')).not.toBeChecked();
  await expect(page.getByLabel('Provider name')).toBeVisible();
  await expect(page.getByLabel('Issuer URL')).toBeVisible();
  await expect(page.getByLabel('Client ID')).toBeVisible();
  await expect(page.getByLabel('Client secret')).toBeVisible();
  await expect(page.getByLabel('Scopes')).toBeVisible();
  await expect(page.getByText('/auth/oidc/callback')).toBeVisible();

  await page.getByLabel('Provider name').fill('Pocket ID');
  await page.getByLabel('Issuer URL').fill('https://id.example.test/');
  await page.getByLabel('Client ID').fill('subtrackr-browser-test');
  await page.getByLabel('Client secret').fill('browser-test-secret');
  await page.getByLabel('Scopes').fill('openid   profile  email');
  await page.getByRole('button', { name: 'Save OIDC settings' }).click();
  await expect(page.locator('#oidc-message')).toContainText('saved');

  await page.reload();
  await expect(page.getByLabel('Provider name')).toHaveValue('Pocket ID');
  await expect(page.getByLabel('Issuer URL')).toHaveValue('https://id.example.test');
  await expect(page.getByLabel('Client ID')).toHaveValue('subtrackr-browser-test');
  await expect(page.getByLabel('Scopes')).toHaveValue('openid profile email');
  await expect(page.getByLabel('Client secret')).toHaveValue('');
  await expect(page.getByLabel('Client secret')).toHaveAttribute('placeholder', /leave blank/i);
  await expect(page.locator('body')).not.toContainText('browser-test-secret');

  await page.getByLabel('Provider name').fill('Authentik');
  await page.getByLabel('Client ID').fill('updated-client');
  await page.getByLabel('Client secret').fill('replacement-browser-secret');
  await page.getByRole('button', { name: 'Save OIDC settings' }).click();
  await expect(page.locator('#oidc-message')).toContainText('saved');

  await page.reload();
  await expect(page.getByLabel('Provider name')).toHaveValue('Authentik');
  await expect(page.getByLabel('Client ID')).toHaveValue('updated-client');
  await expect(page.getByLabel('Client secret')).toHaveValue('');
  await expect(page.locator('body')).not.toContainText('replacement-browser-secret');
  await expect(page.getByLabel('Enable OIDC login')).not.toBeChecked();
});
