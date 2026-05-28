#!/bin/bash
# Pre-publish check — run this before npm publish.
# Verifies all 6 blockers from pre-publish-fixes.md are resolved.

PASS=0
FAIL=0

check() {
    local desc="$1"
    local cmd="$2"
    if eval "$cmd" >/dev/null 2>&1; then
        echo "  ✅ $desc"
        PASS=$((PASS + 1))
    else
        echo "  ❌ $desc"
        FAIL=$((FAIL + 1))
    fi
}

NPM_VERSION=$(node -e "console.log(require('./npm/package.json').version)" 2>/dev/null)
NPM_NAME=$(node -e "console.log(require('./npm/package.json').name)" 2>/dev/null)

echo "╔═══════════════════════════════════════════════╗"
echo "║  Pre-Publish Check                            ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""
echo "  Package: $NPM_NAME@$NPM_VERSION"
echo ""

# ── Fix 1: Binary ─────────────────────────────────────────────────────────────
echo "── Fix 1: Binary ──"
check "Go binary compiles" \
    "CGO_ENABLED=0 go build -ldflags '-X main.Version=$NPM_VERSION' -o /tmp/universe-precheck ./cmd/universe"

if [ -f /tmp/universe-precheck ]; then
    BIN_VERSION=$(/tmp/universe-precheck --version 2>&1)
    check "Binary reports version $NPM_VERSION" \
        "echo '$BIN_VERSION' | grep -q '$NPM_VERSION'"
    rm -f /tmp/universe-precheck
fi

# ── Fix 2: .gitignore ─────────────────────────────────────────────────────────
echo ""
echo "── Fix 2: .gitignore ──"
check ".gitignore exists" "test -f .gitignore"
check "npm/bin/universe is gitignored" \
    "git check-ignore -q npm/bin/universe 2>/dev/null || echo 'npm/bin/universe' | git check-ignore --stdin -q"
check "npm/bin/wrapper.js is NOT gitignored" \
    "! git check-ignore -q npm/bin/wrapper.js 2>/dev/null"

# ── Fix 3: Version sync ───────────────────────────────────────────────────────
echo ""
echo "── Fix 3: Version ──"
check "npm/package.json has version" "test -n '$NPM_VERSION'"
check "Makefile has release target" "grep -q 'release:' Makefile"

# ── Fix 4: npm account ────────────────────────────────────────────────────────
echo ""
echo "── Fix 4: npm account ──"
NPM_USER=$(npm whoami 2>/dev/null)
if [ -n "$NPM_USER" ]; then
    echo "  ✅ npm logged in as: $NPM_USER"
    PASS=$((PASS + 1))
else
    echo "  ❌ Not logged in to npm (run: npm login)"
    FAIL=$((FAIL + 1))
fi

check "publishConfig has access:public" \
    "node -e \"process.exit(require('./npm/package.json').publishConfig?.access === 'public' ? 0 : 1)\""

echo "     Package name: $NPM_NAME"
if npm view "$NPM_NAME" >/dev/null 2>&1; then
    echo "  ⚠️  $NPM_NAME already exists on npm (update or use a different name)"
else
    echo "  ✅ $NPM_NAME is available on npm"
    PASS=$((PASS + 1))
fi

# ── Fix 5: wrapper.js permissions ────────────────────────────────────────────
echo ""
echo "── Fix 5: wrapper.js ──"
check "npm/bin/wrapper.js exists" "test -f npm/bin/wrapper.js"
check "wrapper.js has shebang" "head -1 npm/bin/wrapper.js | grep -q '#!/usr/bin/env node'"
check "wrapper.js is tracked by git" "git ls-files --error-unmatch npm/bin/wrapper.js"

# Check executable bit via git
GIT_MODE=$(git ls-files -s npm/bin/wrapper.js 2>/dev/null | awk '{print $1}')
if [ "$GIT_MODE" = "100755" ]; then
    echo "  ✅ wrapper.js is executable in git (mode 100755)"
    PASS=$((PASS + 1))
else
    echo "  ❌ wrapper.js is not executable in git (mode: $GIT_MODE)"
    echo "     Fix: git update-index --chmod=+x npm/bin/wrapper.js"
    FAIL=$((FAIL + 1))
fi

# ── Fix 6: GitHub remote ─────────────────────────────────────────────────────
echo ""
echo "── Fix 6: GitHub remote ──"
check "git repo initialized" "git rev-parse --git-dir"
REMOTE=$(git remote get-url origin 2>/dev/null)
if [ -n "$REMOTE" ]; then
    echo "  ✅ git remote configured: $REMOTE"
    PASS=$((PASS + 1))
else
    echo "  ❌ No git remote configured"
    echo "     Fix: git remote add origin https://github.com/your-org/universe.git"
    FAIL=$((FAIL + 1))
fi
check "GitHub Actions release workflow exists" "test -f .github/workflows/release.yml"

# ── npm dry run ───────────────────────────────────────────────────────────────
echo ""
echo "── npm package contents ──"
(cd npm && npm pack --dry-run 2>&1) | grep -E "npm notice|Tarball|files:" || true

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "╔═══════════════════════════════════════════════╗"
printf  "║  Results: %-3d passed, %-3d failed              ║\n" $PASS $FAIL
echo "╚═══════════════════════════════════════════════╝"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "🎉 Ready to publish!"
    echo ""
    echo "  Path A (manual):   cd npm && npm publish --access public"
    echo "  Path B (CI/CD):    make release V=$NPM_VERSION"
else
    echo "⚠️  Fix the $FAIL issue(s) above before publishing."
    echo ""
    echo "  See: Planing/pre-publish-fixes.md"
    exit 1
fi
