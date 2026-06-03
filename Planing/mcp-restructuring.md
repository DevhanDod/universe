# Token Optimization — MCP Restructuring

## Build Specification for Claude Code

**Problem:** 17 MCP tool schemas add ~50-85K tokens per turn. With MCP connected (200K) costs MORE than without (115K). Universe should REDUCE tokens, not add them.  
**Solution:** Keep 5 write tools as MCP. Move 12 read tools to shell commands. Add static report. Add Cursor hooks.  
**Expected result:** ~100K with Universe vs ~115K without. Universe saves tokens on every question.  
**References:** Graphify (58K stars) and GitNexus (40K stars) both use this approach.

---

## 1. The Architecture Change

```
BEFORE (current):
  17 MCP tools loaded every turn → ~85K schema overhead
  Agent calls MCP for everything → adds tokens on top of Cursor injection
  Result: 200K tokens (worse than 115K without Universe)

AFTER (this spec):
  5 MCP tools loaded every turn → ~15K schema overhead
  Agent runs shell commands for reads → 0 schema overhead
  Agent reads UNIVERSE_REPORT.md for overview → 0 schema overhead
  Result: ~100K tokens (better than 115K without Universe)
```

---

## 2. Tool Migration Map

### 2.1 KEEP as MCP — 5 tools (write operations that need structured input)

| MCP Tool | Engine | Why it stays MCP |
|----------|--------|-----------------|
| `store_plan` | 5 | Planner agent sends structured plan JSON |
| `store_plan_result` | 5 | Executor sends structured result JSON |
| `verify_plan` | 5 | Planner sends approval with note |
| `store_observation` | 2 | Agent sends structured observation with category and graph_node_id |
| `report_skill_execution` | 3 | Agent sends structured success/failure report |

These 5 tools WRITE data to PostgreSQL. They need MCP because the agent sends structured input that must be validated and stored.

### 2.2 MOVE to shell commands — 12 tools (read operations)

| Old MCP Tool | New Shell Command | Engine |
|-------------|-------------------|--------|
| `get_context` | `universe query <name>` | 1 |
| `get_dependencies` | `universe deps <name>` | 1 |
| `get_impact_analysis` | `universe impact <name>` | 1 |
| `search_graph` | `universe search <term>` | 1 |
| `recall_memory` | `universe recall <query>` | 2 |
| `get_observation_details` | `universe recall --id <id>` | 2 |
| `find_skill` | `universe skill find <query>` | 3 |
| `list_skills` | `universe skills list` | 3 |
| `get_skill_lineage` | `universe skill lineage <id>` | 3 |
| `get_plan` | `universe plan get` | 5 |
| `get_plan_result` | `universe plan result <id>` | 5 |
| `get_cost_summary` | `universe cost` | 5 |

These 12 tools only READ data. The agent runs them in the terminal and reads the output. Same data, same format, zero schema overhead.

### 2.3 NEW — Static report file

```
UNIVERSE_REPORT.md — generated during universe init
  Agent reads this file for codebase overview
  No tool call needed
  Replaces get_context for "what does this codebase do?" questions
```

### 2.4 NEW — Cursor hooks

```
.cursor/hooks.json — intercepts grep/search
  Enriches search results with graph context
  Agent gets structural info without making any tool call
```

---

## 3. New Shell Commands — Implementation

### 3.1 Add to `cmd/universe/query.go` — NEW FILE

```go
package main

import (
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"

    "universe/internal/graph"
)

// ============================================================
// universe query <name> — replaces get_context MCP tool
// ============================================================

var queryCmd = &cobra.Command{
    Use:   "query <symbol-name>",
    Short: "360° view of a function or type — callers, callees, flows, cluster, impact",
    Long: `Get complete context for a symbol in one call.
Returns callers, callees, execution flows, cluster membership,
and impact assessment. Designed for AI agents — compact output,
no source code, just structural intelligence.

