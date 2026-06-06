package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// renderCursorRuleBody builds the .cursor/rules/universe.mdc contents
// with the absolute path of the running universe binary baked into every
// command example.
//
// Why bake the path: Cursor launches Shell tool calls with a clean
// environment, and the npm global bin directory is frequently missing
// from that PATH on Windows. A rule that says "run `universe query`"
// gets a "command not found" response, the agent gives up, and reaches
// for Grep+Read instead. By writing the absolute path into the rule we
// remove that failure mode.
//
// We also expose a UNIVERSE_BIN env-var convention — projects that
// genuinely want a stable name (CI, shared dotfiles) can set it; the
// rule still mentions the literal path so the agent doesn't have to
// guess. Kept under ~500 tokens.
func renderCursorRuleBody() string {
	bin := universeBinaryPath()
	q := bin
	if needsQuoting(bin) {
		q = `"` + bin + `"`
	}
	return `---
description: "Universe knowledge graph — query-first code intelligence"
alwaysApply: true
---

# Universe Knowledge Graph

A prebuilt knowledge graph of this codebase is available. Query it
via the Shell tool BEFORE grepping or reading source files.

## How to invoke

Cursor's PATH does not always include the npm global bin directory,
so call the universe binary by its absolute path:

  ` + q + ` query <SymbolName>

(Adjust the path if your install lives elsewhere — find it with
` + "`where universe`" + ` on Windows or ` + "`which universe`" + ` on Mac/Linux.)

This returns definition location, type, cluster, callers, callees,
flows, and impact in ~200 tokens. Faster and cheaper than grepping
the codebase and reading multiple files.

## Commands (replace <bin> with the absolute path above)

  <bin> query <name>     definition + callers + callees + flows
  <bin> search <term>    find symbols by name / path / package
  <bin> impact <name>    what breaks if you change this
  <bin> deps <name>      dependency list

## When NOT to use

- Creating or editing files (just edit them directly).
- Running tests, builds, or deployments.
- Questions unrelated to code structure.
- If ` + "`query`" + ` returns "not found", fall back to Grep + Read.

## Important

- Trust definition, location, type, and callees from the graph.
- Caller counts may be incomplete (graph coverage varies by project).
  If you need an exact caller list, verify with Grep.
- If you need the actual source code of a function (not just its
  relationships), read ONLY the specific lines the graph shows
  (e.g. file.go:36-50), not the entire file.
- Do NOT read .universe/graph.json directly — it is large and the
  query command extracts what you need.
`
}

// universeBinaryPath returns the absolute path of the currently running
// universe binary so it can be embedded in generated config files.
// Falls back to the bare name "universe" if the OS doesn't expose it.
func universeBinaryPath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "universe"
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}

// needsQuoting reports whether a path contains characters that the
// shell would tokenize on. Spaces are the common case (Program Files,
// "OneDrive - Company") but we wrap anything with a space-equivalent.
func needsQuoting(p string) bool {
	for _, c := range p {
		if c == ' ' || c == '\t' {
			return true
		}
	}
	return false
}

// renderHooksBody returns the .cursor/hooks.json contents.
//
// v0.4.0 drops preToolUse / postToolUse entirely. Both were tried (v0.3.x)
// and confirmed to be non-functional in Cursor — Cursor doesn't inject
// hook stdout into the agent context for those events. The one hook
// channel that does deliver is sessionStart, where the JSON response's
// `additional_context` field IS injected into the model context for the
// whole session. We use it to drop a small project digest.
//
// Schema matches https://cursor.com/docs/hooks.md: top-level "version": 1,
// camelCase event keys, command as a string, timeout in seconds.
func renderHooksBody() string {
	cmd := universeSessionDigestCommand()
	return `{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "command": ` + jsonString(cmd) + `,
        "timeout": 5
      }
    ]
  }
}
`
}

// universeSessionDigestCommand returns the command Cursor invokes at
// session start. We resolve the absolute path of the running binary so
// Cursor's clean PATH (which often omits the npm global bin on Windows)
// doesn't matter.
func universeSessionDigestCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "universe"
	}
	return strconv.Quote(exe) + " session-digest"
}

// jsonString returns `s` as a JSON-safe quoted string. Wrapper around
// strconv.Quote with a tiny rename so the embedded JSON above reads
// naturally.
func jsonString(s string) string {
	return strconv.Quote(s)
}

// writeCursorHooks writes .cursor/hooks.json if and only if the file
// doesn't already exist. We never overwrite — a user might have hand-
// edited hooks for their own workflow, and silently clobbering that
// would be hostile.
func writeCursorHooks(projectDir string) (bool, string, error) {
	path := filepath.Join(projectDir, ".cursor", "hooks.json")
	if _, err := os.Stat(path); err == nil {
		return false, path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, path, err
	}
	if err := os.WriteFile(path, []byte(renderHooksBody()), 0o644); err != nil {
		return false, path, err
	}
	return true, path, nil
}

// writeCursorRule writes our steering rule to .cursor/rules/universe.mdc
// in the project root. Returns (wrote, path, err) — wrote=false when an
// existing identical file is already there, so re-running init is quiet.
func writeCursorRule(projectDir string) (bool, string, error) {
	dir := filepath.Join(projectDir, ".cursor", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, "", err
	}
	path := filepath.Join(dir, "universe.mdc")
	body := renderCursorRuleBody()
	if existing, err := os.ReadFile(path); err == nil && string(existing) == body {
		return false, path, nil
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, path, err
	}
	return true, path, nil
}
