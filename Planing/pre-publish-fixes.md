# Pre-Publish Fixes — Resolve All Blockers Before npm Publish

## Build Specification for Claude Code

**Purpose:** Fix the 6 blockers preventing npm publish. After applying these fixes, `npm publish` works.  
**Estimated time:** 30 minutes  

---

## The 6 Blockers and Their Fixes

| # | Blocker | Fix |
|---|---------|-----|
| 1 | Binary not in `npm/bin/` | Build it there OR change postinstall to download it |
| 2 | `npm/bin/` might be gitignored | Add exception to `.gitignore` |
| 3 | Version mismatch (npm says 0.1.0, binary says "dev") | Sync via Makefile |
| 4 | npm org `@atlas` may not exist | Check and create, or use unscoped name |
| 5 | wrapper.js not executable | Fix permissions + add to npm scripts |
| 6 | No GitHub remote for Actions workflow | Set up remote OR do manual first publish |

---

## Fix 1: Binary Not in `npm/bin/`

There are two strategies. Pick ONE:

### Strategy A: Postinstall downloads binary from GitHub Releases (RECOMMENDED)

The binary is NOT shipped inside the npm package. Instead, `postinstall.js` downloads it after `npm install`. This is what esbuild, Turbo, and Prisma do.

**Why this is better:**
- npm package stays tiny (~15KB instead of ~15MB)
- One npm package works for all platforms (no 5 platform-specific packages)
- Binary comes from GitHub Releases (already built by CI)

**What to verify:**

```bash
# npm/bin/ should contain ONLY the wrapper script, NOT the binary
ls -la npm/bin/
# Expected:
#   wrapper.js     (the JS entry point — ~2KB)
# NOT expected:
#   universe       (the Go binary — 15MB)
#   universe.exe

# The binary gets downloaded by postinstall.js AFTER npm install
# Check that postinstall.js has the correct GitHub URL:
grep "GITHUB_OWNER" npm/scripts/postinstall.js
grep "GITHUB_REPO" npm/scripts/postinstall.js
# Should show your actual GitHub org and repo name
```

**For manual first publish (before GitHub Actions exists):**

You need the binary to exist somewhere downloadable. Two options:

**Option 1: Create a GitHub Release manually first**

```bash
# 1. Build the binary
VERSION=0.1.0
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe ./cmd/universe

# 2. Go to github.com/your-org/universe/releases
# 3. Click "Create a new release"
# 4. Tag: v0.1.0
# 5. Upload the binary as an asset
# 6. Now postinstall.js can download it

# Build for other platforms too (if you want cross-platform on first release):
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe-darwin-arm64 ./cmd/universe
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe-darwin-amd64 ./cmd/universe
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe-linux-amd64 ./cmd/universe
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe-linux-arm64 ./cmd/universe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe-windows-amd64.exe ./cmd/universe

# Upload ALL of them to the GitHub Release
```

**Option 2: Bundle binary inside npm package for first publish only**

```bash
# Build for your current platform only
VERSION=0.1.0
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o npm/bin/universe ./cmd/universe

# Temporarily add "bin/universe" to npm's files array
# (remove after GitHub Actions is set up)
```

This is a quick-and-dirty first publish. Switch to Strategy A (postinstall download) after GitHub Actions is working.

### Strategy B: Ship binary inside npm package (NOT recommended for production)

If you must ship the binary inside the npm package:

```bash
# Build for current platform
VERSION=0.1.0
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o npm/bin/universe ./cmd/universe

# Update npm/package.json files array to include the binary:
# "files": ["bin/", "scripts/", "README.md", "LICENSE"]
# bin/ now contains both wrapper.js AND the universe binary

# Problem: this only works for YOUR platform
# A macOS developer can't use a Linux binary
# So this is only good for testing, not real distribution
```

---

## Fix 2: .gitignore Check

```bash
# Check if npm/bin/ is gitignored
cd /path/to/universe
git status npm/bin/

# If it shows "nothing to commit" but files exist → it's gitignored

# Check .gitignore for patterns that might match:
grep -n "bin" .gitignore
grep -n "npm" .gitignore

# Common gitignore patterns that accidentally exclude npm/bin/:
#   bin/           ← excludes ALL bin/ directories
#   *.exe          ← excludes Windows binaries
#   /bin           ← only excludes root bin/
```