Examples:
  universe query StartTestServer
  universe query "auth.ValidateToken"
  universe query --json StartTestServer`,
    Args: cobra.ExactArgs(1),
    Run:  runQuery,
}

var queryJSON bool

func init() {
    queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output as JSON")
    rootCmd.AddCommand(queryCmd)
}

func runQuery(cmd *cobra.Command, args []string) {
    name := args[0]

    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Graph not found. Run 'universe init' first.\n")
        os.Exit(1)
    }

    node := g.FindNode(name)
    if node == nil {
        // Try fuzzy search
        results := g.Search(name, 5)
        if len(results) == 0 {
            fmt.Fprintf(os.Stderr, "No node found matching '%s'\n", name)
            os.Exit(1)
        }
        fmt.Fprintf(os.Stderr, "No exact match for '%s'. Did you mean:\n", name)
        for _, r := range results {
            fmt.Fprintf(os.Stderr, "  %s [%s] %s:%d\n", r.Name, r.Type, r.FilePath, r.StartLine)
        }
        os.Exit(1)
    }

    callers := g.GetCallers(node.ID)
    callees := g.GetCallees(node.ID)

    // Text output (default) — compact, designed for AI consumption
    fmt.Printf("%s [%s] %s:%d\n", node.Name, node.Type, node.FilePath, node.StartLine)
    fmt.Printf("Cluster: %s\n", node.Cluster)
    fmt.Println()

    if len(callers) > 0 {
        fmt.Printf("Callers (%d):\n", len(callers))
        for i, c := range callers {
            if i >= 15 {
                fmt.Printf("  ...and %d more\n", len(callers)-15)
                break
            }
            fmt.Printf("  %s [%s] %s:%d\n", c.Name, c.Type, c.FilePath, c.StartLine)
        }
    } else {
        fmt.Println("Callers: none (entry point or unused)")
    }
    fmt.Println()

    if len(callees) > 0 {
        fmt.Printf("Callees (%d):\n", len(callees))
        for i, c := range callees {
            if i >= 15 {
                fmt.Printf("  ...and %d more\n", len(callees)-15)
                break
            }
            fmt.Printf("  %s [%s] %s:%d\n", c.Name, c.Type, c.FilePath, c.StartLine)
        }
    } else {
        fmt.Println("Callees: none (leaf function)")
    }

    // Flows
    if len(node.Flows) > 0 {
        fmt.Println()
        fmt.Printf("Flows: %s\n", strings.Join(node.Flows, ", "))
    }

    // Impact summary
    fmt.Println()
    risk := "low"
    if len(callers) > 5 {
        risk = "medium"
    }
    if len(callers) > 10 || g.IsCrossRepo(node.ID) {
        risk = "high"
    }
    fmt.Printf("Impact: %s — %d callers, %d callees\n", risk, len(callers), len(callees))
}

// ============================================================
// universe deps <name> — replaces get_dependencies MCP tool
// ============================================================

var depsCmd = &cobra.Command{
    Use:   "deps <symbol-name>",
    Short: "Show callers and callees for a function",
    Args:  cobra.ExactArgs(1),
    Run:   runDeps,
}

func init() {
    rootCmd.AddCommand(depsCmd)
}

func runDeps(cmd *cobra.Command, args []string) {
    // Same as query but without flows/impact — just callers + callees
    // Implementation similar to runQuery but shorter output
}

// ============================================================
// universe impact <name> — replaces get_impact_analysis MCP tool
// ============================================================

var impactCmd = &cobra.Command{
    Use:   "impact <symbol-name>",
    Short: "Blast radius analysis — what breaks if this changes",
    Args:  cobra.ExactArgs(1),
    Run:   runImpact,
}

func init() {
    rootCmd.AddCommand(impactCmd)
}

func runImpact(cmd *cobra.Command, args []string) {
    name := args[0]

    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Graph not found. Run 'universe init' first.\n")
        os.Exit(1)
    }

    node := g.FindNode(name)
    if node == nil {
        fmt.Fprintf(os.Stderr, "No node found matching '%s'\n", name)
        os.Exit(1)
    }

    // Get precomputed impact or compute on the fly
    impact := g.GetImpact(node.ID)

    fmt.Printf("%s [%s] %s:%d\n", node.Name, node.Type, node.FilePath, node.StartLine)
    fmt.Printf("Risk: %s\n", impact.RiskLevel)
    fmt.Printf("Total affected: %d\n", impact.TotalAffected)
    fmt.Println()

    if len(impact.WillBreak) > 0 {
        fmt.Println("WILL BREAK (depth 1):")
        for _, n := range impact.WillBreak {
            fmt.Printf("  %s [%s] %s:%d [%.0f%%]\n", n.Name, n.Kind, n.File, n.Line, n.Confidence*100)
        }
    }

    if len(impact.LikelyAffected) > 0 {
        fmt.Println()
        fmt.Println("LIKELY AFFECTED (depth 2):")
        for _, n := range impact.LikelyAffected {
            fmt.Printf("  %s [%s] %s:%d\n", n.Name, n.Kind, n.File, n.Line)
        }
    }

    if len(impact.AffectedFlows) > 0 {
        fmt.Println()
        fmt.Printf("Affected flows: %s\n", strings.Join(impact.AffectedFlows, ", "))
    }

    if len(impact.AffectedClusters) > 0 {
        fmt.Printf("Affected clusters: %s\n", strings.Join(impact.AffectedClusters, ", "))
    }
}

// ============================================================
// universe search <term> — replaces search_graph MCP tool
// ============================================================

var searchCmd = &cobra.Command{
    Use:   "search <term>",
    Short: "Search the knowledge graph by name",
    Args:  cobra.ExactArgs(1),
    Run:   runSearch,
}

func init() {
    rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) {
    term := args[0]

    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Graph not found.\n")
        os.Exit(1)
    }

    results := g.Search(term, 10)
    if len(results) == 0 {
        fmt.Printf("No results for '%s'\n", term)
        return
    }

    fmt.Printf("Results for '%s' (%d):\n\n", term, len(results))
    for _, r := range results {
        fmt.Printf("  %s [%s] %s:%d (%s) — %d callers, %d callees\n",
            r.Name, r.Type, r.FilePath, r.StartLine, r.Cluster,
            r.CallerCount, r.CalleeCount)
    }
}
```

