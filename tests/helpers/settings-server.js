// Playwright fixtures that run settings specs against a private SubTrackr server.
//
// Settings are global server state. Several of them (currency, date format,
// language, theme) change what every page renders, and the notification
// channel configs (SMTP, Pushover, Webhook, Telegram) cannot be cleared through
// the API once saved. Running these specs against the shared webServer would
// make other specs flaky while they run in parallel, so each Playwright worker
// that uses this `test` builds the current cmd/server and starts it on a free
// port with its own SQLite database. `baseURL` (and so `page` and `request`)
// points at that server.
const base = require('@playwright/test');
const { execFile, spawn } = require('child_process');
const { promisify } = require('util');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..', '..');

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

async function waitForHealthy(url, proc, logFile, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (proc.exitCode !== null) break;
    try {
      const res = await fetch(`${url}/healthz`);
      if (res.ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`isolated SubTrackr server did not become healthy:\n${fs.readFileSync(logFile, 'utf8')}`);
}

const test = base.test.extend({
  isolatedServer: [async ({}, use) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'subtrackr-settings-e2e-'));
    const bin = path.join(dir, 'subtrackr');
    await promisify(execFile)('go', ['build', '-o', bin, './cmd/server'], { cwd: repoRoot });
    const port = await freePort();
    const logFile = path.join(dir, 'server.log');
    const log = fs.openSync(logFile, 'a');
    const proc = spawn(bin, [], {
      cwd: repoRoot,
      env: { ...process.env, PORT: String(port), DATABASE_PATH: path.join(dir, 'e2e.db') },
      stdio: ['ignore', log, log],
    });
    const baseURL = `http://localhost:${port}`;
    try {
      await waitForHealthy(baseURL, proc, logFile, 60_000);
      await use({ baseURL });
    } finally {
      proc.kill('SIGTERM');
      await new Promise((r) => (proc.exitCode !== null ? r() : proc.once('exit', r)));
      fs.closeSync(log);
      fs.rmSync(dir, { recursive: true, force: true });
    }
  }, { scope: 'worker', timeout: 240_000 }],

  baseURL: async ({ isolatedServer }, use) => {
    await use(isolatedServer.baseURL);
  },
});

module.exports = { test, expect: base.expect };
