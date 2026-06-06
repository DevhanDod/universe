# Universe v0.4.0 — Option B: Rule + CLI (Graphify Model)

## Build Specification for Claude Code

**Version:** 0.4.0  
**Approach:** Remove all MCP tools and broken hooks. Use ONLY a lean `.mdc` rule + shell CLI commands, exactly like Graphify does for Cursor.  
**Reference:** Graphify (58K stars) uses `.cursor/rules/graphify.mdc` with `alwaysApply: true` and `graphify query` via Shell. No hooks. No MCP. It works.

---

## Background: Why This Approach

We tested every other approach and they all failed in Cursor:

| Approach | Result | Why it failed |
|----------|--------|---------------|
| MCP tools (v0.2.7–v0.2.8) | +60K tokens overhead | 6 tool schemas injected every turn |
| PreToolUse hook stdout (v0.3.0) | Agent never saw output | Cursor doesn't inject hook stdout |
| PreToolUse deny + agent_message (v0.3.1) | Agent never saw output | Cursor doesn't deliver agent_message for non-MCP tools |
| postToolUse additional_context (v0.3.2) | Cursor bins it | Confirmed bug, ticket T-C20310 |
| **Rule + CLI (this patch)** | **Works** | Graphify proves it at scale |

The rule tells the agent to run `universe query` via Shell. Shell tool output is injected into the agent's context as a normal tool result. This path has always worked in Cursor and always will — it's a core tool, not a hook feature.

---

## What We're Building

### The entire Universe-in-Cursor integration becomes two things:

**1. `.cursor/rules/universe.mdc`** (~400 tokens, alwaysApply: true)

Tells the agent: "before reading files to understand code structure, run `universe query <symbol>` via the terminal. Trust the graph output. Only read files if the graph has no data or you need exact source code."

**2. Shell commands** (already exist, unchanged)

`universe query`, `universe search`, `universe impact`, `universe deps` — these already work from the terminal. The agent calls them through Cursor's built-in Shell tool. Output goes directly into the agent's context.

That's it. Nothing else.

---

## What to Remove

### Remove all MCP infrastructure

Delete the entire `internal/mcpserver/` directory:

```
DELETE: internal/mcpserver/server.go
DELETE: internal/mcpserver/tools_unified.go
DELETE: internal/mcpserver/tools_read.go
DELETE: internal/mcpserver/tools_graph.go
DELETE: internal/mcpserver/tools_memory.go
DELETE: internal/mcpserver/tools_skills.go
DELETE: internal/mcpserver/tools_plans.go
DELETE: internal/mcpserver/tools_orchestrator.go
DELETE: internal/mcpserver/context.go
DELETE: internal/mcpserver/mcpserver_test.go
```

Keep `internal/mcpserver/response.go` — move it to `internal/formatter/response.go`. The shell commands in `intel.go` use the same formatting functions.

### Remove the MCP command

In `cmd/universe/mcp_cmd.go`: replace the MCP server startup with a message:

```go
fmt.Println("MCP server removed in v0.4.0.")
fmt.Println("Universe now works through .cursor/rules/universe.mdc + shell commands.")
fmt.Println("Run 'universe init' to set up, then use 'universe query <symbol>' in the terminal.")
```

### Remove hooks infrastructure

The hooks don't work in Cursor (confirmed). Remove them:

```
DELETE: cmd/universe/hook_cmd.go
```

In `cmd/universe/init_cmd.go`: stop generating `.cursor/hooks.json`. The hooks.json file is no longer needed.

In `cmd/universe/main.go`: remove `hookCheckCmd()` registration.

### Remove hooks.json generation from init

In `cmd/universe/init_cmd.go`, remove the `generateHooksFile()` function and its call. `universe init` should only generate:
- `.universe/graph.json` (the graph)
- `.universe/UNIVERSE_REPORT.md` (the report)
- `.cursor/rules/universe.mdc` (the rule)

It should NOT generate `.cursor/hooks.json` anymore.

---

## The New `universe.mdc` Rule