### 3.2 Add to `cmd/universe/recall.go` — NEW FILE

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/memory"
)

// ============================================================
// universe recall <query> — replaces recall_memory MCP tool
// ============================================================

var recallCmd = &cobra.Command{
    Use:   "recall <query>",
    Short: "Search your past observations from previous sessions",
    Long: `Search your personal memory — past fixes, patterns, decisions.
Returns compact summaries. Use --id for full detail on a specific observation.

Examples:
  universe recall "type mismatch"
  universe recall --node auth:ValidateToken
  universe recall --id abc123`,
    Run: runRecall,
}

var recallNode string
var recallID string
var recallLimit int

func init() {
    recallCmd.Flags().StringVar(&recallNode, "node", "", "Filter by graph node ID")
    recallCmd.Flags().StringVar(&recallID, "id", "", "Get full detail for specific observation")
    recallCmd.Flags().IntVar(&recallLimit, "limit", 5, "Max results")
    rootCmd.AddCommand(recallCmd)
}

func runRecall(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("No database configured. Run: universe config set db postgres://...")
        return
    }

    store, err := memory.NewStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    defer store.Close()

    // Detail mode: get specific observation
    if recallID != "" {
        obs, err := store.GetByID(recallID)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Not found: %v\n", err)
            os.Exit(1)
        }
        fmt.Printf("[%s] %s\n", obs.Category, obs.Summary)
        fmt.Printf("  Node: %s\n", obs.GraphNodeID)
        fmt.Printf("  Date: %s\n", obs.CreatedAt.Format("2006-01-02 15:04"))
        if obs.Detail != "" {
            fmt.Printf("  Detail: %s\n", obs.Detail)
        }
        return
    }

    // Search mode
    query := ""
    if len(args) > 0 {
        query = args[0]
    }

    results, err := store.SearchKeyword(query, getDeveloperID(), recallLimit)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    if len(results) == 0 {
        fmt.Println("No observations found.")
        return
    }

    fmt.Printf("Observations (%d):\n\n", len(results))
    for _, r := range results {
        fmt.Printf("  [%s] %s\n", r.Category, r.Summary)
        fmt.Printf("    Node: %s | %s | conf: %.0f%%\n",
            r.GraphNodeID, r.CreatedAt.Format("Jan 2"), r.Confidence*100)
    }
}
```

### 3.3 Add to `cmd/universe/skillfind.go` — NEW FILE

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/skills"
)

// ============================================================
// universe skill find <query> — replaces find_skill MCP tool
// ============================================================

var skillFindCmd = &cobra.Command{
    Use:   "find <query>",
    Short: "Search for a matching skill recipe",
    Args:  cobra.ExactArgs(1),
    Run:   runSkillFind,
}

func init() {
    skillsCmd.AddCommand(skillFindCmd)
}

func runSkillFind(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("No database configured.")
        return
    }

    store, err := skills.NewStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    defer store.Close()

    matcher := skills.NewMatcher(store, nil, skills.DefaultConfig())
    result, err := matcher.Match(skills.MatchQuery{TaskText: args[0]})
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    if result.ExplorationTriggered {
        fmt.Println("Exploration mode — reason from scratch this time.")
        return
    }

    if result.BestMatch == nil {
        fmt.Println("No matching skill found.")
        return
    }

    s := result.BestMatch
    fmt.Printf("Skill: %s v%d (%s)\n", s.Name, s.Version, s.Evolution)
    fmt.Printf("Success: %.0f%% (%d uses) | Confidence: %.0f%%\n",
        s.SuccessRate*100, s.TimesApplied, s.Confidence*100)
    fmt.Println()
    fmt.Println("⚠️  REQUIRES VERIFICATION — premium model must review before use")
    fmt.Println()
    fmt.Println("Instruction:")
    fmt.Println(s.Instruction)
}

// ============================================================
// universe skill lineage <id> — replaces get_skill_lineage MCP tool
// ============================================================

var skillLineageCmd = &cobra.Command{
    Use:   "lineage <skill-name>",
    Short: "Show evolution history of a skill",
    Args:  cobra.ExactArgs(1),
    Run:   runSkillLineage,
}

func init() {
    skillsCmd.AddCommand(skillLineageCmd)
}

func runSkillLineage(cmd *cobra.Command, args []string) {
    // Query lineage via recursive CTE, print as tree:
    // v1 (captured, Alice) → v2 (fix, auto) → v3 (fix, auto) [active]
    //                          ↳ type-fix-python v1 (derived, Bob)
}
```

