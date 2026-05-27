#!/usr/bin/env node
const { spawnSync } = require('child_process');
const path = require('path');
const os = require('os');

const isWin = os.platform() === 'win32';
const bin = path.join(__dirname, '..', 'bin', isWin ? 'universe.exe' : 'universe');

const result = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });

if (result.error) {
  if (result.error.code === 'ENOENT') {
    console.error('universe binary not found at: ' + bin);
    console.error('Try reinstalling: npm install -g @atlas/universe');
  } else {
    console.error(result.error.message);
  }
  process.exit(1);
}

process.exit(result.status ?? 0);
