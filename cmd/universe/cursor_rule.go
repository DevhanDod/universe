package main

import (
	"os"
	"path/filepath"
)

// cursorRuleBody is the steering rule we drop into the user's project so
// Cursor (and any other MCP-aware agent that reads .cursor/rules) prefers
// our compact MCP tools over slurping .universe/graph.json raw. The latter
// was happening in practice and was the dominant cause of token blow-ups.
const cursorRuleBody = `---
description: "Universe knowledge graph — graph-first code intelligence"
globs: ["**/*"]
alwaysApply: true
---

# Universe — Graph-First Code Intelligence

This project has a precomputed knowledge graph (callers, callees, flows,
clusters, impact) exposed via Universe MCP tools, plus a summary at
.universe/UNIVERSE_REPORT.md.

## CRITICAL: Query the graph FIRST

For ANY question about code structure, callers, callees, impact, or
"what does X do":

1. Call universe_context first with the symbol or question.
2. The response covers graph + memory + skills in one shot.
3. Only Read source files if:
   - universe_context returns "No node found" for the symbol, or
   - You need the exact source body of a function the graph located, or
   - The question is about literal file contents (comments, formatting).
4. When you do Read a file, read ONLY the line range the graph pointed at,
   not the whole file.

## DO NOT

- Do NOT Grep / Read across the codebase before calling universe_context.
- Do NOT chain universe_query + universe_impact when universe_context
  already includes both.
- Do NOT read .universe/graph.json directly — it's structural-only and
  reading it costs more than any answer it could give.
- Do NOT skip Universe because "the question feels local" — call it
  anyway; the graph response is small and tells you whether the rest of
  the codebase is involved.

## MCP read tools (call these)

- universe_context(name, [question, max_depth, min_confidence])
  PRIMARY tool. Graph + memory + skills, one response.
- universe_query(name)
  Quick lookup: just the graph slice for one symbol.
- universe_impact(name, [direction, max_depth, min_confidence])
  Blast radius. direction = "upstream" (default) or "downstream".
- universe_search(query, [limit])
  Find symbols by name / file path / package.
- universe_recall(query, [node, limit])
  Past observations from memory.
- universe_skill_find(query)
  Match a skill recipe (always REQUIRES VERIFICATION before use).

## Shell write commands (NOT MCP tools — run via Bash)

- universe store-observation --category <fix|pattern|decision> --summary "..." [--node ID]
- universe report-skill --skill-id <id> --success|--failure [--output ...] [--error ...]
- universe store-plan --title "..." --step "1. ..." --step "2. ..."
- universe store-plan-result --plan-id <id> --success [--summary ...]
- universe verify-plan --plan-id <id> --approve|--reject [--note ...]

## When to skip Universe entirely

- Editing a file you already have open and the question is purely about
  that file's literal text.
- Creating brand-new files (graph has nothing yet).
- Shell / build / test invocations.
`

// cursorHooksBody is the .cursor/hooks.json contents written by
// `universe init` when no hooks file exists yet. The hook calls our
// hidden `universe hook-check` command before Read/Grep/Search-style
// tools so the agent gets a nudge toward universe_context when the
// graph already knows the symbol.
const cursorHooksBody = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": {
          "tool_names": ["Read", "ReadFile", "Grep", "Search", "RipGrep", "Glob", "ListFiles"]
        },
        "hook": {
          "type": "command",
          "command": "universe hook-check \"$TOOL_NAME\" \"$TOOL_INPUT\"",
          "timeout_ms": 500,
          "on_failure": "ignore"
        }
      }
    ]
  }
}
`

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
	if err := os.WriteFile(path, []byte(cursorHooksBody), 0o644); err != nil {
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