### 3.4 Add to `cmd/universe/plancli.go` — NEW FILE

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/orchestrator"
)

// ============================================================
// universe plan get — replaces get_plan MCP tool
// ============================================================

var planGetCmd = &cobra.Command{
    Use:   "get",
    Short: "Get the latest pending plan",
    Run:   runPlanGet,
}

func init() {
    // planCmd already exists from workspace.go (universe plan opens planner)
    // Add "get" as a subcommand under a different parent or rename
    // Option: use "universe plan show" or add to existing planCmd
    
    // Create a plans management command
    var plansCmd = &cobra.Command{
        Use:   "plans",
        Short: "Manage execution plans",
    }
    plansCmd.AddCommand(planGetCmd)
    plansCmd.AddCommand(planResultCmd)
    rootCmd.AddCommand(plansCmd)
}

func runPlanGet(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("No database configured.")
        return
    }

    planStore, err := orchestrator.NewPlanStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    defer planStore.Close()

    plan, err := planStore.GetLatestPlan(getDeveloperID())
    if err != nil || plan == nil {
        fmt.Println("No pending plan found.")
        return
    }

    fmt.Printf("Plan: %s\n", plan.Title)
    fmt.Printf("Status: %s\n", plan.Status)
    fmt.Printf("Risk: %s\n", plan.RiskLevel)
    fmt.Println()
    fmt.Println("Steps:")
    for i, step := range plan.Steps {
        fmt.Printf("  %d. %s\n", i+1, step)
    }
    if len(plan.FilesToChange) > 0 {
        fmt.Println()
        fmt.Printf("Files: %v\n", plan.FilesToChange)
    }
}