This is the most important part of the patch. The rule must:
1. Be short (under 500 tokens — every token is paid on every turn)
2. Tell the agent exactly when and how to use Universe
3. Convince the agent that `universe query` is faster than Grep + Read
4. Not make claims about hooks or MCP (they don't exist anymore)

### Modify `cmd/universe/cursor_rule.go`

Replace the entire `cursorRuleBody` constant:

```go
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

This returns: definition location, type, cluster, callers, callees,
execution flows, and impact — in ~200 tokens. Faster and cheaper than
grepping the codebase and reading multiple files.

## Commands

  universe query <name>     — definition + callers + callees + flows
  universe search <term>    — find symbols by name/path/package
  universe impact <name>    — what breaks if you change this
  universe deps <name>      — dependency list

## When NOT to use

- Creating or editing files (just edit them directly)
- Running tests, builds, or deployments
- Questions unrelated to code structure
- If universe query returns "not found", fall back to Grep + Read

## Important

- Trust definition, location, type, and callees from the graph.
- Caller counts may be incomplete (graph covers ~28%% of files).
  If you need exact caller lists, verify with Grep.
- If you need the actual source code of a function (not just its
  relationships), read ONLY the specific lines the graph shows
  (e.g. file.go:36-50), not the entire file.
`
```

Key design decisions:
- No mention of hooks, MCP, or any mechanism. Just "run this command."
- Explicit about what to trust and what not to trust (caller counts are unreliable per §3.5 of hookPatch.md).
- Uses `%%` to escape the percent sign in Go string literals.
- The "faster and cheaper" framing gives the agent a reason to prefer the graph over grepping — this is what makes it actually obey the rule.
- Under 400 tokens total.

---

## sessionStart Hook (OPTIONAL — the one hook that works)

The Cursor `sessionStart` hook DOES deliver `additional_context` to the model. We can use this to inject a small project digest at the start of each session. This is optional but provides a nice baseline context.

### Add `cmd/universe/session_digest_cmd.go` (NEW)

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "github.com/spf13/cobra"
)

// sessionDigestResponse matches Cursor's sessionStart output schema.
type sessionDigestResponse struct {
    AdditionalContext string `json:"additional_context,omitempty"`
}

// sessionDigestCmd creates the `universe session-digest` command.
// Called by Cursor's sessionStart hook at the beginning of each session.
//
// Reads stdin (Cursor sends session info as JSON), ignores it.
// Loads the graph, builds a compact digest, returns it as
// additional_context in the sessionStart response format.
//
// The digest is intentionally small (~200-300 tokens) because it's
// resident for the entire session.
func sessionDigestCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:    "session-digest",
        Short:  "Emit a compact project digest for Cursor sessionStart hook",
        Hidden: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            // Consume stdin (Cursor sends JSON, we don't need it)
            io.Copy(io.Discard, os.Stdin)

            // Find and load the graph
            graphPath := filepath.Join(".universe", "graph.json")
            if _, err := os.Stat(graphPath); os.IsNotExist(err) {
                // No graph — emit empty response, don't error
                json.NewEncoder(os.Stdout).Encode(sessionDigestResponse{})
                return nil
            }

            g, err := loadGraphFromFile(graphPath)
            if err != nil {
                json.NewEncoder(os.Stdout).Encode(sessionDigestResponse{})
                return nil
            }

            // Build compact digest
            digest := buildSessionDigest(g)

            resp := sessionDigestResponse{
                AdditionalContext: digest,
            }
            return json.NewEncoder(os.Stdout).Encode(resp)
        },
    }
    return cmd
}

// buildSessionDigest creates a compact summary of the codebase.
// Target: ~200-300 tokens. This is resident for the entire session.
//
// Format:
//   [Universe] 559 symbols, 76 files, 12 packages
//   Key clusters: auth (45 symbols), api (38), db (22), config (15)
//   Entry points: main, TestRunHealthCheckSuite, TestRunScenarioSuite
//   Run "universe query <name>" for details on any symbol.
//
func buildSessionDigest(g *graph.Graph) string {
    var b strings.Builder

    // Count basics
    nodeCount := len(g.Nodes)
    fileCount := countUniqueFiles(g)
    pkgCount := countUniquePackages(g)

    fmt.Fprintf(&b, "[Universe] %d symbols, %d files, %d packages\n", nodeCount, fileCount, pkgCount)

    // Top clusters by size (max 5)
    clusters := getTopClusters(g, 5)
    if len(clusters) > 0 {
        parts := make([]string, len(clusters))
        for i, c := range clusters {
            parts[i] = fmt.Sprintf("%s (%d)", c.Name, c.Count)
        }
        fmt.Fprintf(&b, "Key clusters: %s\n", strings.Join(parts, ", "))
    }

    // Entry points (functions with 0 callers and high callee count)
    entries := getEntryPoints(g, 5)
    if len(entries) > 0 {
        fmt.Fprintf(&b, "Entry points: %s\n", strings.Join(entries, ", "))
    }

    b.WriteString("Run \"universe query <name>\" in the terminal for details on any symbol.\n")

    return b.String()
}
```

### Update `cmd/universe/init_cmd.go` — generate sessionStart hooks.json

Replace the hooks.json generation. Instead of preToolUse hooks (broken), generate only a sessionStart hook (works):

```go
func generateHooksFile(projectDir string, universePath string) error {
    hooksPath := filepath.Join(projectDir, ".cursor", "hooks.json")

    // Create .cursor/ if needed
    if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
        return err
    }

    // Escape backslashes for JSON on Windows
    escapedPath := strings.ReplaceAll(universePath, `\`, `\\`)

    hooksContent := fmt.Sprintf(`{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "command": "%s session-digest",
        "timeout": 5
      }
    ]
  }
}`, escapedPath)

    if err := os.WriteFile(hooksPath, []byte(hooksContent), 0644); err != nil {
        return err
    }

    fmt.Println("  Created .cursor/hooks.json (sessionStart digest)")
    return nil
}
```

