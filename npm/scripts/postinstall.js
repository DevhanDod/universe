#!/usr/bin/env node
'use strict';
// Downloads the universe binary from GitHub Releases after npm install.
// Strategy: detect platform, fetch the matching asset, write to bin/.

const https = require('https');
const fs = require('fs');
const path = require('path');
const os = require('os');

const GITHUB_OWNER = 'Universe';
const GITHUB_REPO  = 'universe';

const pkg = require('../package.json');
const VERSION = pkg.version;

function getPlatformSuffix() {
  const plat = os.platform();
  const arch = os.arch();

  const platMap = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
  const archMap  = { x64: 'amd64', arm64: 'arm64' };

  const p = platMap[plat];
  const a = archMap[arch];

  if (!p) throw new Error(`Unsupported platform: ${plat}`);
  if (!a) throw new Error(`Unsupported architecture: ${arch}`);

  return p === 'windows' ? `${p}-${a}.exe` : `${p}-${a}`;
}

function download(url, dest, redirects = 0) {
  if (redirects > 5) return Promise.reject(new Error('Too many redirects'));

  return new Promise((resolve, reject) => {
    https.get(url, { headers: { 'User-Agent': 'universe-npm-installer' } }, res => {
      if (res.statusCode === 301 || res.statusCode === 302) {
        return download(res.headers.location, dest, redirects + 1)
          .then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Download failed: HTTP ${res.statusCode} — ${url}`));
      }

      const tmp = dest + '.tmp';
      const file = fs.createWriteStream(tmp);
      res.pipe(file);
      file.on('finish', () => {
        file.close(() => {
          fs.renameSync(tmp, dest);
          resolve();
        });
      });
      file.on('error', err => {
        fs.unlink(tmp, () => {});
        reject(err);
      });
    }).on('error', reject);
  });
}

async function install() {
  let suffix;
  try {
    suffix = getPlatformSuffix();
  } catch (e) {
    console.warn(`[universe] Skipping binary download: ${e.message}`);
    return;
  }

  const binaryName = os.platform() === 'win32' ? 'universe.exe' : 'universe';
  const dest = path.join(__dirname, '..', 'bin', binaryName);

  // Already installed
  if (fs.existsSync(dest)) return;

  const assetName = `universe-${suffix}`;
  const url = `https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/v${VERSION}/${assetName}`;

  console.log(`[universe] Downloading ${assetName} v${VERSION}...`);

  try {
    await download(url, dest);
    if (os.platform() !== 'win32') {
      fs.chmodSync(dest, 0o755);
    }
    console.log(`[universe] Installed to ${dest}`);
  } catch (e) {
    console.error(`[universe] Download failed: ${e.message}`);
    console.error(`[universe] You can download manually from:`);
    console.error(`  https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/tag/v${VERSION}`);
    console.error(`  Save as: ${dest}`);
  }
}

install();
