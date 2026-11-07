// Shared machinery for starting private SubTrackr servers from tests.
//
// Used by the Playwright `webServer` runner (e2e-server.js) and by the
// per-worker fixture in settings-server.js. Nothing here goes through a shell:
// the Go toolchain and the built binary are spawned with argument arrays, and
// data directories are only ever wiped when this module created them or when
// they carry the marker file it writes.
const { execFile, spawn } = require('child_process');
const { promisify } = require('util');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..', '..');
const DATA_DIR_MARKER = '.subtrackr-e2e-data';

/** Builds cmd/server (or another package) into `dir` and returns the binary path. */
async function buildServer(dir, pkg = './cmd/server') {
  const bin = path.join(dir, path.basename(pkg) === 'server' ? 'subtrackr' : `subtrackr-${path.basename(pkg)}`);
  await promisify(execFile)('go', ['build', '-o', bin, pkg], { cwd: repoRoot, maxBuffer: 16 * 1024 * 1024 });
  return bin;
}

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

function canConnect(host, port) {
  return new Promise((resolve) => {
    const socket = net.connect({ host, port });
    socket.unref();
    socket.once('connect', () => { socket.destroy(); resolve(true); });
    socket.once('error', () => resolve(false));
  });
}

function canListen(host, port) {
  return new Promise((resolve) => {
    const srv = net.createServer();
    srv.unref();
    srv.once('error', (err) => resolve(host === '::' && err.code === 'EAFNOSUPPORT'));
    srv.listen(port, host, () => srv.close(() => resolve(true)));
  });
}

/**
 * True when nothing answers on `port` for either loopback address and the
 * port can be bound on both IPv4 and IPv6. A server bound to only one stack
 * (for example 127.0.0.1) would otherwise go unnoticed while Gin binds the
 * other, and the suite would silently talk to the wrong process.
 */
async function isPortFree(port) {
  for (const host of ['127.0.0.1', '::1']) {
    if (await canConnect(host, port)) return false;
  }
  for (const host of ['0.0.0.0', '::']) {
    if (!(await canListen(host, port))) return false;
  }
  return true;
}

async function waitForHealthy(baseURL, proc, logFile, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (proc.exitCode !== null) break;
    try {
      const res = await fetch(`${baseURL}/healthz`);
      if (res.ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  const log = fs.existsSync(logFile) ? fs.readFileSync(logFile, 'utf8') : '(no log)';
  throw new Error(`SubTrackr test server on ${baseURL} did not become healthy:\n${log}`);
}

/**
 * Environment for a test server. On macOS, Go's TLS client verifies
 * certificates through the platform (Security.framework) and ignores
 * SSL_CERT_FILE, so the fake SMTP sink's self-signed certificate is only
 * trusted when x509 verification falls back to Go's own verifier.
 */
function serverEnv({ port, dbPath, extra = {} }) {
  const env = { ...process.env, PORT: String(port), DATABASE_PATH: dbPath, FIXER_API_KEY: '', ...extra };
  if (process.platform === 'darwin') {
    const override = 'x509sslcertoverrideplatform=1';
    env.GODEBUG = env.GODEBUG ? `${env.GODEBUG},${override}` : override;
  }
  return env;
}

/** Spawns a built server binary with stdout/stderr appended to `logFile`. */
function startServer({ bin, env, logFile }) {
  const log = fs.openSync(logFile, 'a');
  const proc = spawn(bin, [], { cwd: repoRoot, env, stdio: ['ignore', log, log] });
  proc.once('exit', () => fs.closeSync(log));
  return proc;
}

async function stopServer(proc) {
  if (proc.exitCode !== null) return;
  proc.kill('SIGTERM');
  await new Promise((r) => {
    const timer = setTimeout(() => { proc.kill('SIGKILL'); }, 5000);
    proc.once('exit', () => { clearTimeout(timer); r(); });
  });
}

// Resolve existing ancestors too: a new child may sit below a symlink.
function canonicalPath(dir) {
  if (fs.existsSync(dir)) return fs.realpathSync(dir);
  const parent = path.dirname(dir);
  return path.join(canonicalPath(parent), path.basename(dir));
}

function isProtectedPath(dir) {
  const home = canonicalPath(os.homedir());
  const root = canonicalPath(repoRoot);
  const protectedDirs = new Set([path.parse(dir).root, home, root, canonicalPath(os.tmpdir())]);
  if (protectedDirs.has(dir)) return true;
  return root.startsWith(dir + path.sep) || home.startsWith(dir + path.sep);
}

/**
 * Returns a fresh, empty data directory for a test server.
 *
 * Without `requested`, a private temporary directory is created and the caller
 * owns its removal (`ownsDir` is true). With `requested` (an absolute path or
 * one relative to the repository), the directory is reused only when it is
 * empty, does not exist yet, or was created by this helper before (it holds
 * the marker file); anything else is refused rather than deleted.
 */
function prepareDataDir(requested, label) {
  if (!requested) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), `subtrackr-e2e-${label}-`));
    fs.writeFileSync(path.join(dir, DATA_DIR_MARKER), `${label}\n`);
    return { dir, ownsDir: true };
  }
  const dir = canonicalPath(path.resolve(repoRoot, requested));
  if (isProtectedPath(dir)) {
    throw new Error(`refusing to use ${dir} as the ${label} E2E data directory`);
  }
  if (fs.existsSync(dir)) {
    if (!fs.statSync(dir).isDirectory()) throw new Error(`${dir} is not a directory`);
    const entries = fs.readdirSync(dir);
    if (entries.length > 0 && !entries.includes(DATA_DIR_MARKER)) {
      throw new Error(`refusing to wipe ${dir}: it was not created by the E2E runner (missing ${DATA_DIR_MARKER})`);
    }
    for (const entry of entries) fs.rmSync(path.join(dir, entry), { recursive: true, force: true });
  } else {
    fs.mkdirSync(dir, { recursive: true });
  }
  fs.writeFileSync(path.join(dir, DATA_DIR_MARKER), `${label}\n`);
  return { dir, ownsDir: false };
}

module.exports = {
  repoRoot, DATA_DIR_MARKER, buildServer, freePort, isPortFree, waitForHealthy, serverEnv,
  startServer, stopServer, prepareDataDir,
};
