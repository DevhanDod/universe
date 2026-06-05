# Universe v0.3.0 — Phase 1: Zero-MCP Hook Architecture

## Plan for Review

---

## Goal

Remove ALL MCP tools. Move knowledge graph delivery to PreToolUse hooks
(like Graphify). Keep GitNexus-style confidence scoring for filtering.
Solve the 3 identified issues that cause 154K tokens instead of 94K.

---

## The 3 Issues We're Solving

### Issue 1: MCP tool schema bloat (+15-20K per turn)

6 MCP tools inject ~15-20K tokens of JSON schema into every turn. The
agent only ever calls `universe_context`. The other 5 are dead weight.

**Solution:** Remove ALL 6 MCP tools. Zero schema cost. The graph
delivers its data through PreToolUse hooks that inject directly into
the agent's context — like Graphify does.

### Issue 2: Agent reads MCP tool schema file (+1-2K per turn)

Before calling `universe_context`, the agent reads
`mcps/.../tools/universe_context.json` to discover the parameters.
This is a wasted file read.

**Solution:** No MCP tools = nothing to discover. The hook fires
automatically. The agent never needs to "learn" how to use Universe.

### Issue 3: Agent reads source files after graph already answered (+3-15K)

The graph tells the agent everything it needs, but the agent still
reads files to "confirm." Each file read risks triggering the repo's
convention rules (which in the API test repo adds 10-15K tokens).

**Solution:** The hook injects the graph answer INSTEAD of the file
read, not alongside it. When the hook provides a complete answer, it
tells the agent "Graph answered your question. No file read needed."
The agent sees the answer already in its context and skips the read.
This is exactly how Graphify's hooks work — they intercept and replace,
not intercept and suggest.

---

## Architecture Change

### Before (v0.2.8 — current)

```
Agent asks question
  → Agent decides to call universe_context MCP tool
  → MCP tool returns graph data (~200 tokens)
  → Agent decides to read source files anyway (~3K + 10-15K convention rules)
  → Total overhead: ~15-20K schemas + 1-2K discovery + 3-15K file reads

Cost added by Universe: ~60K tokens
```

### After (v0.3.0 — this patch)

```
Agent asks question
  → Agent tries to Grep/Read a file
  → PreToolUse hook fires BEFORE the read happens
  → Hook loads graph, finds matching node
  → Hook injects graph answer directly into agent context (~100-300 tokens)
  → Hook tells agent "graph answered this, skip the file read"
  → Agent sees the answer, skips the file read
  → No convention rules triggered (no file was read)

Cost added by Universe: ~2K (universe.mdc rule) + ~200 (hook injection)
```

### How Graphify does it (our reference)

Graphify registers PreToolUse hooks that intercept Read/Grep/Search.
When the hook fires, it:
1. Extracts the symbol or file path from the tool input
2. Queries the graph
3. If the graph has data: prints the answer directly (the agent sees it)
4. The agent decides whether it still needs the file read

The key difference from our current hook: Graphify's hook provides the
FULL ANSWER, not just a suggestion to "try universe_context instead."

### How GitNexus confidence scoring fits in

When the hook queries the graph, it uses confidence-filtered traversal:
- High confidence (≥0.8): direct AST-extracted relationships → include
- Medium confidence (0.5-0.8): inferred cross-package → include but mark
- Low confidence (<0.5): skip entirely

This means the hook response is precise and trustworthy. The agent sees
confidence levels and can decide: "all relationships are ≥90% confidence,
I trust this answer" vs "some are 50%, I should verify by reading."

---

## What Changes

### Files to DELETE

```
internal/mcpserver/server.go           — MCP server entry point (entire file)
internal/mcpserver/tools_unified.go    — HandleContextV28
internal/mcpserver/tools_read.go       — HandleQueryV28, HandleImpactV28, etc.
internal/mcpserver/tools_graph.go      — old handlers (already dead code)
internal/mcpserver/tools_memory.go     — old handlers (already dead code)
internal/mcpserver/tools_skills.go     — old handlers (already dead code)
internal/mcpserver/tools_plans.go      — old handlers (already dead code)
internal/mcpserver/tools_orchestrator.go — old handlers (already dead code)
internal/mcpserver/context.go          — session context (MCP-specific)
internal/mcpserver/mcpserver_test.go   — tests for deleted handlers
```

### Files to KEEP (move out of mcpserver/)

```
internal/mcpserver/response.go         — KEEP the formatting logic
                                         Move to internal/formatter/response.go
                                         This is pure formatting, not MCP-specific.
                                         The hook will use these same functions.
```

