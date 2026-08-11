'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const https = require('node:https');
const crypto = require('node:crypto');
const { spawn } = require('node:child_process');

const SUPPORTED = new Set(['win32-x64', 'darwin-arm64', 'darwin-x64', 'linux-x64', 'linux-arm64']);
function platformAsset(platform = process.platform, arch = process.arch) {
  const key = `${platform}-${arch}`;
  if (!SUPPORTED.has(key)) throw new Error(`Unsupported Gorouter platform: ${platform}/${arch}`);
  return `gorouter-${platform === 'win32' ? 'windows' : platform}-${arch}` + (platform === 'win32' ? '.exe' : '');
}
function buildDownloadURL(version, platform = process.platform, arch = process.arch) {
  return `https://github.com/gorouter/gorouter/releases/download/${version}/${platformAsset(platform, arch)}`;
}
async function withRetries(operation, retries = 3) {
  let last;
  for (let attempt = 0; attempt <= retries; attempt++) { try { return await operation(attempt); } catch (error) { last = error; } }
  throw last;
}
function sha256(content) { return crypto.createHash('sha256').update(content).digest('hex'); }
function verifySha256(content, expectedSha256) {
  if (!expectedSha256) throw new Error('An expected SHA-256 checksum is required');
  const expected = expectedSha256.replace(/^sha256:/, '').toLowerCase();
  if (!/^[a-f0-9]{64}$/.test(expected)) throw new Error('Invalid expected SHA-256 checksum');
  if (sha256(content) !== expected) throw new Error('SHA-256 checksum mismatch');
}
function verifySignature(content, signature, publicKey) {
  if (!signature || !publicKey) throw new Error('Signature verification material is required');
  let key;
  try { key = crypto.createPublicKey(publicKey); } catch { throw new Error('Invalid signature public key'); }
  if (!crypto.verify(null, content, key, signature)) throw new Error('Signature verification failed');
}
async function atomicReplace(target, content, expectedSha256, signature, signaturePublicKey) {
  verifySha256(content, expectedSha256);
  if (signature !== undefined || signaturePublicKey !== undefined) verifySignature(content, signature, signaturePublicKey);
  await fs.mkdir(path.dirname(target), { recursive: true });
  const temporary = path.join(path.dirname(target), `.gorouter-${process.pid}-${Date.now()}.tmp`);
  try { await fs.writeFile(temporary, content, { mode: 0o755 }); await fs.rename(temporary, target); } catch (error) { await fs.rm(temporary, { force: true }); throw error; }
}
function installActions() { return ['download', 'verify-sha256', 'verify-signature', 'atomic-replace']; }
function request(url) { return new Promise((resolve, reject) => https.get(url, response => { if (response.statusCode !== 200) { response.resume(); return reject(new Error(`Download failed: HTTP ${response.statusCode}`)); } const chunks=[]; response.on('data', c => chunks.push(c)); response.on('end', () => resolve(Buffer.concat(chunks))); }).on('error', reject)); }
async function install({ version, platform = process.platform, arch = process.arch, target, request: requestFn = request, expectedSha256, signaturePublicKey }) {
  const destination = target || path.join(os.homedir(), '.gorouter', 'bin', platformAsset(platform, arch));
  if (!expectedSha256) throw new Error('An expected SHA-256 checksum is required');
  if (!signaturePublicKey) throw new Error('Signature verification material is required');
  const payload = await withRetries(() => requestFn(buildDownloadURL(version, platform, arch)));
  const signature = await withRetries(() => requestFn(`${buildDownloadURL(version, platform, arch)}.sig`));
  await atomicReplace(destination, payload, expectedSha256, signature, signaturePublicKey);
  return destination;
}

// Release verification public key (Ed25519 SPKI PEM). Public material, safe to
// embed; the signing identity is rotated for production via GOROUTER_SIGNING_KEY.
const DEFAULT_SIGNING_KEY = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAV1g+Jhg1DgNEDi+zkYtG+veexjLXuiXCXb8JHXrfaMY=
-----END PUBLIC KEY-----`;

const DEFAULT_VERSION = 'v1.2.3';

// Pinned SHA-256 digests for the default release version, produced by the
// deterministic build (tools/build) from the release commit. A non-default
// GOROUTER_VERSION requires an explicit GOROUTER_SHA256.
const DEFAULT_SHA256 = {
  'linux-x64': '9e4ba88110ee35dcc3caeb035e2c4e6f62ee9739bf53d3940ede07747e9a3274',
  'linux-arm64': '5f8230498e1a753bebc5a19f81e1e67d27b36cefff44728d90f7e9e22ea08e31',
  'win32-x64': '66b6e1f1a2b14ea7b829e0b6a9d5f31d2f9ae2799919ad03fa242932c0dd0ac0',
  'darwin-x64': '4224af051845b9d9e50e76299e7613f2f65e8a88d4750769df92adaccfef5e40',
  'darwin-arm64': '6a7206004cc9ddd61333b8b52bd1891c2447d68f7c62fb040bbfb72b63bafd97',
};

function resolveInstallOptions(env = process.env, platform = process.platform, arch = process.arch) {
  const version = env.GOROUTER_VERSION || DEFAULT_VERSION;
  const expectedSha256 = env.GOROUTER_SHA256 || (version === DEFAULT_VERSION ? DEFAULT_SHA256[`${platform}-${arch}`] : undefined);
  const signaturePublicKey = env.GOROUTER_SIGNING_KEY || DEFAULT_SIGNING_KEY;
  return { version, expectedSha256, signaturePublicKey };
}

async function fileExists(target) {
  try { await fs.access(target); return true; } catch { return false; }
}

function runBinary(binaryPath, args, spawnFn = spawn) {
  return new Promise((resolve, reject) => {
    const child = spawnFn(binaryPath, args, { stdio: 'inherit' });
    child.on('error', reject);
    child.on('exit', code => resolve(code === null ? 1 : code));
  });
}

async function runMain(argv, env, installFn = install, spawnFn = runBinary) {
  if (argv[2] === '--version') { console.log('1.2.3'); return 0; }
  const opts = resolveInstallOptions(env);
  const destination = path.join(os.homedir(), '.gorouter', 'bin', platformAsset());
  if (!(await fileExists(destination))) await installFn({ ...opts, target: destination });
  return spawnFn(destination, argv.slice(2));
}

if (require.main === module) {
  runMain(process.argv, process.env)
    .then(code => { process.exitCode = code; })
    .catch(error => { console.error(error.message); process.exitCode = 1; });
}

module.exports = { platformAsset, buildDownloadURL, withRetries, sha256, verifySha256, verifySignature, atomicReplace, installActions, install, DEFAULT_SIGNING_KEY, DEFAULT_VERSION, DEFAULT_SHA256, resolveInstallOptions, fileExists, runBinary, runMain };
