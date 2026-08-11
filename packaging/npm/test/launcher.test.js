const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const launcher = require('../launcher');

test('buildDownloadURL pins the requested version and platform', () => {
  assert.equal(launcher.buildDownloadURL('v1.2.3', 'linux', 'x64'), 'https://github.com/gorouter/gorouter/releases/download/v1.2.3/gorouter-linux-x64');
});

test('unsupported platform fails clearly', () => {
  assert.throws(() => launcher.platformAsset('aix', 'ppc64'), /Unsupported Gorouter platform: aix\/ppc64/);
});

test('retry policy is bounded', async () => {
  let attempts = 0;
  await assert.rejects(() => launcher.withRetries(async () => { attempts++; throw new Error('down'); }, 2), /down/);
  assert.equal(attempts, 3);
});

test('atomic verified replacement preserves existing data', async () => {
  const fs = require('node:fs/promises');
  const os = require('node:os');
  const path = require('node:path');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'gorouter-'));
  const target = path.join(dir, 'bin');
  const data = path.join(dir, 'data');
  await fs.writeFile(target, 'old'); await fs.writeFile(data, 'config');
  const crypto = require('node:crypto');
  const digest = crypto.createHash('sha256').update('new').digest('hex');
  await launcher.atomicReplace(target, Buffer.from('new'), digest);
  assert.equal(await fs.readFile(target, 'utf8'), 'new');
  assert.equal(await fs.readFile(data, 'utf8'), 'config');
});

test('atomic replacement rejects a payload whose SHA-256 does not match the pinned digest', async () => {
  const fs = require('node:fs/promises');
  const os = require('node:os');
  const path = require('node:path');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'gorouter-'));
  const target = path.join(dir, 'bin');
  await assert.rejects(
    () => launcher.atomicReplace(target, Buffer.from('tampered'), 'sha256:' + '0'.repeat(64)),
    /SHA-256 checksum mismatch/
  );
  await assert.rejects(() => fs.access(target));
});

test('install verifies a separately supplied digest before replacing the binary', async () => {
  const crypto = require('node:crypto');
  const fs = require('node:fs/promises');
  const os = require('node:os');
  const path = require('node:path');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'gorouter-'));
  const target = path.join(dir, 'bin');
  const payload = Buffer.from('trusted');
  const digest = crypto.createHash('sha256').update(payload).digest('hex');
  const { privateKey, publicKey } = crypto.generateKeyPairSync('ed25519');
  const signature = crypto.sign(null, payload, privateKey);
  await launcher.install({ version: 'v1.2.3', platform: 'linux', arch: 'x64', target, request: async url => url.endsWith('.sig') ? signature : payload, expectedSha256: digest, signaturePublicKey: publicKey.export({ type: 'spki', format: 'pem' }) });
  assert.equal(await fs.readFile(target, 'utf8'), 'trusted');
});

test('install verifies a detached Ed25519 signature before replacing the binary', async () => {
  const crypto = require('node:crypto');
  const fs = require('node:fs/promises');
  const os = require('node:os');
  const path = require('node:path');
  const { privateKey, publicKey } = crypto.generateKeyPairSync('ed25519');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'gorouter-'));
  const target = path.join(dir, 'bin');
  const payload = Buffer.from('signed');
  const signature = crypto.sign(null, payload, privateKey).toString('base64');
  const digest = crypto.createHash('sha256').update(payload).digest('hex');
  const request = async url => url.endsWith('.sig') ? Buffer.from(signature, 'base64') : payload;
  await launcher.install({ version: 'v1.2.3', platform: 'linux', arch: 'x64', target, request, expectedSha256: digest, signaturePublicKey: publicKey.export({ type: 'spki', format: 'pem' }) });
  assert.equal(await fs.readFile(target, 'utf8'), 'signed');
});

test('install rejects a missing or invalid detached signature', async () => {
  const crypto = require('node:crypto');
  const { publicKey } = crypto.generateKeyPairSync('ed25519');
  const payload = Buffer.from('unsigned');
  const digest = crypto.createHash('sha256').update(payload).digest('hex');
  await assert.rejects(() => launcher.install({ version: 'v1.2.3', platform: 'linux', arch: 'x64', request: async url => url.endsWith('.sig') ? Buffer.from('bad') : payload, expectedSha256: digest, signaturePublicKey: publicKey.export({ type: 'spki', format: 'pem' }) }), /Signature verification failed/);
});