// ============================================================
// universe plans result <id> — replaces get_plan_result MCP tool
// ============================================================

var planResultCmd = &cobra.Command{
    Use:   "result [plan-id]",
    Short: "Get execution result for a plan",
    Run:   runPlanResult,
}

func runPlanResult(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("No database configured.")
        return
    }

    planStore, err := orchestrator.NewPlanStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    defer planStore.Close()

    var plan *orchestrator.Plan
    if len(args) > 0 {
        plan, _ = planStore.GetPlanByID(args[0])
    } else {
        plan, _ = planStore.GetLatestCompletedPlan(getDeveloperID())
    }

    if plan == nil {
        fmt.Println("No completed plan found.")
        return
    }

    fmt.Printf("Plan: %s\n", plan.Title)
    fmt.Printf("Status: %s\n", plan.Status)
    if plan.ResultSuccess != nil {
        if *plan.ResultSuccess {
            fmt.Println("Result: ✅ Success")
        } else {
            fmt.Println("Result: ❌ Failed")
        }
    }
    if plan.ResultSummary != "" {
        fmt.Printf("Summary: %s\n", plan.ResultSummary)
    }
    if plan.ResultError != "" {
        fmt.Printf("Error: %s\n", plan.ResultError)
    }
}

// ============================================================
// universe cost — replaces get_cost_summary MCP tool
// ============================================================

var costCmd = &cobra.Command{
    Use:   "cost",
    Short: "Show estimated cost savings",
    Run:   runCost,
}

func init() {
    rootCmd.AddCommand(costCmd)
}

func runCost(cmd *cobra.Command, args []string) {
    // Query plan_costs materialized view
    // Print: actual cost, would-have-been, savings, plan count
}
```

---

## 4. Update MCP Server — Remove 12 Read Tools

### 4.1 Update `internal/mcpserver/server.go`

**REMOVE these 12 tool registrations from RunStdio:**

```go
// REMOVE all of these — they are now shell commands:

// Engine 1 — all 4 moved to shell
// mcp.AddTool(server, &mcp.Tool{Name: "get_context", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_dependencies", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_impact_analysis", ...
// mcp.AddTool(server, &mcp.Tool{Name: "search_graph", ...

// Engine 2 — reads moved to shell
// mcp.AddTool(server, &mcp.Tool{Name: "recall_memory", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_observation_details", ...

// Engine 3 — reads moved to shell
// mcp.AddTool(server, &mcp.Tool{Name: "find_skill", ...
// mcp.AddTool(server, &mcp.Tool{Name: "list_skills", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_skill_lineage", ...

// Engine 5 — reads moved to shell
// mcp.AddTool(server, &mcp.Tool{Name: "get_plan", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_plan_result", ...
// mcp.AddTool(server, &mcp.Tool{Name: "get_cost_summary", ...
```

**KEEP these 5 tool registrations:**

```go
// Engine 2 — write
mcp.AddTool(server, &mcp.Tool{
    Name:        "store_observation",
    Description: "Save an observation to your personal memory — a fix, pattern, or decision from this session.",
}, h.HandleStoreObservation)

// Engine 3 — write
mcp.AddTool(server, &mcp.Tool{
    Name:        "report_skill_execution",
    Description: "Report success or failure of applying a skill recipe.",
}, h.HandleReportSkillExecution)

// Engine 5 — writes
mcp.AddTool(server, &mcp.Tool{
    Name:        "store_plan",
    Description: "Store a step-by-step plan for the executor agent. Optionally opens the executor Cursor window.",
}, h.HandleStorePlan)

