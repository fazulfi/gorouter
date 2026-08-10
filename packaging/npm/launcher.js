'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const https = require('node:https');
const crypto = require('node:crypto');

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
if (require.main === module) { if (process.argv[2] === '--version') console.log('1.2.3'); else install({ version: process.env.GOROUTER_VERSION || 'v1.2.3', expectedSha256: process.env.GOROUTER_SHA256 }).catch(error => { console.error(error.message); process.exitCode = 1; }); }
module.exports = { platformAsset, buildDownloadURL, withRetries, sha256, verifySha256, verifySignature, atomicReplace, installActions, install };
