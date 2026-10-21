// @ts-check
const path = require('path');
const { defineConfig, devices } = require('@playwright/test');

const isolatedSpecs = /isolated-.*\.spec\.js$/;

/**
 * @see https://playwright.dev/docs/test-configuration
 */
module.exports = defineConfig({
  testDir: './tests',
  /* Run tests in files in parallel */
  fullyParallel: true,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  /* Opt out of parallel tests on CI. */
  workers: process.env.CI ? 1 : undefined,
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: 'html',
  /* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
  use: {
    /* Base URL to use in actions like `await page.goto('/')`. */
    baseURL: 'http://localhost:8082',

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: 'on-first-retry',
  },

  /* Configure projects for major browsers */
  projects: [
    {
      name: 'chromium',
      testIgnore: isolatedSpecs,
      use: {
        ...devices['Desktop Chrome'],
        /* Optional: reuse a preinstalled Chrome instead of `npx playwright install`. */
        launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined },
      },
    },

    {
      name: 'firefox',
      testIgnore: isolatedSpecs,
      use: { ...devices['Desktop Firefox'] },
    },

    {
      name: 'webkit',
      testIgnore: isolatedSpecs,
      use: { ...devices['Desktop Safari'] },
    },

    /* Specs that change global server state (login, clear-all, restore, import)
       run one at a time against their own server and database on :8083. */
    {
      name: 'isolated',
      testMatch: isolatedSpecs,
      workers: 1,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: 'http://localhost:8083',
        launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined },
      },
    },

    /* Test against mobile viewports. */
    // {
    //   name: 'Mobile Chrome',
    //   use: { ...devices['Pixel 5'] },
    // },
    // {
    //   name: 'Mobile Safari',
    //   use: { ...devices['iPhone 12'] },
    // },

    /* Test against branded browsers. */
    // {
    //   name: 'Microsoft Edge',
    //   use: { ...devices['Desktop Edge'], channel: 'msedge' },
    // },
    // {
    //   name: 'Google Chrome',
    //   use: { ...devices['Desktop Chrome'], channel: 'chrome' },
    // },
  ],

  /* Run your local dev server before starting the tests */
  webServer: [
    {
      command: 'rm -rf test-data && mkdir -p test-data && PORT=8082 DATABASE_PATH=./test-data/e2e.db go run ./cmd/server',
      url: 'http://localhost:8082/healthz',
      reuseExistingServer: !process.env.CI,
    },
    {
      /* Separate directory: the :8082 command wipes test-data/ while this one starts. */
      command: 'rm -rf test-data-isolated && mkdir -p test-data-isolated && PORT=8083 DATABASE_PATH=./test-data-isolated/e2e-isolated.db go run ./cmd/server',
      url: 'http://localhost:8083/healthz',
      reuseExistingServer: !process.env.CI,
      /* Trust the fake SMTP server's self-signed certificate so STARTTLS succeeds. */
      env: { SSL_CERT_FILE: path.join(__dirname, 'tests/fixtures/isolated-smtp-cert.pem') },
    },
  ],
});