**Fix:**

Add an exception to `.gitignore`:

```gitignore
# Binaries (general)
bin/
*.exe

# BUT keep the npm wrapper script
!npm/bin/
!npm/bin/wrapper.js

# Don't commit the actual Go binary to git
# (it's downloaded by postinstall or built by CI)
npm/bin/universe
npm/bin/universe.exe
```

**Verify:**

```bash
git add npm/bin/wrapper.js
git status npm/bin/
# Should show: new file: npm/bin/wrapper.js
# Should NOT show: npm/bin/universe (binary should not be in git)
```

---

## Fix 3: Version Sync

The npm package version and the Go binary version MUST match. Otherwise `universe --version` shows the wrong thing.

**The problem:**

```bash
# npm/package.json says:
cat npm/package.json | grep version
# "version": "0.1.0"

# But the Go binary says:
./universe --version
# universe vdev     ← WRONG — should say v0.1.0
```

**The fix — use a Makefile target that syncs both:**

```makefile
# In your Makefile, the release target already handles this:

.PHONY: release
release:
	@if [ -z "$(V)" ]; then echo "Usage: make release V=0.1.0"; exit 1; fi
	
	# Step 1: Update npm package.json version
	cd npm && npm version $(V) --no-git-tag-version
	
	# Step 2: Build binary with matching version
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(V)" -o universe ./cmd/universe
	
	# Step 3: Verify versions match
	@NPM_V=$$(node -e "console.log(require('./npm/package.json').version)"); \
	 BIN_V=$$(./universe --version 2>&1 | grep -o '[0-9][0-9.]*'); \
	 if [ "$$NPM_V" != "$$BIN_V" ]; then \
	   echo "❌ VERSION MISMATCH: npm=$$NPM_V binary=$$BIN_V"; exit 1; \
	 else \
	   echo "✅ Versions match: $$NPM_V"; \
	 fi
	
	# Step 4: Commit + tag + push
	git add -A
	git commit -m "release: v$(V)"
	git tag v$(V)
	git push origin main
	git push origin v$(V)
	
	@echo "✅ Release v$(V) pushed. GitHub Actions will build and publish."
```

**For manual builds (without Makefile):**

```bash
VERSION=0.1.0

# Always set version in BOTH places:
cd npm && npm version $VERSION --no-git-tag-version && cd ..
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o universe ./cmd/universe

# Verify:
./universe --version
# Should print: universe v0.1.0

node -e "console.log(require('./npm/package.json').version)"
# Should print: 0.1.0
```

**Also verify that `main.go` has the Version variable:**

```go
// cmd/universe/main.go — this MUST exist:
var Version = "dev"

// The -ldflags "-X main.Version=0.1.0" overwrites "dev" at compile time
```

---

## Fix 4: npm Account and Organization

```bash
# Step 1: Check if you're logged in to npm
npm whoami
# If not logged in: npm login

# Step 2: Check if @atlas org exists and you have access
npm org ls atlas 2>/dev/null
# If "404" → org doesn't exist, you need to create it
# If shows members → you have access

# Step 3: Check if the package name is available
npm view @atlas/universe
# If "404" → name is available ✓
# If shows package info → name is taken ✗
```

**If `@atlas` is taken or you can't create it:**

Option A: Use a different org name

```bash
# Create your own org at https://www.npmjs.com/org/create
# Examples: @atlas-ai, @universe-ai, @your-company

# Then update npm/package.json:
# "name": "@your-org/universe"

# And update postinstall.js GitHub URLs:
# const GITHUB_OWNER = 'your-org';
# const GITHUB_REPO = 'universe';
```

Option B: Use an unscoped package name

```bash
# Check if "universe-cli" is available:
npm view universe-cli
# If 404 → available

# Update npm/package.json:
# "name": "universe-cli"
# Remove "publishConfig": {"access": "public"} (not needed for unscoped)
```

Option C: Use your username scope

```bash
# Your npm username automatically works as a scope
# "name": "@yourusername/universe"
# No org creation needed
```

