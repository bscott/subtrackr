// Playwright fixtures that run specs against a private SubTrackr server.
//
// Settings are global server state. Several of them (currency, date format,
// language, theme) change what every page renders, and the notification
// channel configs (SMTP, Pushover, Webhook, Telegram) cannot be cleared through
// the API once saved. Running these specs against the shared webServer would
// make other specs flaky while they run in parallel, so each Playwright worker
// that uses this `test` builds the current cmd/server and starts it on a free
// port with its own SQLite database in a private temporary directory.
// `baseURL` (and so `page` and `request`) points at that server.
//
// Specs that need extra server environment (for example an SSL_CERT_FILE) can
// set it with `test.use({ isolatedServerEnv: { ... } })`.
const base = require('@playwright/test');
const fs = require('fs');
const path = require('path');
const {
  buildServer, freePort, waitForHealthy, serverEnv, startServer, stopServer, prepareDataDir,
} = require('./server-process');

const test = base.test.extend({
  isolatedServerEnv: [{}, { option: true, scope: 'worker' }],

  isolatedServer: [async ({ isolatedServerEnv }, use) => {
    const { dir } = prepareDataDir(undefined, 'worker');
    const logFile = path.join(dir, 'server.log');
    let proc;
    try {
      const bin = await buildServer(dir);
      let port;
      // freePort() picks a port that is free at that instant; with many workers
      // (and their fake providers) starting at once, another process can grab it
      // before Gin binds. Retry on that specific failure rather than failing the suite.
      for (let attempt = 1; ; attempt++) {
        port = await freePort();
        fs.writeFileSync(logFile, '');
        proc = startServer({ bin, logFile, env: serverEnv({ port, dbPath: path.join(dir, 'e2e.db'), extra: isolatedServerEnv }) });
        try {
          await waitForHealthy(`http://localhost:${port}`, proc, logFile, 60_000);
          break;
        } catch (err) {
          const bindRace = proc.exitCode !== null && /address already in use/.test(fs.readFileSync(logFile, 'utf8'));
          if (!bindRace || attempt >= 5) throw err;
        }
      }
      const baseURL = `http://localhost:${port}`;
      await use({ baseURL, port, dir, logFile, log: () => fs.readFileSync(logFile, 'utf8') });
    } finally {
      if (proc) await stopServer(proc);
      fs.rmSync(dir, { recursive: true, force: true });
    }
  }, { scope: 'worker', timeout: 240_000 }],

  baseURL: async ({ isolatedServer }, use) => {
    await use(isolatedServer.baseURL);
  },
});

module.exports = { test, expect: base.expect };