mcp.AddTool(server, &mcp.Tool{
    Name:        "store_plan_result",
    Description: "Report the result of executing a plan — what was done, files changed, tests passed or failed.",
}, h.HandleStorePlanResult)

mcp.AddTool(server, &mcp.Tool{
    Name:        "verify_plan",
    Description: "Approve or reject an executor's plan result after reviewing it.",
}, h.HandleVerifyPlan)
```

### 4.2 Delete handler files for removed tools

```bash
# These files can be simplified or removed:
# internal/mcpserver/tools_graph.go    — DELETE (graph reads are now shell commands)
# internal/mcpserver/tools_memory.go   — SIMPLIFY (keep only store_observation handler)
# internal/mcpserver/tools_skills.go   — SIMPLIFY (keep only report_skill_execution handler)
# internal/mcpserver/tools_plans.go    — SIMPLIFY (keep store_plan, store_plan_result, verify_plan)
```

---

## 5. Generate `UNIVERSE_REPORT.md` During Init

### 5.1 Add report generation to `universe init`

After building the graph, clusters, and flows, generate a markdown report:

```go
// internal/report/generator.go — NEW FILE

// GenerateReport creates UNIVERSE_REPORT.md from the graph data.
// This file is read by the AI agent for codebase overview.
// It replaces the need for get_context MCP calls on broad questions.
//
// The report contains:
//   1. Codebase summary (language, file count, node count, package count)
//   2. Cluster overview (each cluster with key files and entry points)
//   3. God nodes (most-connected functions — high impact targets)
//   4. Key execution flows (the main paths through the codebase)
//   5. Cross-repo dependencies (if multiple repos detected)
//   6. Suggested questions the graph can answer
//
// Target size: 3-8KB (small enough to read into context cheaply)

func GenerateReport(g *graph.Graph, outputPath string) error
```

### 5.2 Report template

```markdown
# Universe — Codebase Intelligence Report

Generated: 2026-05-28 | Nodes: 504 | Edges: 2669 | Packages: 17

## Clusters

| Cluster | Files | Key functions | Entry points |
|---------|-------|--------------|--------------|
| api-handlers | 12 | LoginHandler, RegisterHandler | Router.Setup |
| database | 8 | InitDB, MigrateDB, QueryBuilder | InitDB |
| test-infrastructure | 15 | StartTestServer, SetupFixtures | Test* functions |
| authentication | 6 | ValidateToken, GenerateJWT | ValidateToken |
| ... | | | |

## High-Impact Nodes (change carefully)

1. **InitDB** (db.go:25) — 12 dependents across 3 clusters. Changes affect all tests + production.
2. **ValidateToken** (auth.go:42) — 8 dependents, cross-cluster. Authentication gateway.
3. **Router.Setup** (router.go:10) — 15 dependents. All API handlers flow through this.

## Key Execution Flows

1. **Login flow**: LoginHandler → ValidateInput → AuthService.Login → DB.FindUser → JWT.Generate
2. **Test setup**: TestX → StartTestServer → SetupRouter → InitDB → LoadConfig
3. **API request**: Router → Middleware.Auth → Handler → DB.Query → Response.JSON

## For AI Agents

To get details about any symbol, run in the terminal:
  universe query <name>        — full context (callers, callees, flows, impact)
  universe impact <name>       — blast radius for planned changes
  universe search <term>       — find symbols by name
  universe recall <query>      — search past session memory
  universe skill find <query>  — find matching skill recipes
```

### 5.3 Update `universe init` to generate the report

```go
// At the end of runInit, after saving graph.json:

reportPath := filepath.Join(LocalDataDir(), "UNIVERSE_REPORT.md")
if err := report.GenerateReport(g, reportPath); err != nil {
    fmt.Fprintf(os.Stderr, "Warning: couldn't generate report: %v\n", err)
} else {
    fmt.Printf("   Report: %s\n", reportPath)
}
```

---

## 6. Update Cursor Rule

### 6.1 Replace the current `.cursor/rules/universe.mdc`

The rule must tell the agent to use shell commands and the report instead of expecting MCP tools for reads:

```markdown
---
description: "Universe — AI code intelligence"
globs: ["**/*"]
alwaysApply: true
---