test('install fails closed when verification material is missing', async () => {
  await assert.rejects(
    () => launcher.install({ version: 'v1.2.3', platform: 'linux', arch: 'x64', request: async () => Buffer.from('payload') }),
    /expected SHA-256 checksum/
  );
});

// === PRODUCTION WIRING TESTS ===

test('install() requires explicit signaturePublicKey (fail-closed)', async () => {
  await assert.rejects(
    () => launcher.install({ version: 'v1.2.3', platform: 'linux', arch: 'x64', request: async () => Buffer.from('payload'), expectedSha256: '0000000000000000000000000000000000000000000000000000000000000000' }),
    /Signature verification material is required/
  );
});

test('resolveInstallOptions returns genuine verification material for the production path', () => {
  const opts = launcher.resolveInstallOptions({}, 'linux', 'x64');
  assert.match(opts.expectedSha256, /^[0-9a-f]{64}$/);
  assert.equal(opts.version, 'v1.2.3');
  crypto.createPublicKey(opts.signaturePublicKey);
});

test('resolveInstallOptions uses GOROUTER_SIGNING_KEY env override when set', () => {
  const kp = crypto.generateKeyPairSync('ed25519');
  const pem = kp.publicKey.export({ type: 'spki', format: 'pem' });
  const opts = launcher.resolveInstallOptions({ GOROUTER_SIGNING_KEY: pem, GOROUTER_SHA256: 'aa'.repeat(32) }, 'linux', 'x64');
  assert.equal(opts.signaturePublicKey, pem);
  assert.equal(opts.expectedSha256, 'aa'.repeat(32));
  assert.equal(opts.version, 'v1.2.3');
});

test('resolveInstallOptions returns undefined digest for non-default version (fail-closed)', () => {
  const opts = launcher.resolveInstallOptions({ GOROUTER_VERSION: 'v2.0.0' }, 'linux', 'x64');
  assert.equal(opts.version, 'v2.0.0');
  assert.equal(opts.expectedSha256, undefined);
});

test('production path (resolveInstallOptions -> install) verifies and installs a signed binary', async () => {
  const crypto = require('node:crypto');
  const fs = require('node:fs/promises');
  const os = require('node:os');
  const path = require('node:path');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'gorouter-prod-test-'));
  const target = path.join(dir, 'bin');
  const payload = Buffer.from('production-binary');
  const digest = crypto.createHash('sha256').update(payload).digest('hex');
  const kp = crypto.generateKeyPairSync('ed25519');
  const pem = kp.publicKey.export({ type: 'spki', format: 'pem' });
  const signature = crypto.sign(null, payload, kp.privateKey);
  const opts = launcher.resolveInstallOptions({ GOROUTER_SIGNING_KEY: pem, GOROUTER_SHA256: digest }, 'linux', 'x64');
  const request = async url => url.endsWith('.sig') ? signature : payload;
  await launcher.install({ ...opts, platform: 'linux', arch: 'x64', target, request });
  assert.equal(await fs.readFile(target, 'utf8'), 'production-binary');
});

test('runMain spawns the installed binary with the original arguments', async () => {
  const os = require('node:os');
  const path = require('node:path');
  const expectedBinary = path.join(os.homedir(), '.gorouter', 'bin', launcher.platformAsset());
  let captured;
  const mockSpawnBinary = async (binaryPath, args) => { captured = { binaryPath, args }; return 0; };
  const mockInstall = async () => {};
  const code = await launcher.runMain(['node', 'launcher.js', '--help'], {}, mockInstall, mockSpawnBinary);
  assert.equal(captured.binaryPath, expectedBinary);
  assert.deepEqual(captured.args, ['--help']);
  assert.equal(code, 0);
});

test('DEFAULT_SIGNING_KEY is a valid SPKI public key', () => {
  crypto.createPublicKey(launcher.DEFAULT_SIGNING_KEY);
});
