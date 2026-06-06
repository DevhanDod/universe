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
	return "universe session-digest"
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
	return `---
description: "Universe knowledge graph — query-first code intelligence"
alwaysApply: true
---

# Universe Knowledge Graph

This project has a knowledge graph at .universe/ with symbol definitions,
caller/callee relationships, execution flows, clusters, and impact analysis.

Rules:

* For codebase questions, first run ` + "`universe query \"<name>\"`" + ` when
  .universe/graph.json exists. Use ` + "`universe impact \"<name>\"`" + ` for blast
  radius and ` + "`universe search \"<term>\"`" + ` for broad lookups. These return
  a scoped summary, usually much smaller than grep + file read output.
* Stale .universe/ files are expected after code changes; a stale graph
  is not a reason to skip universe. Only skip universe if the user
  explicitly says not to use it.
* Read source files only when query/impact/search do not surface enough
  context, and then read ONLY the specific lines the graph points to,
  not entire files.
* After modifying code, run ` + "`universe init`" + ` to keep the graph current.
* Trust definition, location, type, and callees from the graph.
  Caller counts may be incomplete — verify with grep if exact counts
  are needed.
* Do NOT read .universe/graph.json directly.
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