Note the format differences from before:
- `"version": 1` at top level (required by Cursor)
- `"sessionStart"` in camelCase (not PascalCase)
- `"timeout": 5` in seconds (not `timeout_ms`)
- No `matcher`, no `tool_names` object, no nested `hook` object
- Command is a plain string, not an args array

### Register in `cmd/universe/main.go`

```go
rootCmd.AddCommand(sessionDigestCmd())
```

---

## File Changes Summary

### New files
```
cmd/universe/session_digest_cmd.go   — sessionStart digest command
internal/formatter/response.go       — moved from internal/mcpserver/response.go
```

### Modified files
```
cmd/universe/cursor_rule.go          — new lean rule content
cmd/universe/mcp_cmd.go              — deprecation message only
cmd/universe/init_cmd.go             — sessionStart hooks.json, remove preToolUse
cmd/universe/main.go                 — remove hook cmd, add session-digest cmd
```

### Deleted files
```
internal/mcpserver/server.go
internal/mcpserver/tools_unified.go
internal/mcpserver/tools_read.go
internal/mcpserver/tools_graph.go
internal/mcpserver/tools_memory.go
internal/mcpserver/tools_skills.go
internal/mcpserver/tools_plans.go
internal/mcpserver/tools_orchestrator.go
internal/mcpserver/context.go
internal/mcpserver/mcpserver_test.go
cmd/universe/hook_cmd.go
```

### Unchanged files
```
internal/graph/graph.go              — all query methods stay
internal/models/models.go            — all models stay
internal/analyzer/                   — all analysis stays
internal/compress/                   — stays (Phase 2)
internal/memory/                     — stays (Phase 2)
internal/skills/                     — stays (Phase 2)
internal/orchestrator/               — stays
internal/parser/                     — stays
cmd/universe/intel.go                — shell commands stay (this is the core now)
cmd/universe/store_cmds.go           — write commands stay
```

---

## Implementation Order

1. Move `internal/mcpserver/response.go` → `internal/formatter/response.go`
2. Update imports in `cmd/universe/intel.go` to use `formatter` package
3. Delete all other files in `internal/mcpserver/`
4. Delete `cmd/universe/hook_cmd.go`
5. Rewrite `cmd/universe/cursor_rule.go` with new lean rule
6. Create `cmd/universe/session_digest_cmd.go`
7. Update `cmd/universe/init_cmd.go` — sessionStart hooks.json only
8. Update `cmd/universe/mcp_cmd.go` — deprecation message
9. Update `cmd/universe/main.go` — remove hook, add session-digest
10. `go build ./...` and `go test ./...`
11. Build, publish, install on test machine
12. Run `universe init` in the test project
13. Restart Cursor
14. Test

---

## Testing

### Test 1: Rule is generated correctly

```bash
universe init
cat .cursor/rules/universe.mdc
# Should show the new lean rule (no mention of hooks or MCP)
# Should be under 500 tokens
```

### Test 2: hooks.json is correct Cursor format

```bash
cat .cursor/hooks.json
# Should have "version": 1
# Should have "sessionStart" (camelCase)
# Should have "timeout": 5 (seconds, not timeout_ms)
# Should NOT have preToolUse or postToolUse
```

### Test 3: Session digest works

```bash
echo '{}' | universe session-digest
# Should output JSON with additional_context containing the digest
# Digest should be ~200-300 tokens
```

### Test 4: Shell commands still work