**After choosing a name, update these files:**

```bash
# 1. npm/package.json — "name" field
# 2. npm/README.md — install command
# 3. npm/scripts/postinstall.js — GITHUB_OWNER and GITHUB_REPO
# 4. Any documentation referencing the package name
```

---

## Fix 5: File Permissions

The npm `bin` entry (wrapper.js) must be executable on Unix systems.

```bash
# Check current permissions
ls -la npm/bin/wrapper.js
# Should show: -rwxr-xr-x (executable)
# If shows: -rw-r--r-- (not executable) → fix it

# Fix permissions
chmod +x npm/bin/wrapper.js

# Verify the shebang line is present at the top of wrapper.js
head -1 npm/bin/wrapper.js
# Must be: #!/usr/bin/env node
# If missing, npm won't know to run it with Node.js
```

**Also add a prepare script to npm/package.json** as a safety net:

```json
{
  "scripts": {
    "postinstall": "node scripts/postinstall.js",
    "prepare": "chmod +x bin/wrapper.js 2>/dev/null || true"
  }
}
```

The `prepare` script runs before publish, ensuring permissions are correct. The `2>/dev/null || true` makes it silent on Windows (where chmod doesn't exist).

**Git also needs to track the executable bit:**

```bash
# Tell git to preserve the executable permission
git update-index --chmod=+x npm/bin/wrapper.js
git commit -m "fix: make wrapper.js executable"
```

---

## Fix 6: GitHub Remote

```bash
# Check if a remote exists
git remote -v
# If empty → no remote configured

# Add your GitHub remote
git remote add origin https://github.com/your-org/universe.git

# Verify
git remote -v
# origin  https://github.com/your-org/universe.git (fetch)
# origin  https://github.com/your-org/universe.git (push)

# Push everything
git push -u origin main
```

**If you haven't committed anything yet:**

```bash
# Initialize git (if not already)
git init
git branch -M main

# Create .gitignore
cat > .gitignore << 'EOF'
# Go
*.exe
*.exe~
*.dll
*.so
*.dylib

# Build output
/universe
/dist/

# Dependencies
/node_modules/
/dashboard/node_modules/
/dashboard/dist/

# npm binary (downloaded by postinstall, not committed)
npm/bin/universe
npm/bin/universe.exe

# Local config
.universe/
.env

# OS files
.DS_Store
Thumbs.db
EOF

# First commit
git add -A
git commit -m "initial: Universe project with 5 engines"

# Add remote and push
git remote add origin https://github.com/your-org/universe.git
git push -u origin main
```

---

## The Two Publish Paths

### Path A: Manual First Publish (do this NOW to test)

Best for: "I want to test `npm install` works before setting up CI/CD."

```bash
# 1. Make sure you're logged in to npm
npm login

# 2. Build the binary for YOUR platform
VERSION=0.1.0
CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$VERSION" -o npm/bin/universe ./cmd/universe

# 3. Sync npm version
cd npm && npm version $VERSION --no-git-tag-version && cd ..

# 4. Verify
./npm/bin/universe --version
# → universe v0.1.0

# 5. Fix permissions
chmod +x npm/bin/wrapper.js

# 6. Temporarily update npm/package.json to include the binary:
#    Add "bin/universe" to the "files" array (just for this first publish)
#    "files": ["bin/", "scripts/", "README.md", "LICENSE"]
#    (bin/ already includes wrapper.js AND the binary)

# 7. Do a dry run first
cd npm
npm publish --dry-run
# Review what would be published — check file list and size

# 8. Publish for real
npm publish --access public

# 9. Test the install
npm install -g @atlas/universe  # (or whatever name you chose)
universe --version
# → universe v0.1.0

# 10. Clean up — remove the binary from npm/bin/ (don't commit it to git)
rm npm/bin/universe
```

**IMPORTANT: This manual publish only works for YOUR platform.** A Linux developer gets your macOS binary (or vice versa) and it won't run. This is fine for testing — switch to Path B (GitHub Actions) for the real release.

### Path B: GitHub Actions Publish (set up for all future releases)

Best for: "I want `make release V=0.2.0` to handle everything automatically."

```bash
# 1. Push code to GitHub (Fix 6 above)
git push origin main

# 2. Add NPM_TOKEN secret to GitHub
#    github.com/your-org/universe → Settings → Secrets → Actions
#    Name: NPM_TOKEN
#    Value: your npm automation token (from npmjs.com → Profile → Access Tokens)

# 3. Verify the workflow file exists
cat .github/workflows/release.yml
# Should contain: build + release + npm-publish jobs

# 4. Tag and push
make release V=0.1.0
# OR manually:
git tag v0.1.0
git push origin v0.1.0

# 5. Watch GitHub Actions
#    github.com/your-org/universe/actions
#    Should show: build (5 binaries) → release (GitHub Release) → npm-publish

# 6. After ~3-5 minutes, test
npm install -g @atlas/universe
universe --version
# → universe v0.1.0
```

---

## Pre-Publish Checklist — Run This Before Either Path

```bash
#!/bin/bash
echo "╔═══════════════════════════════════════════╗"
echo "║  Pre-Publish Checklist                     ║"
echo "╚═══════════════════════════════════════════╝"
echo ""

PASS=0
FAIL=0

check() {
    if eval "$2" > /dev/null 2>&1; then
        echo "✅ $1"; PASS=$((PASS + 1))
    else
        echo "❌ $1"; FAIL=$((FAIL + 1))
    fi
}

# Binary
check "Go binary compiles" "go build -ldflags '-X main.Version=0.1.0' -o /tmp/universe-check ./cmd/universe"
check "Binary reports correct version" "/tmp/universe-check --version | grep -q '0.1.0'"
rm -f /tmp/universe-check

# npm package
check "npm/package.json exists" "test -f npm/package.json"
check "npm version is 0.1.0" "node -e \"process.exit(require('./npm/package.json').version === '0.1.0' ? 0 : 1)\""
check "npm/bin/wrapper.js exists" "test -f npm/bin/wrapper.js"
check "wrapper.js has shebang" "head -1 npm/bin/wrapper.js | grep -q '#!/usr/bin/env node'"
check "wrapper.js is executable" "test -x npm/bin/wrapper.js"
check "postinstall.js exists" "test -f npm/scripts/postinstall.js"
check "publishConfig has access:public" "node -e \"process.exit(require('./npm/package.json').publishConfig?.access === 'public' ? 0 : 1)\""

# npm account
check "npm logged in" "npm whoami"

# npm name available
NPM_NAME=$(node -e "console.log(require('./npm/package.json').name)")
check "Package name available ($NPM_NAME)" "npm view $NPM_NAME 2>&1 | grep -q '404\|ERR'"

# git
check "Git repo initialized" "git rev-parse --git-dir"
check "Git remote configured" "git remote get-url origin"
check ".gitignore exists" "test -f .gitignore"
check "Binary not tracked by git" "! git ls-files --error-unmatch npm/bin/universe 2>/dev/null"
check "wrapper.js tracked by git" "git ls-files --error-unmatch npm/bin/wrapper.js"

# GitHub Actions (if using Path B)
check "Release workflow exists" "test -f .github/workflows/release.yml"

echo ""
echo "═══════════════════════════════════════════"
echo "  $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════════"

if [ $FAIL -eq 0 ]; then
    echo ""
    echo "🎉 Ready to publish!"
    echo ""
    echo "Path A (manual):  cd npm && npm publish --access public"
    echo "Path B (CI/CD):   make release V=0.1.0"
else
    echo ""
    echo "⚠️  Fix the $FAIL issues above before publishing"
fi
```

Save as `pre-publish-check.sh`, run it, fix anything that fails.

---

## After First Successful Publish

```bash
# Verify it works for real:
npm install -g @atlas/universe   # (or your chosen name)
universe --version               # → universe v0.1.0
universe init                    # scan a codebase
universe setup                   # pick models, generate config
universe status                  # all engines show status

# If everything works:
# 1. Remove the binary from npm/bin/ if you did a manual publish
# 2. Set up GitHub Actions for future releases (Path B)
# 3. Next release: make release V=0.2.0 (fully automatic)
```
