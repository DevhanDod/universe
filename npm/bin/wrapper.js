#!/usr/bin/env node
'use strict';
const { spawnSync } = require('child_process');
const path = require('path');
const os = require('os');
const fs = require('fs');

const isWin = os.platform() === 'win32';
const bin = path.join(__dirname, isWin ? 'universe.exe' : 'universe');

if (!fs.existsSync(bin)) {
  console.error('universe binary not found at: ' + bin);
  console.error('');
  console.error('Try reinstalling:');
  console.error('  npm install -g @atlas/universe');
  console.error('');
  console.error('Or run the postinstall script manually:');
  console.error('  node ' + path.join(__dirname, '..', 'scripts', 'postinstall.js'));
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });

if (result.error) {
  console.error('Failed to run universe: ' + result.error.message);
  process.exit(1);
}

process.exit(result.status ?? 0);