# Universe Code Intelligence

## For codebase overview
Read .universe/UNIVERSE_REPORT.md — it has clusters, key nodes, and flows.

## For specific code questions
Run these in the terminal (NOT MCP tools):

  universe query <name>        Full context: callers, callees, flows, impact
  universe impact <name>       What breaks if this changes
  universe search <term>       Find functions/types by name
  universe recall <query>      Your past session observations
  universe skill find <query>  Matching skill recipes (verify before using)

## When to use vs when to skip

USE Universe commands when:
  - "What depends on X?" → universe query X
  - "What breaks if I change X?" → universe impact X
  - "How does the auth flow work?" → universe query AuthHandler
  - Cross-file or cross-package questions

SKIP Universe commands when:
  - "What does this function do?" → just read the file
  - "Fix this bug on line 42" → just read the file
  - The file is already open in your editor
  - Single function in a single file → read the file directly

## For storing data (these ARE MCP tools)
  store_observation   — save a fix/pattern/decision to your memory
  store_plan          — save a plan for the executor agent
  store_plan_result   — report execution result
  verify_plan         — approve or reject a result
  report_skill_execution — report skill success/failure

## Output rules
  - No "I'd be happy to help", no filler
  - Code stays exact
  - Max 2 sentences for explanations
```

---

## 7. Add Cursor Hooks (Optional Enhancement)

### 7.1 Create `.cursor/hooks.json`

```json
{
  "hooks": {
    "beforeShellExecution": {
      "command": "node .cursor/hooks/universe-hook.cjs"
    }
  }
}
```

### 7.2 Create `.cursor/hooks/universe-hook.cjs`

```javascript
#!/usr/bin/env node

// Universe Cursor Hook — enriches grep/search with graph context
// Runs BEFORE the agent executes a shell command
// If the command is a search (grep, rg, find), appends graph context

const { execSync } = require('child_process');
const fs = require('fs');

// Read hook input from stdin
let input = '';
process.stdin.on('data', d => input += d);
process.stdin.on('end', () => {
    try {
        const data = JSON.parse(input);
        const command = data.command || data.tool_input?.command || '';

        // Only intercept search commands
        if (!isSearchCommand(command)) {
            // Not a search — let it run normally
            process.stdout.write(JSON.stringify({}));
            return;
        }

        // Extract the search pattern
        const pattern = extractPattern(command);
        if (!pattern || pattern.length < 3) {
            process.stdout.write(JSON.stringify({}));
            return;
        }

        // Check if .universe/graph.json exists
        if (!fs.existsSync('.universe/graph.json')) {
            process.stdout.write(JSON.stringify({}));
            return;
        }

        // Run universe query to get graph context
        try {
            const context = execSync(`universe query "${pattern}" 2>/dev/null`, {
                encoding: 'utf8',
                timeout: 500, // 500ms max — must not slow down the agent
            });

            if (context && context.trim()) {
                // Append graph context to the tool result
                process.stdout.write(JSON.stringify({
                    additionalContext: `\n--- Universe Graph Context ---\n${context.trim()}\n--- End Graph Context ---\n`
                }));
                return;
            }
        } catch {
            // universe query failed or timed out — continue without enrichment
        }

        process.stdout.write(JSON.stringify({}));
    } catch {
        process.stdout.write(JSON.stringify({}));
    }
});

function isSearchCommand(cmd) {
    return /\b(grep|rg|find|ag|ack|git\s+grep)\b/.test(cmd);
}

