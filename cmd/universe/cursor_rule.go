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
description: Universe knowledge-graph access — prefer MCP tools, do not read graph.json
alwaysApply: true
---

# Using the Universe knowledge graph

This project has a precomputed call graph at .universe/graph.json. The
Universe MCP server exposes it through compact tools — use those, not the
raw file.

## Rules

1. For "what does X do / who uses X" questions, call the get_context MCP
   tool first. It returns callers, callees, flows, cluster, and impact in
   ONE call. Do not chain get_dependencies + get_impact_analysis afterwards
   unless get_context says it is incomplete.

2. For "find a function/type by name" use search_graph, then get_context on
   the chosen result.

3. For "what breaks if I change X" use get_impact_analysis. It returns a
   precomputed blast radius grouped by depth.

4. Do NOT read .universe/graph.json directly with cat / Get-Content /
   Read / fs.readFileSync. It is structural data only — source is no
   longer embedded — and reading it costs more tokens than any answer it
   could give. When you need source, open the actual source file at the
   file_path returned by the MCP tools.

5. MCP tool responses give you "Name [kind] file:line" refs. Treat those
   as authoritative locations. Open the source file with Read if and only
   if the line range is not enough to answer the question.
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
