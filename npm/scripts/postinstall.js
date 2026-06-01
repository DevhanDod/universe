#!/usr/bin/env node

const os = require('os');
const fs = require('fs');
const path = require('path');
const https = require('https');

// ============================================================
// CONFIGURATION — update these to match your GitHub repo
// ============================================================

const GITHUB_OWNER = 'DevhanDod';
const GITHUB_REPO  = 'universe';

// Platform → GitHub Release asset name mapping
// These names MUST match what you upload to GitHub Releases
const PLATFORM_MAP = {
    'win32-x64':     'universe-windows-amd64.exe',
    'win32-arm64':   'universe-windows-arm64.exe',
    'darwin-x64':    'universe-darwin-amd64',
    'darwin-arm64':  'universe-darwin-arm64',
    'linux-x64':     'universe-linux-amd64',
    'linux-arm64':   'universe-linux-arm64',
};

// ============================================================
// MAIN
// ============================================================

async function main() {
    const pkg = require('../package.json');
    const VERSION = pkg.version;
    const platformKey = `${os.platform()}-${os.arch()}`;
    const assetName = PLATFORM_MAP[platformKey];

    if (!assetName) {
        console.error(`[universe] Unsupported platform: ${platformKey}`);
        console.error(`[universe] Supported: ${Object.keys(PLATFORM_MAP).join(', ')}`);
        console.error(`[universe] Build from source: https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}`);
        // Don't exit 1 — let npm install complete, wrapper.js will show error later
        return;
    }

    // Binary destination — sits next to wrapper.js in bin/
    const ext = os.platform() === 'win32' ? '.exe' : '';
    const targetPath = path.join(__dirname, '..', 'bin', `universe${ext}`);

    // Skip if binary already exists and is correct version
    if (fs.existsSync(targetPath)) {
        try {
            const { execFileSync } = require('child_process');
            const output = execFileSync(targetPath, ['--version'], {
                encoding: 'utf8',
                timeout: 5000
            });
            if (output.includes(VERSION)) {
                console.log(`[universe] v${VERSION} already installed ✓`);
                return;
            }
        } catch {
            // Binary exists but wrong version or broken — re-download
        }
    }

    // Download URL
    const url = `https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/v${VERSION}/${assetName}`;

    console.log(`[universe] Downloading ${assetName} v${VERSION}...`);

    try {
        await downloadFile(url, targetPath);

        // Make executable on Unix
        if (os.platform() !== 'win32') {
            fs.chmodSync(targetPath, 0o755);
        }

        // Verify the binary runs
        try {
            const { execFileSync } = require('child_process');
            const output = execFileSync(targetPath, ['--version'], {
                encoding: 'utf8',
                timeout: 5000
            });
            console.log(`[universe] ${output.trim()} installed ✓`);
        } catch {
            console.log('[universe] Binary downloaded but verification skipped');
        }

        console.log('');
        console.log('Get started:');
        console.log('  universe init        Scan your codebase');
        console.log('  universe setup       Configure models');
        console.log('  universe status      Check engines');
        console.log('');

        // Windows Git Bash PATH hint
        if (os.platform() === 'win32') {
            console.log('Note for Git Bash users:');
            console.log('  If "universe" command is not found, add npm to your PATH:');
            console.log('  export PATH="$PATH:/c/Users/$USER/AppData/Roaming/npm"');
            console.log('  Make permanent: echo the line above >> ~/.bashrc');
            console.log('');
        }

    } catch (error) {
        console.error(`[universe] Download failed: ${error.message}`);

        // Detect common corporate network issues
        if (error.message.includes('self-signed certificate') ||
            error.message.includes('certificate') ||
            error.message.includes('UNABLE_TO_VERIFY')) {
            console.error('');
            console.error('[universe] This looks like a corporate proxy/firewall issue.');
            console.error('[universe] Try installing with SSL check disabled:');
            console.error('');
            console.error('  NODE_TLS_REJECT_UNAUTHORIZED=0 npm install -g @devhand/universe');
            console.error('');
            console.error('Or download the binary manually:');
        } else {
            console.error('[universe] You can download manually from:');
        }

        console.error(`  ${url}`);
        console.error(`  Save as: ${targetPath}`);
        console.error('');

        // Don't exit 1 — let npm install succeed
        // The wrapper.js will show a clear error when the user tries to run it
    }
}

// ============================================================
// DOWNLOAD — follows redirects, handles SSL issues
// ============================================================

function downloadFile(url, destPath) {
    return new Promise((resolve, reject) => {
        const dir = path.dirname(destPath);
        if (!fs.existsSync(dir)) {
            fs.mkdirSync(dir, { recursive: true });
        }

        let redirectCount = 0;
        let insecure = process.env.NODE_TLS_REJECT_UNAUTHORIZED === '0';

        function doRequest(requestUrl) {
            if (redirectCount++ > 5) {
                reject(new Error('Too many redirects'));
                return;
            }

            // Fresh write stream per attempt — reusing a closed stream silently
            // drops the response data, which is what caused retries to hang.
            const file = fs.createWriteStream(destPath);

            const requestOptions = {
                headers: {
                    'User-Agent': `@devhand/universe-npm/${require('../package.json').version}`
                }
            };
            if (insecure) {
                requestOptions.rejectUnauthorized = false;
            }

            const protocol = requestUrl.startsWith('https') ? https : require('http');

            protocol.get(requestUrl, requestOptions, (response) => {
                if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
                    file.close();
                    try { fs.unlinkSync(destPath); } catch {}
                    doRequest(response.headers.location);
                    return;
                }

                if (response.statusCode === 404) {
                    file.close();
                    try { fs.unlinkSync(destPath); } catch {}
                    reject(new Error(
                        `Binary not found (404). Release v${require('../package.json').version} may not have this platform's binary.\n` +
                        `  URL: ${requestUrl}`
                    ));
                    return;
                }

                if (response.statusCode !== 200) {
                    file.close();
                    try { fs.unlinkSync(destPath); } catch {}
                    reject(new Error(`HTTP ${response.statusCode}: ${response.statusMessage}`));
                    return;
                }

                const total = parseInt(response.headers['content-length'], 10);
                let downloaded = 0;

                response.on('data', (chunk) => {
                    downloaded += chunk.length;
                    if (total && process.stderr.isTTY) {
                        const pct = Math.round((downloaded / total) * 100);
                        const mb = (downloaded / 1024 / 1024).toFixed(1);
                        process.stderr.write(`\r[universe] Downloading... ${pct}% (${mb}MB)`);
                    }
                });

                response.pipe(file);

                file.on('finish', () => {
                    file.close();
                    if (process.stderr.isTTY) {
                        process.stderr.write('\r[universe] Download complete.              \n');
                    } else {
                        console.log('[universe] Download complete.');
                    }
                    resolve();
                });

                file.on('error', (err) => {
                    try { fs.unlinkSync(destPath); } catch {}
                    reject(err);
                });

            }).on('error', (err) => {
                file.close();
                try { fs.unlinkSync(destPath); } catch {}

                if ((err.message.includes('self-signed') ||
                     err.message.includes('certificate') ||
                     err.message.includes('UNABLE_TO_VERIFY')) &&
                    !insecure) {

                    console.log('[universe] SSL error detected — retrying without certificate validation...');
                    insecure = true;
                    redirectCount = 0;
                    doRequest(url);
                    return;
                }

                reject(err);
            });
        }

        doRequest(url);
    });
}

// ============================================================
// RUN
// ============================================================

main().catch((error) => {
    console.error('[universe] postinstall error:', error.message);
    // Don't exit 1 — npm install should still succeed
});