function extractPattern(cmd) {
    // Extract the search term from grep/rg commands
    // "rg StartTestServer" → "StartTestServer"
    // "grep -r 'ValidateToken'" → "ValidateToken"
    const match = cmd.match(/(?:grep|rg|ag|ack|git\s+grep)\s+(?:-[^\s]+\s+)*['"]?([^'";\s|>]+)/);
    return match ? match[1] : null;
}
```

### 7.3 Generate hooks during `universe setup`

Add hook generation to `internal/orchestrator/setup.go`:

```go
func generateCursorHooks(projectDir string) error {
    hooksDir := filepath.Join(projectDir, ".cursor", "hooks")
    os.MkdirAll(hooksDir, 0755)

    // Write hooks.json
    hooksConfig := `{
  "hooks": {
    "beforeShellExecution": {
      "command": "node .cursor/hooks/universe-hook.cjs"
    }
  }
}`
    os.WriteFile(filepath.Join(projectDir, ".cursor", "hooks.json"), []byte(hooksConfig), 0644)

    // Write the hook script (embed the JS from section 7.2)
    // ...

    return nil
}
```

---

## 8. Updated Command Tree

```
universe init                     Scan + build graph + generate report
universe status                   All engine statuses
universe setup                    Pick models, generate all config

# READ commands (shell — 0 schema overhead):
universe query <name>             360° view of a symbol
universe deps <name>              Callers and callees
universe impact <name>            Blast radius analysis
universe search <term>            Find symbols by name
universe recall <query>           Search personal memory
universe recall --id <id>         Full observation detail
universe skill find <query>       Find matching skill recipe
universe skills list              List all active skills
universe skill lineage <name>     Skill evolution history
universe plans get                Latest pending plan
universe plans result [id]        Plan execution result
universe cost                     Cost savings summary

# WRITE commands (MCP — need structured input):
# (these stay as MCP tools, called by the agent in chat)
store_observation                 Save observation to memory
report_skill_execution            Report skill success/failure
store_plan                        Save plan for executor
store_plan_result                 Report execution result
verify_plan                       Approve/reject result

# WORKSPACE commands:
universe plan                     Open planner Cursor window
universe exec                     Open executor Cursor window
universe start                    Open both windows
universe dashboard                Open web dashboard
```

---

## 9. Testing

```bash
# Test 1: Shell commands work
universe query StartTestServer
# Should return compact text with callers, callees, flows

universe impact InitDB
# Should return blast radius grouped by depth

universe search "auth"
# Should return matching nodes

universe recall "type mismatch"
# Should return past observations

# Test 2: MCP only has 5 tools
# Connect to Cursor, check MCP panel
# Should show: store_observation, report_skill_execution,
#              store_plan, store_plan_result, verify_plan
# Should NOT show: get_context, get_dependencies, etc.

# Test 3: Token comparison
# Fresh Cursor chat WITH Universe MCP:
# Ask: "What happens in StartTestServer?"
# Note token count → should be ~100K (was 200K)

# Fresh Cursor chat WITHOUT Universe MCP:
# Same question
# Note token count → should be ~115K

# Universe should use FEWER tokens, not more

# Test 4: UNIVERSE_REPORT.md exists
cat .universe/UNIVERSE_REPORT.md
# Should show clusters, god nodes, flows

# Test 5: Cursor hooks work (if implemented)
# In Cursor, grep for a function name
# The result should include "Universe Graph Context" section
```

---

## 10. Acceptance Criteria

- [ ] MCP server registers exactly 5 tools (not 17)
- [ ] `universe query <name>` returns compact context via shell
- [ ] `universe impact <name>` returns blast radius via shell
- [ ] `universe search <term>` returns results via shell
- [ ] `universe recall <query>` returns observations via shell
- [ ] `universe skill find <query>` returns matching skill via shell
- [ ] `universe plans get` returns pending plan via shell
- [ ] `universe cost` returns savings summary via shell
- [ ] `UNIVERSE_REPORT.md` generated during `universe init`
- [ ] Cursor rule updated to reference shell commands, not MCP tools
- [ ] Token usage with Universe connected is LOWER than without Universe
- [ ] All shell commands complete in under 500ms
- [ ] Shell command output is compact text (no JSON blobs, no source code)
- [ ] Each shell command output is under 2KB
- [ ] Cursor hooks enrich search results with graph context (optional)
