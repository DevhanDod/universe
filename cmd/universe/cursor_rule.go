package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// cursorRuleBody is intentionally under ~400 tokens — every byte here
// is paid on every turn of every chat. The shape (Graphify model) is
// rule + shell CLI: tell the agent to run `universe query <name>` via
// Cursor's built-in Shell tool before grepping, and trust the output.
// Shell tool results land in the agent's context the same way any
// other tool result does, which is the one path Cursor reliably honors.
const cursorRuleBody = `---
description: "Universe knowledge graph — query-first code intelligence"
alwaysApply: true
---

# Universe Knowledge Graph

A prebuilt knowledge graph of this codebase is available. Query it
via the terminal BEFORE grepping or reading source files.

## When to use

For any question about code structure, definitions, callers, callees,
dependencies, or impact — run the query first:

  universe query <SymbolName>

This returns definition location, type, cluster, callers, callees,
execution flows, and impact in ~200 tokens. Faster and cheaper than
grepping the codebase and reading multiple files.

## Commands

  universe query <name>     definition + callers + callees + flows
  universe search <term>    find symbols by name / path / package
  universe impact <name>    what breaks if you change this
  universe deps <name>      dependency list

## When NOT to use

- Creating or editing files (just edit them directly).
- Running tests, builds, or deployments.
- Questions unrelated to code structure.
- If ` + "`universe query`" + ` returns "not found", fall back to Grep + Read.

## Important

- Trust definition, location, type, and callees from the graph.
- Caller counts may be incomplete (graph coverage varies by project).
  If you need an exact caller list, verify with Grep.
- If you need the actual source code of a function (not just its
  relationships), read ONLY the specific lines the graph shows
  (e.g. file.go:36-50), not the entire file.
`

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
	if existing, err := os.ReadFile(path); err == nil && string(existing) == cursorRuleBody {
		return false, path, nil
	}
	if err := os.WriteFile(path, []byte(cursorRuleBody), 0o644); err != nil {
		return false, path, err
	}
	return true, path, nil
}