### Files to MODIFY

```
cmd/universe/hook_cmd.go              — MAJOR rewrite (core of this patch)
cmd/universe/cursor_rule.go           — Rewrite universe.mdc for hook-only mode
cmd/universe/mcp_cmd.go               — Remove or stub (no MCP server to start)
cmd/universe/main.go                  — Remove MCP command registration
cmd/universe/init_cmd.go              — Update hooks.json generation
```

### Files to CREATE

```
internal/hookengine/engine.go         — Shared hook logic: load graph, query, format
internal/hookengine/engine_test.go    — Tests for hook engine
internal/formatter/response.go        — Moved from mcpserver/response.go
```

### Files UNCHANGED

```
internal/graph/graph.go               — All query methods stay as-is
internal/models/models.go             — Node/Edge/Confidence stays as-is
internal/analyzer/                    — All analysis stays as-is
internal/compress/                    — All compression stays as-is
internal/memory/                      — Stays as-is (used by hooks later in Phase 2)
internal/skills/                      — Stays as-is (used by hooks later in Phase 2)
internal/orchestrator/                — Stays as-is
internal/parser/                      — Stays as-is
cmd/universe/intel.go                 — Shell commands stay as-is (fallback)
cmd/universe/store_cmds.go            — Shell write commands stay as-is
```

---

## Detailed Changes

### 1. `cmd/universe/hook_cmd.go` — MAJOR REWRITE

This is the core of the patch. The current hook does this:

```
"[Universe] Graph has data for 'ErrorResponse'. 
 Consider universe_context(name: "ErrorResponse") instead of Read."
```

That's a SUGGESTION. The agent ignores it and reads the file anyway.

The new hook does this:

```
"[Universe] ErrorResponse [struct] test-server.go:58 (test_server)
 Callers (1): sendError test-server.go:563
 Callees (0)
 Flows: handleRequest
 Impact: low — 1 affected node(s)
 Confidence: all relationships ≥90%
 
 Graph has complete context for this symbol. File read is likely unnecessary."
```

That's the FULL ANSWER. The agent sees callers, callees, flows, impact,
confidence — everything it would have gotten from `universe_context`.
It doesn't need to read the file.

#### Implementation

```go
// hook_cmd.go — the new version

// runHookCheck is called by Cursor's PreToolUse hook before every
// Read, Grep, Search, RipGrep, Glob, and ListFiles call.
//
// BEFORE (v0.2.8): printed a one-line suggestion to use universe_context.
// AFTER  (v0.3.0): prints the full graph answer so the agent doesn't
//                   need to call any tool or read any file.
//
// The response format matches what FormatNodeResponse() produces —
// the agent already learned this format from universe_query output,
// so it reads it fluently.

func runHookCheck(_ *cobra.Command, args []string) error {
    toolName := args[0]
    toolInput := args[1]

    g, err := loadGraph(filepath.Join(LocalDataDir(), "graph.json"))
    if err != nil {
        return nil // silent — no graph yet
    }

    symbol := extractSymbolFromToolInput(toolName, toolInput)
    if symbol == "" {
        return nil // can't determine target, let the tool proceed
    }

    nodes := g.SearchNodes(symbol, 3)
    if len(nodes) == 0 {
        return nil // graph doesn't know this symbol, let the tool proceed
    }

    // Build the full response using the same formatter MCP tools used.
    // This is the key change: we give the ANSWER, not a suggestion.
    first := nodes[0]
    cfg := formatter.DefaultResponseConfig()
    cfg.IncludeConf = true  // show confidence so agent can judge trust

    response := formatter.FormatNodeResponse(first, g, cfg)

    // Print confidence assessment
    callers := g.GetDependentsFiltered(first.ID, 0.0, 1)
    minConf := 1.0
    for _, c := range callers {
        if c.Confidence < minConf {
            minConf = c.Confidence
        }
    }

    fmt.Printf("[Universe] %s", response)

    if minConf >= 0.8 {
        fmt.Printf("Confidence: all relationships ≥%.0f%%\n", minConf*100)
        fmt.Printf("Graph has complete context. File read is likely unnecessary.\n")
    } else if minConf >= 0.5 {
        fmt.Printf("Confidence: some relationships are %.0f%% (inferred).\n", minConf*100)
        fmt.Printf("Graph has partial context. Targeted file read may help verify.\n")
    }
    // If minConf < 0.5 or no callers: print nothing extra, let the read proceed.

    return nil
}
```

