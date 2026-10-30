const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { prepareDataDir, serverEnv, repoRoot } = require('./server-process');

test('data directory guards resolve symlink targets and preserve foreign files', (t) => {
  const base = fs.mkdtempSync(path.join(os.tmpdir(), 'subtrackr-guard-test-'));
  t.after(() => fs.rmSync(base, { recursive: true, force: true }));
  for (const [name, target] of [['home', os.homedir()], ['repo', repoRoot], ['tmp', os.tmpdir()]]) {
    const link = path.join(base, name);
    fs.symlinkSync(target, link);
    assert.throws(() => prepareDataDir(link, 'test'), /refusing to use/);
  }
  const foreign = path.join(base, 'foreign');
  fs.mkdirSync(foreign);
  fs.writeFileSync(path.join(foreign, 'keep'), 'untouched');
  assert.throws(() => prepareDataDir(foreign, 'test'), /refusing to wipe/);
  assert.equal(fs.readFileSync(path.join(foreign, 'keep'), 'utf8'), 'untouched');
  const owned = path.join(base, 'owned');
  prepareDataDir(owned, 'test');
  fs.writeFileSync(path.join(owned, 'old.db'), 'test');
  prepareDataDir(owned, 'test');
  assert.equal(fs.existsSync(path.join(owned, 'old.db')), false);
});

test('test servers do not inherit the live currency credential', () => {
  const previous = process.env.FIXER_API_KEY;
  try {
    process.env.FIXER_API_KEY = 'fake-inherited-key';
    assert.equal(serverEnv({ port: 12345, dbPath: '/tmp/test.db' }).FIXER_API_KEY, '');
  } finally {
    if (previous === undefined) delete process.env.FIXER_API_KEY;
    else process.env.FIXER_API_KEY = previous;
  }
});
