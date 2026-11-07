// Helpers for the UX specs (mobile navigation, themes, keyboard, console errors).
const fs = require('fs');
const path = require('path');
const { expect } = require('@playwright/test');

/** Directory for screenshots that document what the browser actually rendered. */
const screenshotDir = path.resolve(__dirname, '..', '..', 'test-results', 'ux-screenshots');

async function saveScreenshot(page, name, options = {}) {
  fs.mkdirSync(screenshotDir, { recursive: true });
  const file = path.join(screenshotDir, `${name}.png`);
  await page.screenshot({ path: file, fullPage: true, scale: 'css', ...options });
  return file;
}

/**
 * Records browser console errors, uncaught page errors and failed requests.
 * Attach it before navigating. `report()` returns everything collected so far.
 *
 * Only `console.error` level messages count as errors: Tailwind's CDN build
 * logs a `console.warn` about production use on every page, which is a
 * design decision of the app, not a runtime failure. Warnings are kept
 * separately so the test can annotate them without failing.
 */
function collectConsole(page) {
  const errors = [];
  const warnings = [];
  const pageErrors = [];
  const failedRequests = [];
  page.on('console', (message) => {
    const entry = `${message.type()}: ${message.text()} (${message.location().url}:${message.location().lineNumber})`;
    if (message.type() === 'error') errors.push(entry);
    else if (message.type() === 'warning') warnings.push(entry);
  });
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('requestfailed', (request) => {
    failedRequests.push(`${request.method()} ${request.url()} -> ${request.failure()?.errorText}`);
  });
  page.on('response', (response) => {
    if (response.status() >= 400 && !response.request().isNavigationRequest()) {
      failedRequests.push(`${response.request().method()} ${response.url()} -> HTTP ${response.status()}`);
    }
  });
  return {
    errors, warnings, pageErrors, failedRequests,
    report() {
      return { errors: [...errors], warnings: [...warnings], pageErrors: [...pageErrors], failedRequests: [...failedRequests] };
    },
  };
}

/**
 * The page must not scroll sideways: layout overflow on narrow screens is a
 * real UX defect. Waits for fonts and a stable layout (two identical
 * consecutive measurements) before judging, and names the elements that
 * reach past the viewport so the failure says what to fix.
 */
async function expectNoHorizontalOverflow(page, label) {
  await page.evaluate(() => document.fonts?.ready).catch(() => {});
  const measure = () => page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  let last = await measure();
  await expect.poll(async () => {
    const next = await measure();
    const stable = next.scrollWidth === last.scrollWidth;
    last = next;
    return stable;
  }, { timeout: 5000, intervals: [100, 200, 300] }).toBe(true);
  const { scrollWidth, clientWidth } = last;
  if (scrollWidth > clientWidth) {
    const offenders = await page.evaluate((limit) => {
      const seen = [];
      for (const el of document.querySelectorAll('body *')) {
        const rect = el.getBoundingClientRect();
        if (rect.width > 0 && rect.right > limit + 1) {
          const describe = (node) => `${node.tagName.toLowerCase()}${node.id ? '#' + node.id : ''}${node.className && typeof node.className === 'string' ? '.' + node.className.trim().split(/\s+/).slice(0, 3).join('.') : ''}`;
          seen.push(`${describe(el)} right=${Math.round(rect.right)}`);
        }
        if (seen.length >= 12) break;
      }
      return seen;
    }, clientWidth);
    expect(scrollWidth, `${label}: document.scrollWidth ${scrollWidth} exceeds viewport ${clientWidth}; elements past the edge:\n  ${offenders.join('\n  ')}`).toBeLessThanOrEqual(clientWidth);
  }
}

/** Computed background colour of `selector` as "rgb(r, g, b)" / "rgba(...)". */
async function backgroundColor(page, selector) {
  return page.locator(selector).first().evaluate((el) => getComputedStyle(el).backgroundColor);
}

/** The element that currently has keyboard focus, described for assertions. */
async function activeElement(page) {
  return page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body) return { tag: 'BODY' };
    return { tag: el.tagName, id: el.id || null, name: el.getAttribute('name'), text: (el.textContent || '').trim().slice(0, 40) };
  });
}

module.exports = { screenshotDir, saveScreenshot, collectConsole, expectNoHorizontalOverflow, backgroundColor, activeElement };