#### Confidence-based guidance (from GitNexus)

The hook uses confidence scores to tell the agent HOW MUCH to trust
the graph answer:

| Min confidence of relationships | What the hook says | Agent behavior |
|---|---|---|
| ≥ 0.8 (high) | "Graph has complete context. File read is likely unnecessary." | Agent skips the file read |
| 0.5–0.8 (medium) | "Graph has partial context. Targeted file read may help verify." | Agent reads ONLY the specific lines |
| < 0.5 (low) | (nothing extra) | Agent reads the file normally |

This is the GitNexus approach: confidence scoring determines whether
the agent trusts the graph or falls back to file reads. High-confidence
answers prevent file reads. Low-confidence answers let them through.

### 2. `internal/hookengine/engine.go` — NEW

Shared logic so both `hook_cmd.go` and shell commands (intel.go)
use the same query + format pipeline.

```go
package hookengine

import (
    "github.com/Universe/universe/internal/formatter"
    "github.com/Universe/universe/internal/graph"
)

// QueryResult holds everything the hook or shell command needs to
// render a response.
type QueryResult struct {
    Response      string  // formatted text (FormatNodeResponse output)
    MinConfidence float64 // lowest confidence among direct relationships
    NodeFound     bool    // whether the graph had data for the query
    NodeName      string  // canonical name from the graph
    FilePath      string  // where the symbol is defined
    StartLine     int     // start line of definition
    EndLine       int     // end line of definition
}

// Query loads the graph and returns a formatted result for a symbol.
// Used by hook_cmd.go and intel.go.
func Query(g *graph.Graph, symbol string) QueryResult {
    nodes := g.SearchNodes(symbol, 3)
    if len(nodes) == 0 {
        return QueryResult{NodeFound: false}
    }

    first := nodes[0]
    cfg := formatter.DefaultResponseConfig()
    cfg.IncludeConf = true

    response := formatter.FormatNodeResponse(first, g, cfg)

    // Calculate minimum confidence across direct relationships
    callers := g.GetDependentsFiltered(first.ID, 0.0, 1)
    callees := g.GetDependenciesFiltered(first.ID, 0.0, 1)
    minConf := 1.0
    for _, c := range callers {
        if c.Confidence < minConf {
            minConf = c.Confidence
        }
    }
    for _, c := range callees {
        if c.Confidence < minConf {
            minConf = c.Confidence
        }
    }

    return QueryResult{
        Response:      response,
        MinConfidence: minConf,
        NodeFound:     true,
        NodeName:      first.Name,
        FilePath:      first.FilePath,
        StartLine:     first.StartLine,
        EndLine:       first.EndLine,
    }
}
```

### 3. `internal/formatter/response.go` — MOVED from mcpserver/

Move `internal/mcpserver/response.go` to `internal/formatter/response.go`.
Change the package declaration from `mcpserver` to `formatter`.
Update all imports. No logic changes — same functions, same format.

Functions moved:
- `FormatNodeRef()`
- `FormatNodeResponse()`
- `FormatNodeList()`
- `FormatImpactResponse()`
- `writeRefList()`
- `truncateToBudget()`
- `ResponseConfig` struct
- `DefaultResponseConfig()`

### 4. `cmd/universe/cursor_rule.go` — REWRITE

