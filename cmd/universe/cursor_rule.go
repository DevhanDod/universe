package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// cursorRuleBody is the steering rule we drop into the user's project so
// Cursor (and any other MCP-aware agent that reads .cursor/rules) prefers
// our compact MCP tools over slurping .universe/graph.json raw. The latter
// was happening in practice and was the dominant cause of token blow-ups.
const cursorRuleBody = `---
description: "Universe knowledge graph — hook-delivered, no MCP tools"
alwaysApply: true
---

# Universe — Hook-Delivered Code Intelligence

Universe gives you a precomputed knowledge graph of this codebase
(callers, callees, flows, clusters, impact, confidence) automatically
through Cursor's PreToolUse hook. There is no tool to call.

## How it works

When you Read / Grep / Search a symbol, the hook checks the graph
BEFORE the tool runs and prints the answer into your context. You will
see a block like:

  [Universe] ErrorResponse [struct] test-server.go:58 (test_server)
  Callers (1): sendError [method] test-server.go:563
  Flows: handleRequest
  Impact: low — 1 affected node(s)
  Confidence: all relationships ≥90%
  Graph has complete context. File read is likely unnecessary.

## What to do with hook output

- "File read is likely unnecessary" (confidence ≥80%) — SKIP the file
  read entirely. The graph answered the question.
- "Targeted file read may help verify" (confidence 50–80%) — Read ONLY
  the line range the hook showed (e.g. test-server.go:58–66), not the
  whole file.
- No hook output — graph has no data for this symbol; proceed normally.

## Shell commands (optional, on demand)

- universe query <name>    same data as the hook, on demand
- universe impact <name>   blast radius (--max-depth, --min-confidence)
- universe search <term>   find symbols by name / path / package
- universe deps <name>     direct callers + callees
- universe recall <query>  past observations from memory

## Write commands (shell, structured input)

- universe store-observation --category <fix|pattern|decision> --summary "..." [--node ID]
- universe report-skill --skill-id <id> --success|--failure
- universe store-plan --title "..." --step "1. ..." --step "2. ..."
- universe store-plan-result --plan-id <id> --success [--summary ...]
- universe verify-plan --plan-id <id> --approve|--reject [--note ...]

## DO NOT

- Do NOT read .universe/graph.json directly. It is large and structural;
  the hook gives you what you need.
- Do NOT read entire files when the hook gave you a line range.
- Do NOT call a Universe MCP tool — there is no MCP server anymore.
`

// universeHookCommand returns the command string for .cursor/hooks.json.
// We resolve the absolute path to the currently-running universe binary
// (the one the user just invoked `universe init` with) and bake it into
// the hook command. Doing this avoids two bugs we've seen in practice:
//
//   1. Cursor launches hook commands with a clean PATH that doesn't
//      always include the npm global bin directory on Windows — the
//      hook just silently failed to fire.
//   2. Different machines have universe at different paths (npm global
//      vs. a local go build vs. an absolute install). Hardcoding any
//      one of those breaks the others.
//
// Resolving the path at init time pins it to whatever was on the
// developer's PATH when they ran `universe init`, which is the version
// they want their agent to use anyway.
//
// JSON requires backslashes to be escaped — strconv.Quote handles both
// that and any spaces in the path in one go.
func universeHookCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// Fall back to bare name — works wherever PATH is set, which is
		// most CI environments and most Mac/Linux dev boxes.
		exe = "universe"
	}
	// Wrap in quotes so paths containing spaces don't tokenize wrong
	// when Cursor forks the command.
	quoted := strconv.Quote(exe)
	return quoted + " hook-check \"$TOOL_NAME\" \"$TOOL_INPUT\""
}

// renderHooksBody returns the .cursor/hooks.json contents in Cursor's
// documented schema: https://cursor.com/docs/hooks.md
//
//   { "version": 1,
//     "hooks": {
//       "preToolUse": [
//         { "command": "./binary args", "matcher": "Read|Grep|..." }
//       ]
//     } }
//
// Versions before v0.3.3 shipped a schema invented from Claude Code's
// hook docs (PascalCase event key, matcher-as-object, command nested
// under "hook"); Cursor silently rejected the whole file. That is the
// reason the hook never fired in chat despite the binary working
// perfectly when invoked by hand.
func renderHooksBody() string {
	cmd := universeHookCommand()
	// Matcher is a regex Cursor evaluates against the tool name; we
	// list every Read/Grep-style tool we want to intercept.
	matcher := "Read|ReadFile|Grep|Search|RipGrep|Glob|ListFiles"
	return `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": ` + jsonString(cmd) + `,
        "matcher": ` + jsonString(matcher) + `
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
	if existing, err := os.ReadFile(path); err == nil && string(existing) == cursorRuleBody {
		return false, path, nil
	}
	if err := os.WriteFile(path, []byte(cursorRuleBody), 0o644); err != nil {
		return false, path, err
	}
	return true, path, nil
}
