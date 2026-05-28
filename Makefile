.PHONY: build test clean release dashboard

# ── Local build ───────────────────────────────────────────────────────────────

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=dev" -o universe ./cmd/universe

build-all:
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(V)" -o dist/universe-linux-amd64   ./cmd/universe
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags "-s -w -X main.Version=$(V)" -o dist/universe-linux-arm64   ./cmd/universe
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(V)" -o dist/universe-darwin-amd64  ./cmd/universe
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags "-s -w -X main.Version=$(V)" -o dist/universe-darwin-arm64  ./cmd/universe
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(V)" -o dist/universe-windows-amd64.exe ./cmd/universe

# ── Dashboard ─────────────────────────────────────────────────────────────────

dashboard:
	cd dashboard && npm ci && npm run build

# ── Tests ─────────────────────────────────────────────────────────────────────

test:
	CGO_ENABLED=0 go test ./... -count=1

engine-check:
	bash universe-engine-check.sh

test-local:
	bash test-local.sh

# ── Release ───────────────────────────────────────────────────────────────────

release:
	@if [ -z "$(V)" ]; then echo "Usage: make release V=0.1.0"; exit 1; fi

	@echo "── Syncing versions to $(V) ──"

	# Sync npm package version
	cd npm && npm version $(V) --no-git-tag-version

	# Build local binary to verify it compiles with this version
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(V)" -o /tmp/universe-release-check ./cmd/universe

	@BIN_V=$$(/tmp/universe-release-check --version 2>&1 | tr -d '\n'); \
	 echo "  Binary reports: $$BIN_V"; \
	 echo "  npm version:    $(V)"
	@rm -f /tmp/universe-release-check

	@echo ""
	@echo "── Committing and tagging ──"
	git add npm/package.json
	git commit -m "release: v$(V)"
	git tag v$(V)

	@echo ""
	@echo "── Pushing to GitHub ──"
	git push origin main
	git push origin v$(V)

	@echo ""
	@echo "✅ Release v$(V) pushed."
	@echo "   GitHub Actions will build 5 platform binaries and publish to npm."
	@echo "   Watch: https://github.com/Universe/universe/actions"

# ── Cleanup ───────────────────────────────────────────────────────────────────

clean:
	rm -f universe universe.exe
	rm -rf dist/
