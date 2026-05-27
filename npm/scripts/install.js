#!/usr/bin/env node
// postinstall: make the binary executable on unix
const { execSync } = require('child_process');
const path = require('path');
const os = require('os');

if (os.platform() !== 'win32') {
  const bin = path.join(__dirname, '..', 'bin', 'universe');
  try {
    execSync(`chmod +x "${bin}"`);
  } catch (_) {
    // non-fatal
  }
}