```bash
universe query ErrorResponse
universe search "error"
universe impact ErrorResponse
universe deps ErrorResponse
# All should produce compact text output
```

### Test 5: MCP server is gone

```bash
universe mcp --repo .
# Should print deprecation message, not start a server
```

### Test 6: THE TOKEN TEST

In a fresh Cursor session, ask:
```
What does ErrorResponse do?
```

Then ask:
```
Did you use universe query or any universe commands?
How many tokens did this conversation use?
```

**Target:** Under 90K total (below the 94K without-Universe baseline).

If the agent used `universe query`: check that it got the answer from the graph and didn't also read the full file.

If the agent ignored the rule and used Grep + Read: the rule wording needs adjusting, but the architecture is correct.

### Test 7: Multi-question session

Ask 5 questions in one session:
```
1. What does ErrorResponse do?
2. What calls sendError?
3. What would break if I changed TestServer struct?
4. How does the health check flow work?
5. What's the relationship between TestServer and the API routes?
```

**Target:** Total under 120K for all 5 questions.
Without Universe, this would be ~94K + (4 × 15K file reads) = ~154K.

---

## Token Budget

| Component | Cost | Notes |
|-----------|------|-------|
| Cursor system prompt | ~45K | Fixed, can't change |
| Repo rules (00-rules-overview etc) | ~40K | Fixed, project's own |
| universe.mdc | ~400 | NEW — your cost |
| sessionStart digest | ~250 | NEW — one-time per session |
| `universe query` via Shell (per question) | ~300 | Only when agent uses it |
| **Base overhead** | **~85.6K** | Before any question |
| **Per question (graph answers it)** | **~300** | Just the Shell command |
| **Per question (falls back to file read)** | **~5-15K** | Grep + Read + convention rules |

**Comparison:**

| Scenario | Without Universe | With Universe | Savings |
|----------|-----------------|---------------|---------|
| 1 simple question | ~94K | ~86K | ~8K (9%) |
| 1 complex question | ~110K | ~86K | ~24K (22%) |
| 5-question session | ~154K | ~88K | ~66K (43%) |
| 10-question session | ~214K | ~90K | ~124K (58%) |

Universe gets cheaper the more questions you ask, because each `universe query` costs ~300 tokens while each Grep + Read cycle costs ~5-15K.

---

## Known Limitations (to fix in later phases)

1. **Graph coverage is 27.6%** (21/76 files parsed). Caller/impact data is unreliable. The rule explicitly warns the agent about this. Fix: improve parser coverage in a separate patch.

2. **Agent might ignore the rule.** In testing, the agent said "reading the source is faster and more trustworthy." The rule wording addresses this by framing `universe query` as faster and cheaper. If the agent still ignores it, we may need to make the wording stronger or add examples.

3. **Memory and skills are not integrated.** Engine 2 (memory) and Engine 3 (skills) are still shell-only. Phase 2 will explore integrating them into the session digest or the query output.

4. **No sessionStart on Cursor cloud agents.** The sessionStart hook doesn't fire for cloud agents. The rule + CLI approach works regardless.

---

## Acceptance Criteria

- [ ] Zero MCP tools registered (no MCP server at all)
- [ ] Zero preToolUse/postToolUse hooks
- [ ] `universe mcp` prints deprecation message
- [ ] `universe.mdc` is under 500 tokens
- [ ] `universe.mdc` does NOT mention hooks or MCP
- [ ] `universe.mdc` tells agent to use `universe query` via terminal
- [ ] `universe.mdc` warns about unreliable caller counts
- [ ] `.cursor/hooks.json` uses native Cursor format (version 1, camelCase)
- [ ] `.cursor/hooks.json` has ONLY sessionStart (no preToolUse)
- [ ] `universe session-digest` outputs valid Cursor sessionStart JSON
- [ ] Session digest is under 300 tokens
- [ ] All shell commands work unchanged (query, search, impact, deps)
- [ ] All files in `internal/mcpserver/` deleted (except response.go → moved)
- [ ] `cmd/universe/hook_cmd.go` deleted
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] Token count with Universe is under 90K for a simple question
- [ ] Token count is below the 94K without-Universe baseline

---

## What NOT to Change

- Do NOT modify shell commands in `intel.go` (they're the core now)
- Do NOT change graph storage format or analysis logic
- Do NOT touch Engine 2/3/4/5 internals (Phase 2)
- Do NOT add any MCP tools back
- Do NOT add preToolUse or postToolUse hooks (they don't work in Cursor)
- Do NOT modify the repo's own `.cursor/rules/` convention files
