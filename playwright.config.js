// @ts-check
const { defineConfig, devices } = require('@playwright/test');

const isolatedSpecs = /isolated-.*\.spec\.js$/;
const mobileSpecs = /mobile-.*\.spec\.js$/;

/* Ports for the two shared test servers. Override them to run a second copy of
   the suite next to one already using the defaults, e.g.
     E2E_PORT=8182 E2E_ISOLATED_PORT=8183 npx playwright test
   Each server keeps its SQLite database in a private temporary directory.
   E2E_DATA_DIR / E2E_ISOLATED_DATA_DIR (absolute, or relative to this file)
   choose the directory instead; the runner only reuses a directory that is
   empty or that it created earlier, and never deletes anything else.
   See tests/helpers/e2e-server.js. */
const port = process.env.E2E_PORT || '8082';
const isolatedPort = process.env.E2E_ISOLATED_PORT || '8083';

const chromiumLaunch = {
  /* Optional: reuse a preinstalled Chrome instead of `npx playwright install`. */
  launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined },
};

/**
 * @see https://playwright.dev/docs/test-configuration
 */
module.exports = defineConfig({
  testDir: './tests',
  /* Only *.spec.js files are tests; pocketid.e2e.js is a standalone script. */
  testMatch: /.*\.spec\.js$/,
  /* Run tests in files in parallel */
  fullyParallel: true,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  /* Shared settings and subscription counts require serial execution locally too.
     Separate CI jobs still run browser projects in parallel on isolated hosts. */
  workers: 1,
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: process.env.CI
    ? [['list'], ['github'], ['html', { open: 'never' }], ['json', { outputFile: 'test-results/results.json' }]]
    : 'html',
  /* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
  use: {
    /* Base URL to use in actions like `await page.goto('/')`. */
    baseURL: `http://localhost:${port}`,

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: 'on-first-retry',
    /* Keep evidence for failures so CI artifacts show what the browser saw. */
    screenshot: 'only-on-failure',
    video: process.env.CI ? 'retain-on-failure' : 'off',
  },

  /* Configure projects for major browsers */
  projects: [
    {
      name: 'chromium',
      testIgnore: [isolatedSpecs, mobileSpecs],
      use: { ...devices['Desktop Chrome'], ...chromiumLaunch },
    },

    {
      name: 'firefox',
      testIgnore: [isolatedSpecs, mobileSpecs],
      use: { ...devices['Desktop Firefox'] },
    },

    {
      name: 'webkit',
      testIgnore: [isolatedSpecs, mobileSpecs],
      use: { ...devices['Desktop Safari'] },
    },

    /* Specs that change global server state (login, clear-all, restore, import)
       run one at a time against their own server and database (E2E_ISOLATED_PORT, default 8083). */
    {
      name: 'isolated',
      testMatch: isolatedSpecs,
      workers: 1,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: `http://localhost:${isolatedPort}`,
        ...chromiumLaunch,
      },
    },

    /* Mobile viewports: hamburger navigation, touch targets and form layout.
       mobile-*.spec.js only; the desktop projects skip them. */
    {
      name: 'mobile-chromium',
      testMatch: mobileSpecs,
      use: { ...devices['Pixel 7'], ...chromiumLaunch },
    },
    {
      name: 'mobile-webkit',
      testMatch: mobileSpecs,
      use: { ...devices['iPhone 13'] },
    },
  ],

  /* Start the two shared test servers. The runner refuses to attach to a
     server it did not start (a busy port is an error) and keeps each
     database in its own temporary directory. */
  webServer: [
    {
      command: 'node tests/helpers/e2e-server.js main',
      cwd: __dirname,
      url: `http://localhost:${port}/healthz`,
      reuseExistingServer: false,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
      timeout: 180_000,
    },
    {
      command: 'node tests/helpers/e2e-server.js isolated',
      cwd: __dirname,
      url: `http://localhost:${isolatedPort}/healthz`,
      reuseExistingServer: false,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
      timeout: 180_000,
    },
  ],
});
