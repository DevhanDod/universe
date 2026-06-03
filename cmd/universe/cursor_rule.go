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
description: "Universe code intelligence — use shell commands, do not read graph.json"
globs: ["**/*"]
alwaysApply: true
---

# Universe Code Intelligence

This project has a precomputed call graph at .universe/graph.json plus a
summary at .universe/UNIVERSE_REPORT.md. Read the report for the broad
picture; use the shell commands below for specific questions.

## For codebase overview

Read .universe/UNIVERSE_REPORT.md. It lists clusters, high-impact nodes,
and key execution flows in 3-8KB.

## For specific symbols (terminal commands, NOT MCP tools)

    universe query <name>       360° context: callers, callees, flows, impact
    universe deps <name>        callers + callees only
    universe impact <name>      blast radius for planned changes
    universe search <term>      find symbols by name
    universe recall <query>     past session memory
    universe skills find <q>    matching skill recipes (verify before using)
    universe plans get          latest pending plan
    universe plans result [id]  plan execution result
    universe cost               cost savings summary

Output is compact text, capped at ~2KB per call. Run them with the Bash
tool — they print to stdout, no MCP roundtrip needed.

## When to use vs when to skip

USE Universe commands when:
- "What depends on X?" → universe query X
- "What breaks if I change X?" → universe impact X
- "How does the auth flow work?" → read UNIVERSE_REPORT.md, then universe query <handler>
- Cross-file or cross-package questions

SKIP Universe commands when:
- The file is already open and the question is local — just read the file
- Single function fits in one file → open it directly

## Do not

- Do NOT read .universe/graph.json directly (cat / Get-Content / Read).
  It is structural data only, no source — and reading it wastes tokens.
- Do NOT call get_context / get_dependencies / get_impact_analysis /
  search_graph as MCP tools — those have been moved to shell commands.

## MCP tools that DO still exist (write operations)

These remain MCP because the agent sends structured input to PostgreSQL:
    store_observation       save a fix / pattern / decision to memory
    report_skill_execution  report skill success / failure
    store_plan              save a plan for the executor agent
    store_plan_result       report execution result
    verify_plan             approve or reject a plan result
`

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