The new rule must:
- NOT mention any MCP tools (they don't exist anymore)
- Tell the agent that the PreToolUse hook provides graph answers
- Tell the agent to trust high-confidence hook responses
- Tell the agent to read ONLY specific lines when needed

```go
const cursorRuleBody = `---
description: "Universe knowledge graph — graph-first via hooks"
alwaysApply: true
---

# Universe — Knowledge Graph (Hook-Powered)

Universe provides a knowledge graph of this codebase via automatic hooks.
You do NOT need to call any tool to access it. It works like this:

## How it works

When you try to Read, Grep, or Search, Universe's PreToolUse hook checks
the graph BEFORE your tool runs. If the graph has data for the symbol
you're looking for, it prints the answer directly in your context:

  [Universe] ErrorResponse [struct] test-server.go:58 (test_server)
  Callers (1): sendError test-server.go:563
  Flows: handleRequest
  Impact: low — 1 affected node
  Confidence: all relationships ≥90%
  Graph has complete context. File read is likely unnecessary.

## What to do when you see a hook response

- "File read is likely unnecessary" → SKIP the file read. The graph answered it.
- "Targeted file read may help verify" → Read ONLY the specific lines shown
  (e.g. test-server.go:58-66, NOT the whole file).
- No hook output → Graph doesn't know this symbol. Proceed normally.

## Shell commands (for explicit queries)

- universe query <name> — same data as the hook, on demand
- universe impact <name> — blast radius analysis
- universe search <term> — find symbols by name/path
- universe deps <name> — dependency list

## DO NOT
- Read .universe/graph.json directly (it's a large binary-like file).
- Read entire files when the hook or graph gives specific line ranges.
`
```

Key differences from current rule:
- No mention of MCP tools or universe_context
- Explains that hooks work AUTOMATICALLY
- Tells the agent what to do with hook output
- Keeps shell commands as explicit fallback
- Total: ~800 tokens (down from ~2K)

### 5. `cmd/universe/mcp_cmd.go` — REMOVE MCP SERVER

The `universe mcp` command currently starts the MCP server over stdio.
With no MCP tools, this command has no purpose.

Options (choose one):
- **Option A (recommended):** Keep the command but make it print a
  deprecation message: "MCP server removed in v0.3.0. Universe now
  works through PreToolUse hooks. Run 'universe init' to set up hooks."
- **Option B:** Delete the command entirely and remove from main.go.

Option A is safer because users who have `universe mcp` in their
Cursor MCP config will get a clear error message instead of a crash.

### 6. `cmd/universe/init_cmd.go` — UPDATE HOOKS

The current `init` generates `.cursor/hooks.json`. The hook config
stays the same structure but the behavior changes because `hook_cmd.go`
now provides full answers.

No changes needed to the hooks.json template — it already intercepts
the right tools (Read, ReadFile, Grep, Search, RipGrep, Glob, ListFiles)
and calls `universe hook-check`.

One addition: remove the MCP config generation. If `init` currently
writes to `.cursor/mcp.json` or similar, stop writing it. The agent
should not see Universe as an MCP server anymore.

### 7. `cmd/universe/main.go` — CLEAN UP

Remove:
- MCP command registration (if mcp_cmd.go is deleted)
- Any MCP-related imports

Keep:
- All shell commands (query, deps, impact, search, recall, etc.)
- Hook command
- Init command
- Store commands
- Analyze command

---

## What This Does NOT Change (Phase 2 / Later)

- **Memory (Engine 2):** Still stored in PostgreSQL, still queryable via
  `universe recall` shell command. Phase 2 will add a SessionStart hook
  that auto-injects relevant memories (like claude-mem does).

- **Skills (Engine 3):** Still stored and queryable via
  `universe skill find` shell command. Phase 2 will add auto-injection.

- **Compression (Engine 4):** Still a library. Phase 2 will wire it into
  the hook response formatter.

- **Orchestrator (Engine 5):** Still uses shell commands for plans. No
  change.

- **Graph storage:** Still `graph.json`. No database migration.

- **Graph analysis:** Clusters, flows, impact, confidence — all stay
  as-is. The hook just reads what's already computed.

---

## Token Budget After This Patch

For the test question: "What does ErrorResponse do?"

| Component | Before (154K) | After (v0.3.0) | Savings |
|-----------|---------------|-----------------|---------|
| Cursor system prompt + built-in tools | ~45K | ~45K | 0 |
| MCP tool schemas (6 tools) | ~15-20K | **0** | **-15-20K** |
| Agent reads tool schema JSON | ~1-2K | **0** | **-1-2K** |
| universe.mdc (always-apply) | ~2K | ~0.8K | **-1.2K** |
| 00-rules-overview.mdc | ~10K | ~10K | 0 |
| Skills + MCP descriptions | ~8K | **~3K** | **-5K** |
| Environment context | ~5K | ~5K | 0 |
| Hook injection (graph answer) | 0 | ~0.2K | +0.2K |
| universe_context MCP call | ~0.2K | **0** | -0.2K |
| File read (307 lines) | ~3K | **0** | **-3K** |
| Convention rules (auto-attached) | ~12K | **0** | **-12K** |
| Agent synthesis | ~0.5K | ~0.5K | 0 |
| **TOTAL** | **~154K** | **~65K** | **~89K saved** |

The convention rules saving (~12K) happens because the agent doesn't
read the file at all — the hook answered the question. No file read =
no glob match = no convention rules injected.

**65K is BELOW the 94K without-Universe baseline.** Universe now saves
tokens instead of adding them.

For the second question in the same session, the savings are even bigger:
- Without Universe: another ~15K for grep + file read + convention rules
- With Universe: ~200 tokens for the hook injection
- Cumulative savings grow with every question

---

## Implementation Order

1. Create `internal/formatter/` — move `response.go` from mcpserver
2. Create `internal/hookengine/engine.go` — shared query logic
3. Rewrite `cmd/universe/hook_cmd.go` — full-answer hook
4. Rewrite `cmd/universe/cursor_rule.go` — hook-only rule
5. Stub/deprecate `cmd/universe/mcp_cmd.go` — no more MCP server
6. Update `cmd/universe/init_cmd.go` — stop generating MCP config
7. Update `cmd/universe/main.go` — remove MCP registration
8. Delete all files in `internal/mcpserver/` (except response.go which was moved)
9. Update `go.mod` — remove MCP SDK dependency if no longer needed
10. Run `go build ./...` and `go test ./...`

---

## Testing

### Test 1: Hook provides full answer

```bash
# Simulate the hook being called before a Read:
universe hook-check "Read" '{"file_path": "internal/test-server/test-server.go"}'

# Expected output:
# [Universe] ErrorResponse [struct] test-server.go:58 (test_server)
# Callers (1): sendError [method] test-server.go:563 (test_server)
# ...
# Confidence: all relationships ≥90%
# Graph has complete context. File read is likely unnecessary.
```

### Test 2: Hook is silent when graph has no data

```bash
universe hook-check "Read" '{"file_path": "some-unknown-file.go"}'
# Expected: no output (silent, tool proceeds normally)
```

### Test 3: Hook shows confidence for uncertain relationships

```bash
universe hook-check "Grep" '{"pattern": "SomeExternalFunc"}'
# If the graph has this with low confidence:
# Expected output includes "some relationships are 60% (inferred)"
# and "Targeted file read may help verify"
```

### Test 4: No MCP server starts

```bash
universe mcp --repo .
# Expected: "MCP server removed in v0.3.0. Universe now works through
# PreToolUse hooks. Run 'universe init' to set up hooks."
```

### Test 5: Shell commands still work

```bash
universe query ErrorResponse
universe impact ErrorResponse
universe search "error"
universe deps ErrorResponse
# All should produce the same output as before
```

### Test 6: THE TOKEN TEST

```bash
# Fresh Cursor session with Universe v0.3.0 hooks installed:
# Ask: "What does ErrorResponse do?"
# Note total token count → TARGET: under 70K

# Compare to:
# - v0.2.8 with MCP: 154K
# - Without Universe: 94K
# v0.3.0 MUST be below 94K
```

---

## Acceptance Criteria

- [ ] Zero MCP tools registered (no MCP server, no tool schemas)
- [ ] `universe mcp` prints deprecation message (doesn't crash)
- [ ] PreToolUse hook prints full graph answer (not just a suggestion)
- [ ] Hook response includes callers, callees, flows, impact
- [ ] Hook response includes confidence assessment
- [ ] Hook says "file read is likely unnecessary" when confidence ≥ 80%
- [ ] Hook says "targeted file read may help verify" when confidence 50-80%
- [ ] Hook is silent when graph has no data for the symbol
- [ ] Hook completes in under 500ms
- [ ] Shell commands (query, impact, search, deps) still work unchanged
- [ ] `universe.mdc` does not mention any MCP tools
- [ ] `universe.mdc` explains how hooks work
- [ ] `universe.mdc` is under 1K tokens
- [ ] All old mcpserver/ files deleted (except response.go which moved)
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] Token count for simple question is under 70K
- [ ] Token count is BELOW the without-Universe baseline (94K)

---

## Risk & Mitigation

**Risk:** Agent ignores hook output and reads the file anyway.
**Mitigation:** The cursor rule explicitly says "when you see 'file read
is likely unnecessary', SKIP the file read." The hook output is designed
to look like a complete answer, not a suggestion. Testing will verify.

**Risk:** Hook is too slow (>500ms) and Cursor kills it.
**Mitigation:** The hook loads a JSON file and does an in-memory search.
Current hook already does this in ~50ms. Adding FormatNodeResponse adds
negligible time (it's string formatting, not I/O).

**Risk:** Removing MCP breaks users who have `universe mcp` in their config.
**Mitigation:** Option A — keep the command, print deprecation message.
The agent sees no tools (good) and the user gets a clear next step.

**Risk:** Some questions need the file source and the hook can't provide it.
**Mitigation:** Low-confidence hook responses don't block file reads.
When the hook says nothing (no graph data) or says "targeted file read
may help verify," the agent proceeds normally. We only block reads when
confidence is high and the graph has complete context.
