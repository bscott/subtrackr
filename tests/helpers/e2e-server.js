#!/usr/bin/env node
// Starts one of the two Playwright `webServer` instances (see playwright.config.js).
//
//   node tests/helpers/e2e-server.js main       # E2E_PORT (8082), E2E_DATA_DIR
//   node tests/helpers/e2e-server.js isolated   # E2E_ISOLATED_PORT (8083), E2E_ISOLATED_DATA_DIR
//
// The server binary is built with `go build` and run directly, so no shell
// command is assembled from environment variables. The database lives in a
// private temporary directory unless a data directory is requested, and a
// requested directory is only reused when it is empty or was created by this
// runner before (prepareDataDir refuses anything else). A port that is already
// in use is an error: the suite never attaches to a server it did not start.
const fs = require('fs');
const path = require('path');
const {
  buildServer, isPortFree, waitForHealthy, serverEnv, startServer, stopServer, prepareDataDir,
} = require('./server-process');

const ROLES = {
  main: { portVar: 'E2E_PORT', defaultPort: '8082', dirVar: 'E2E_DATA_DIR', db: 'e2e.db' },
  isolated: {
    portVar: 'E2E_ISOLATED_PORT', defaultPort: '8083', dirVar: 'E2E_ISOLATED_DATA_DIR', db: 'e2e-isolated.db',
    // Trust the fake SMTP sink's self-signed certificate so STARTTLS succeeds.
    extraEnv: { SSL_CERT_FILE: path.resolve(__dirname, '..', 'fixtures', 'isolated-smtp-cert.pem') },
  },
};

async function main() {
  const roleName = process.argv[2];
  const role = ROLES[roleName];
  if (!role) {
    console.error(`usage: e2e-server.js <${Object.keys(ROLES).join('|')}>`);
    process.exit(2);
  }
  const port = Number(process.env[role.portVar] || role.defaultPort);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    console.error(`${role.portVar}=${process.env[role.portVar]} is not a valid port`);
    process.exit(2);
  }
  if (!(await isPortFree(port))) {
    console.error(`port ${port} is already in use; stop that server or set ${role.portVar} to a free port`);
    process.exit(1);
  }

  let prepared;
  try {
    prepared = prepareDataDir(process.env[role.dirVar], roleName);
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }
  const { dir, ownsDir } = prepared;
  // Server output goes under test-results/ so it survives the temporary data
  // directory and is uploaded with the CI artifacts.
  const logDir = path.resolve(__dirname, '..', '..', 'test-results');
  fs.mkdirSync(logDir, { recursive: true });
  const logFile = path.join(logDir, `server-${roleName}.log`);
  fs.writeFileSync(logFile, '');
  let proc;
  let shuttingDown = false;
  const shutdown = async (code) => {
    if (shuttingDown) return;
    shuttingDown = true;
    if (proc) await stopServer(proc);
    if (ownsDir) fs.rmSync(dir, { recursive: true, force: true });
    process.exit(code);
  };
  process.on('SIGTERM', () => shutdown(0));
  process.on('SIGINT', () => shutdown(0));
  process.on('SIGHUP', () => shutdown(0));

  try {
    const bin = await buildServer(dir);
    proc = startServer({ bin, logFile, env: serverEnv({ port, dbPath: path.join(dir, role.db), extra: role.extraEnv }) });
    proc.once('exit', (code) => {
      if (!shuttingDown) {
        console.error(`SubTrackr ${roleName} test server exited with code ${code}:\n${fs.readFileSync(logFile, 'utf8')}`);
        shutdown(code || 1);
      }
    });
    await waitForHealthy(`http://localhost:${port}`, proc, logFile, 60_000);
    console.log(`SubTrackr ${roleName} test server listening on http://localhost:${port} (data: ${dir})`);
  } catch (err) {
    console.error(err.message);
    await shutdown(1);
  }
}

main();
