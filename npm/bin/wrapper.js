#!/usr/bin/env node

const { execFileSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

// Binary location — downloaded by postinstall.js, sits next to this file
const ext = os.platform() === 'win32' ? '.exe' : '';
const binaryName = `universe${ext}`;
const binaryPath = path.join(__dirname, binaryName);

// Check if binary exists
if (!fs.existsSync(binaryPath)) {
    const pkg = require('../package.json');

    console.error('');
    console.error('  universe binary not found.');
    console.error('');
    console.error('  The binary was not downloaded during installation.');
    console.error('  This usually happens because of a corporate proxy/firewall.');
    console.error('');
    console.error('  Fix option 1 — retry with SSL bypass:');
    console.error('    NODE_TLS_REJECT_UNAUTHORIZED=0 npm install -g @devhand/universe');
    console.error('');
    console.error('  Fix option 2 — run postinstall manually:');
    console.error(`    node "${path.join(__dirname, '..', 'scripts', 'postinstall.js')}"`);
    console.error('');
    console.error('  Fix option 3 — download manually:');
    console.error(`    https://github.com/DevhanDod/universe/releases/tag/v${pkg.version}`);
    console.error(`    Save as: ${binaryPath}`);
    console.error('');

    process.exit(1);
}

// Ensure binary is executable (Unix)
if (os.platform() !== 'win32') {
    try {
        fs.accessSync(binaryPath, fs.constants.X_OK);
    } catch {
        fs.chmodSync(binaryPath, 0o755);
    }
}

// Run the binary with all arguments passed through
try {
    execFileSync(binaryPath, process.argv.slice(2), {
        stdio: 'inherit',
        env: {
            ...process.env,
            UNIVERSE_INSTALLED_VIA: 'npm',
            UNIVERSE_VERSION: require('../package.json').version
        }
    });
} catch (error) {
    // execFileSync throws on non-zero exit code
    // Binary already printed its error via stdio: 'inherit'
    process.exit(error.status || 1);
}
