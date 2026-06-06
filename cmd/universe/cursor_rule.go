package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// universeSessionDigestCommand returns the command Cursor invokes at
// sessionStart. We route through the per-project wrapper at
// .universe/run.cmd|run.sh so hooks.json stays portable across
// machines — the wrapper holds the absolute binary path, the hook
// holds only a relative reference. Matches the same indirection the
// generated rule uses.
func universeSessionDigestCommand() string {
	return universeRunWrapperRelPath() + " session-digest"
}

// renderCursorRuleBody builds the .cursor/rules/universe.mdc contents.
//
// The rule references a per-project wrapper script at .universe/run.cmd
// (Windows) or .universe/run.sh (POSIX) rather than the bare `universe`
// command or an absolute path. The wrapper is written by
// writeUniverseRunWrapper at init time and contains the absolute path
// to whichever universe binary ran init. This makes the rule:
//
//   - PATH-independent — Cursor's Shell tool doesn't need npm's global
//     bin directory in its environment.
//   - Identical across machines — the rule text is the same everywhere
//     because the per-machine detail lives in the wrapper script.
//   - Self-healing — re-running `universe init` rewrites the wrapper
//     against the current binary, so reinstalls don't break the agent.
//
// Kept under ~500 tokens since every byte ships on every turn.
func renderCursorRuleBody() string {
	wrap := universeRunWrapperRelPath()
	return `---
description: "Universe knowledge graph — query-first code intelligence"
alwaysApply: true
---

# Universe Knowledge Graph

A prebuilt knowledge graph of this codebase is available. Query it
via the Shell tool BEFORE grepping or reading source files.

## How to invoke

Run the per-project wrapper from the project root:

  ` + wrap + ` query <SymbolName>

The wrapper resolves the universe binary's absolute path so it works
regardless of Cursor's PATH. If the file is missing, re-run
` + "`universe init`" + ` to regenerate it.

This returns definition location, type, cluster, callers, callees,
flows, and impact in ~200 tokens. Faster and cheaper than grepping
the codebase and reading multiple files.

## Commands

  ` + wrap + ` query <name>     definition + callers + callees + flows
  ` + wrap + ` search <term>    find symbols by name / path / package
  ` + wrap + ` impact <name>    what breaks if you change this
  ` + wrap + ` deps <name>      dependency list

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
