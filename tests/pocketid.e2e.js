const { chromium } = require('@playwright/test');

const pocketID = 'https://pocket-id.test:8443';
const subtrackr = 'http://subtrackr.test:8080';

async function completePocketInteraction(page) {
  await page.waitForTimeout(500);
  if (page.url().startsWith(`${pocketID}/interaction`)) {
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.waitForTimeout(1000);
    if (page.url().startsWith(pocketID)) {
      console.log(`Pocket ID remained at ${page.url()}:\n${await page.locator('body').innerText()}`);
    }
  }
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  try {
    const setup = await context.request.post(`${pocketID}/api/signup/setup`, {
      data: {
        username: 'e2e-admin',
        email: 'e2e-admin@example.test',
        firstName: 'E2E',
        lastName: 'Admin',
      },
    });
    if (!setup.ok()) {
      throw new Error(`Pocket ID setup failed: ${setup.status()} ${await setup.text()}`);
    }
    const setupCookies = (await context.storageState()).cookies.filter(cookie => cookie.domain.includes('pocket-id'));
    if (setupCookies.length === 0) {
      throw new Error(`Pocket ID setup returned no browser session cookie; set-cookie=${setup.headers()['set-cookie'] || 'none'}`);
    }

    const page = await context.newPage();
    let callbackURL = '';
    page.on('request', request => {
      if (request.url().startsWith(`${subtrackr}/auth/oidc/callback?`)) {
        callbackURL = request.url();
      }
    });

    await page.goto(`${subtrackr}/settings`);
    if (!page.url().startsWith(`${subtrackr}/login`)) {
      throw new Error(`Expected protected settings to redirect to login, got ${page.url()}`);
    }

    let oidcLink = page.locator('a[href^="/auth/oidc/login"]');
    if (await oidcLink.count() !== 1) {
      throw new Error(`Expected one OIDC login link at ${page.url()}:\n${await page.locator('body').innerText()}`);
    }
    await oidcLink.click();
	await completePocketInteraction(page);
    await page.waitForURL(`${subtrackr}/settings`);
    if (!callbackURL) {
      throw new Error('Browser never visited the SubTrackr OIDC callback');
    }
    await page.getByRole('heading', { name: 'Settings' }).waitFor();

    const replay = await context.request.get(callbackURL, { maxRedirects: 0 });
    if (replay.status() !== 302 || !replay.headers().location?.startsWith('/login?error=')) {
      throw new Error(`Expected callback replay rejection, got ${replay.status()} ${replay.headers().location || ''}`);
    }

    await page.goto(`${subtrackr}/api/auth/logout`);
    await page.goto(`${subtrackr}/settings`);
    if (!page.url().startsWith(`${subtrackr}/login`)) {
      throw new Error(`Expected logout to protect settings, got ${page.url()}`);
    }

    oidcLink = page.locator('a[href^="/auth/oidc/login"]');
    if (await oidcLink.count() !== 1) {
      throw new Error(`Expected one OIDC login link at ${page.url()}:\n${await page.locator('body').innerText()}`);
    }
    await oidcLink.click();
	await completePocketInteraction(page);
    await page.waitForURL(`${subtrackr}/settings`);
    await page.getByRole('heading', { name: 'Settings' }).waitFor();

    console.log('Pocket ID OIDC E2E passed: login, callback, protected session, replay rejection, logout, and re-login');
  } finally {
    await context.close();
    await browser.close();
  }
})().catch(error => {
  console.error(error);
  process.exit(1);
});
