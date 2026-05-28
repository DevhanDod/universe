# Post-Publish Fixes — Make Install Work for All Developers

## Apply These Changes to npm/scripts/postinstall.js and npm/bin/wrapper.js

**Problems found during first install test:**
1. Corporate networks block binary download (`self-signed certificate in certificate chain`)
2. Windows Git Bash doesn't have npm global path in PATH
3. Error messages reference wrong package name (`@atlas/universe` instead of `@devhand/universe`)
4. Version mismatch between npm package and GitHub Release tag

---

## Fix 1: Update `npm/scripts/postinstall.js` — Corporate SSL + Better Errors

**REPLACE the entire `npm/scripts/postinstall.js` with this version:**

```javascript
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
        // Ensure target directory exists
        const dir = path.dirname(destPath);
        if (!fs.existsSync(dir)) {
            fs.mkdirSync(dir, { recursive: true });
        }

        const file = fs.createWriteStream(destPath);
        let redirectCount = 0;

        function doRequest(requestUrl) {
            if (redirectCount++ > 5) {
                reject(new Error('Too many redirects'));
                return;
            }

            // SSL options — try with strict SSL first, fall back to permissive
            const requestOptions = {
                headers: {
                    'User-Agent': `@devhand/universe-npm/${require('../package.json').version}`
                }
            };

            // If NODE_TLS_REJECT_UNAUTHORIZED is set to '0', respect it
            // This allows: NODE_TLS_REJECT_UNAUTHORIZED=0 npm install -g @devhand/universe
            if (process.env.NODE_TLS_REJECT_UNAUTHORIZED === '0') {
                requestOptions.rejectUnauthorized = false;
            }

            const protocol = requestUrl.startsWith('https') ? https : require('http');

            protocol.get(requestUrl, requestOptions, (response) => {
                // Follow redirects (GitHub releases redirect to S3/CDN)
                if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
                    doRequest(response.headers.location);
                    return;
                }

                if (response.statusCode === 404) {
                    file.close();
                    fs.unlinkSync(destPath);
                    reject(new Error(
                        `Binary not found (404). Release v${require('../package.json').version} may not have this platform's binary.\n` +
                        `  URL: ${requestUrl}`
                    ));
                    return;
                }

                if (response.statusCode !== 200) {
                    file.close();
                    fs.unlinkSync(destPath);
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
                    fs.unlinkSync(destPath);
                    reject(err);
                });

            }).on('error', (err) => {
                file.close();
                try { fs.unlinkSync(destPath); } catch {}

                // If SSL error and we haven't tried permissive mode yet, retry
                if ((err.message.includes('self-signed') ||
                     err.message.includes('certificate') ||
                     err.message.includes('UNABLE_TO_VERIFY')) &&
                    process.env.NODE_TLS_REJECT_UNAUTHORIZED !== '0') {
                    
                    console.log('[universe] SSL error detected — retrying without certificate validation...');
                    process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
                    doRequest(requestUrl);
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
```

---

## Fix 2: Update `npm/bin/wrapper.js` — Correct Package Name + Better Errors

**REPLACE the entire `npm/bin/wrapper.js` with this version:**

```javascript
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
    // Binary already printed error via stdio: 'inherit'
    process.exit(error.status || 1);
}
```

---

## Fix 3: Update `npm/package.json` — Clean Up

**REPLACE with this exact version:**

```json
{
  "name": "@devhand/universe",
  "version": "0.1.4",
  "description": "Universe — AI token optimization platform for Cursor with 5 engines: Knowledge Graph, Persistent Memory, Self-Evolving Skills, Compression, and Plan-Bridge",
  "keywords": [
    "cursor",
    "ai",
    "mcp",
    "token-optimization",
    "llm",
    "developer-tools",
    "knowledge-graph"
  ],
  "homepage": "https://github.com/DevhanDod/universe",
  "repository": {
    "type": "git",
    "url": "https://github.com/DevhanDod/universe.git"
  },
  "license": "MIT",
  "bin": {
    "universe": "./bin/wrapper.js"
  },
  "files": [
    "bin/wrapper.js",
    "scripts/postinstall.js",
    "README.md"
  ],
  "scripts": {
    "postinstall": "node scripts/postinstall.js",
    "prepare": "node -e \"try{require('fs').chmodSync('bin/wrapper.js',0o755)}catch(e){}\""
  },
  "engines": {
    "node": ">=16"
  },
  "publishConfig": {
    "access": "public"
  },
  "os": [
    "linux",
    "darwin",
    "win32"
  ],
  "cpu": [
    "x64",
    "arm64"
  ]
}
```

---

## Fix 4: Version Alignment

Make sure the npm version and GitHub Release tag match.

**Check your current state:**

```bash
# What version is npm/package.json?
node -e "console.log(require('./npm/package.json').version)"

# What GitHub Release exists?
# Go to: https://github.com/DevhanDod/universe/releases
# You should see v0.1.2 with universe-windows-amd64.exe

# What version does the binary report?
./universe-windows-amd64.exe --version
```

**All three MUST match.** If npm is at 0.1.4 but GitHub Release is v0.1.2:

Option A — Create a new GitHub Release at v0.1.4:

```bash
# Rebuild binary with matching version
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=0.1.4" -o universe-windows-amd64.exe ./cmd/universe

# Verify
./universe-windows-amd64.exe --version
# Should show: universe version 0.1.4

# Go to https://github.com/DevhanDod/universe/releases/new
# Tag: v0.1.4
# Upload: universe-windows-amd64.exe
# Publish
```

Option B — Reset npm to match GitHub Release v0.1.2:

```bash
# Set npm version to match
cd npm
# Manually edit package.json: "version": "0.1.2"
```

**Recommended: Go with Option A** — build a fresh binary at the latest npm version and create a matching GitHub Release.

---

## Fix 5: Add `.gitattributes` for Line Endings

Prevent Windows line ending issues with wrapper.js:

**Create `npm/.gitattributes`:**

```
# Force Unix line endings for scripts that npm runs
bin/wrapper.js text eol=lf
scripts/postinstall.js text eol=lf
```

---

## Apply All Fixes — Step by Step

```bash
# 1. Navigate to project root
cd ~/OneDrive\ -\ IFS/Desktop/Elavate/Universe

# 2. Replace postinstall.js with the fixed version
# (copy the code from Fix 1 above into npm/scripts/postinstall.js)

# 3. Replace wrapper.js with the fixed version
# (copy the code from Fix 2 above into npm/bin/wrapper.js)

# 4. Replace package.json with the fixed version
# (copy from Fix 3 above into npm/package.json)

# 5. Create .gitattributes
echo 'bin/wrapper.js text eol=lf' > npm/.gitattributes
echo 'scripts/postinstall.js text eol=lf' >> npm/.gitattributes

# 6. Build the binary with matching version
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=0.1.4" -o universe-windows-amd64.exe ./cmd/universe

# 7. Verify binary
./universe-windows-amd64.exe --version
# Must show: universe version 0.1.4

# 8. Create GitHub Release v0.1.4
# Go to: https://github.com/DevhanDod/universe/releases/new
# Tag: v0.1.4
# Upload: universe-windows-amd64.exe
# Publish

# 9. Commit and push
git add -A
git commit -m "fix: corporate SSL, PATH hints, correct error messages"
git push

# 10. Publish to npm
cd npm
npm publish --access public

# 11. Test fresh install (in a different folder)
cd ~/Desktop
npm install -g @devhand/universe
universe --version
# Should show: universe version 0.1.4
```

---

## What Other Developers Do After This Fix

```bash
# Normal install (works if no corporate proxy):
npm install -g @devhand/universe
universe --version

# Corporate network install (SSL issues):
NODE_TLS_REJECT_UNAUTHORIZED=0 npm install -g @devhand/universe
universe --version

# Git Bash on Windows (if command not found):
export PATH="$PATH:/c/Users/$USER/AppData/Roaming/npm"
universe --version
# Make permanent:
echo 'export PATH="$PATH:/c/Users/$USER/AppData/Roaming/npm"' >> ~/.bashrc
```

---

## Checklist — Verify All Fixes

```bash
# After applying all fixes and publishing:

# 1. postinstall.js has DevhanDod (not Universe or your-org)
grep "GITHUB_OWNER" npm/scripts/postinstall.js
# Expected: const GITHUB_OWNER = 'DevhanDod';

# 2. postinstall.js has SSL auto-retry
grep "self-signed" npm/scripts/postinstall.js
# Expected: shows the SSL retry logic

# 3. wrapper.js references @devhand/universe (not @atlas)
grep "devhand" npm/bin/wrapper.js
# Expected: @devhand/universe in error messages

# 4. package.json has correct homepage
grep "DevhanDod" npm/package.json
# Expected: shows DevhanDod in homepage and repository

# 5. npm version matches GitHub Release tag
node -e "console.log(require('./npm/package.json').version)"
# Must match the GitHub Release tag (e.g., 0.1.4)

# 6. Binary version matches
./universe-windows-amd64.exe --version
# Must match npm version

# 7. .gitattributes exists
cat npm/.gitattributes
# Expected: shows eol=lf rules
```
