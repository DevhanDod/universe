# Critical Fix: Graph Token Optimization

## Precompute at Index Time, Return Compact Answers at Query Time

**Problem:** Graph MCP tools return raw node data with full source code, causing 4x MORE tokens (534K) than without the graph (121K). The agent explores raw connections and makes multiple tool calls, each returning massive JSON.

**Root cause:** Universe sends raw graph data and expects the AI to figure out the relationships. GitNexus (40k stars) proved the opposite approach works: precompute everything at index time, return structured answers at query time.

**Fix:** Three changes — (1) precompute clusters and flows during `universe init`, (2) restructure tool responses to return compact pre-structured answers, (3) never send source code through MCP.

---

## 1. The Token Problem — Before and After

### Before (current — 534K tokens)

```
Agent calls: get_dependencies("StartTestServer")
Universe returns: {
  "node": {
    "id": "test_helpers:StartTestServer",
    "name": "StartTestServer",
    "type": "function",
    "metadata": {
      "content": "func StartTestServer(...) {\n    router := SetupRouter()\n    
                  db := InitDB()\n    config := LoadConfig()\n    ... 
                  200 LINES OF SOURCE CODE ..."
    }
  },
  "callers": [ ...47 full node objects with metadata.content... ],
  "callees": [ ...12 full node objects with metadata.content... ]
}
// Response size: ~180KB of JSON
// Agent then calls get_dependencies for each caller → more massive responses
// Total: 5-8 tool calls, 534K tokens
```

### After (fixed — target <100K tokens)

```
Agent calls: get_dependencies("StartTestServer")
Universe returns: {
  "node": "StartTestServer [func] test_helpers.go:42",
  "callers": [
    "TestGetUser [func] user_test.go:15 — calls StartTestServer directly",
    "TestCreateOrder [func] order_test.go:30 — calls StartTestServer directly", 
    "TestAuth [func] auth_test.go:8 — calls StartTestServer directly"
  ],
  "callees": [
    "SetupRouter [func] router.go:10",
    "InitDB [func] db.go:25", 
    "LoadConfig [func] config.go:5"
  ],
  "cluster": "test-infrastructure",
  "risk": "low — 3 callers, same repo, all test files",
  "summary": "Test helper that sets up the server. 3 test files depend on it. Changes here affect all integration tests."
}
// Response size: ~800 bytes
// Agent has the COMPLETE picture in ONE call
// No need for follow-up calls
// Total: 1 tool call, ~2K tokens for the tool response
```

---

## 2. What to Precompute During `universe init`

Add three new analysis passes after the graph is built:

### 2.1 Cluster Detection

Group related nodes into functional communities (auth, API, database, tests, etc.)

```go
// internal/analyzer/clusters.go — NEW FILE

// DetectClusters groups graph nodes into functional communities.
// Runs during universe init after the graph is built.
//
// Algorithm: Label Propagation (simple, fast, good enough)
//   1. Assign each node to its own cluster (by package name initially)
//   2. For each node, adopt the most common cluster among neighbors
//   3. Repeat until stable (usually 3-5 iterations)
//   4. Name clusters by dominant package or common keywords
//
// Alternative: Leiden algorithm (better quality, more complex)
//
// Output: map[nodeID] → clusterName
//
// Example clusters detected:
//   "authentication" — auth.go, validate.go, token.go, middleware.go
//   "api-handlers"   — handler.go, router.go, middleware.go
//   "database"       — db.go, migration.go, models.go
//   "test-infra"     — test_helpers.go, fixtures.go, mocks.go
//   "config"         — config.go, env.go, flags.go

type Cluster struct {
    Name       string   `json:"name"`
    NodeCount  int      `json:"node_count"`
    NodeIDs    []string `json:"node_ids"`
    KeyFiles   []string `json:"key_files"`    // most connected files in this cluster
    EntryPoints []string `json:"entry_points"` // nodes called from outside the cluster
}

func DetectClusters(g *graph.Graph) []Cluster
```

### 2.2 Execution Flow Detection

Trace call chains from entry points (main, handlers, test functions) through the graph.

```go
// internal/analyzer/flows.go — NEW FILE

// DetectFlows traces execution paths from entry points through the call graph.
// Runs during universe init.
//
// Algorithm:
//   1. Find entry points (functions with many callers OR known patterns):
//      - main(), init()
//      - HTTP handlers (Handler suffix, router registrations)
//      - Test functions (Test prefix)
//      - CLI commands
//   2. For each entry point, DFS through callees
//   3. Record the path as a "flow"
//   4. Name the flow by the entry point
//
// Output: list of execution flows
//
// Example:
//   Flow "LoginHandler": LoginHandler → ValidateInput → AuthService.Login 
//     → DB.FindUser → PasswordHash.Compare → TokenService.Generate → Response.JSON
//   Flow "TestGetUser": TestGetUser → StartTestServer → SetupRouter → InitDB 
//     → httptest.NewRequest → handler.ServeHTTP

type Flow struct {
    Name       string     `json:"name"`
    EntryPoint string     `json:"entry_point"`   // node ID
    Steps      []FlowStep `json:"steps"`
    StepCount  int        `json:"step_count"`
    CrossRepo  bool       `json:"cross_repo"`
    Clusters   []string   `json:"clusters"`       // which clusters this flow touches
}

type FlowStep struct {
    NodeID   string `json:"node_id"`
    Name     string `json:"name"`
    File     string `json:"file"`
    Line     int    `json:"line"`
    Cluster  string `json:"cluster"`
    StepNum  int    `json:"step_num"`
}

func DetectFlows(g *graph.Graph, clusters []Cluster) []Flow
```

### 2.3 Impact Chains

For each node, precompute the blast radius: what breaks if this changes.

```go
// internal/analyzer/impact.go — NEW FILE

// PrecomputeImpact calculates the blast radius for frequently-accessed nodes.
// Runs during universe init.
//
// Algorithm:
//   1. For each node, BFS upstream (who calls this, who calls them)
//   2. Group by depth: depth 1 = WILL BREAK, depth 2 = LIKELY AFFECTED, depth 3+ = POSSIBLY AFFECTED
//   3. Calculate confidence based on call relationship type
//   4. Store as a precomputed impact summary per node
//
// Only precomputes for "important" nodes (>= 3 callers, or part of multiple flows).
// Other nodes compute impact on-demand (but still return compact results).

type ImpactSummary struct {
    NodeID      string            `json:"node_id"`
    NodeName    string            `json:"node_name"`
    TotalAffected int            `json:"total_affected"`
    CrossRepo   bool              `json:"cross_repo"`
    RiskLevel   string            `json:"risk_level"` // low, medium, high
    ByDepth     map[int][]Impact  `json:"by_depth"`   // depth → list of affected
    AffectedFlows []string        `json:"affected_flows"`
    AffectedClusters []string     `json:"affected_clusters"`
    Summary     string            `json:"summary"`     // human-readable one-liner
}

type Impact struct {
    NodeID     string  `json:"node_id"`
    NodeName   string  `json:"name"`
    File       string  `json:"file"`
    Line       int     `json:"line"`
    Confidence float64 `json:"confidence"`
    Relation   string  `json:"relation"` // "calls", "imports", "extends"
}

func PrecomputeImpact(g *graph.Graph, flows []Flow) map[string]*ImpactSummary
```

### 2.4 Updated `universe init` Pipeline

```go
// Current pipeline:
//   1. Scan files
//   2. Parse with tree-sitter
//   3. Build graph (nodes + edges)
//   4. Save graph.json

// New pipeline:
//   1. Scan files
//   2. Parse with tree-sitter
//   3. Build graph (nodes + edges)
//   4. Detect clusters            ← NEW
//   5. Detect execution flows     ← NEW
//   6. Precompute impact chains   ← NEW
//   7. Save graph.json (now includes clusters, flows, impact)
```

### 2.5 Updated `graph.json` Structure

```json
{
  "nodes": {
    "test_helpers:StartTestServer": {
      "id": "test_helpers:StartTestServer",
      "name": "StartTestServer",
      "type": "function",
      "file_path": "test_helpers.go",
      "package": "test_helpers",
      "start_line": 42,
      "end_line": 85,
      "cluster": "test-infrastructure",
      "flows": ["TestGetUser", "TestCreateOrder", "TestAuth"],
      "caller_count": 3,
      "callee_count": 3
    }
  },
  "edges": [ ... ],
  "clusters": [
    {
      "name": "test-infrastructure",
      "node_count": 12,
      "key_files": ["test_helpers.go", "fixtures.go"],
      "entry_points": ["TestGetUser", "TestCreateOrder"]
    }
  ],
  "flows": [
    {
      "name": "TestGetUser",
      "entry_point": "user_test:TestGetUser",
      "steps": [
        {"name": "TestGetUser", "file": "user_test.go", "line": 15},
        {"name": "StartTestServer", "file": "test_helpers.go", "line": 42},
        {"name": "SetupRouter", "file": "router.go", "line": 10}
      ],
      "clusters": ["test-infrastructure", "api-handlers"]
    }
  ],
  "impact": {
    "test_helpers:StartTestServer": {
      "total_affected": 3,
      "risk_level": "low",
      "summary": "Test helper — 3 test files depend on it. Changes affect integration tests only.",
      "by_depth": {
        "1": [
          {"name": "TestGetUser", "file": "user_test.go", "line": 15, "confidence": 0.95},
          {"name": "TestCreateOrder", "file": "order_test.go", "line": 30, "confidence": 0.95},
          {"name": "TestAuth", "file": "auth_test.go", "line": 8, "confidence": 0.95}
        ]
      },
      "affected_flows": ["TestGetUser", "TestCreateOrder", "TestAuth"]
    }
  }
}
```

**CRITICAL: Remove `metadata.content` from graph.json.** Source code should NEVER be stored in the graph. The graph stores structure (what calls what), not content (the actual code). If the agent needs source code, it reads the file directly in Cursor.

---

## 3. Restructure MCP Tool Responses

### Rule 1: Never send source code through MCP

```go
// BEFORE — sends source code:
type NodeInfo struct {
    ID       string                 `json:"id"`
    Name     string                 `json:"name"`
    Kind     string                 `json:"kind"`
    Package  string                 `json:"package"`
    File     string                 `json:"file"`
    Line     int                    `json:"line"`
    Metadata map[string]interface{} `json:"metadata"` // ← CONTAINS SOURCE CODE
}

// AFTER — compact, no source code:
type NodeRef struct {
    Name    string `json:"name"`
    Kind    string `json:"kind"`     // function, struct, interface, method
    File    string `json:"file"`     // file path only
    Line    int    `json:"line"`     // start line only
    Cluster string `json:"cluster"`  // which cluster this belongs to
}

// Display format: "StartTestServer [func] test_helpers.go:42 (test-infrastructure)"
func (n NodeRef) String() string {
    return fmt.Sprintf("%s [%s] %s:%d (%s)", n.Name, n.Kind, n.File, n.Line, n.Cluster)
}
```

### Rule 2: Return pre-structured answers, not raw data

### Rule 3: One tool call should give the complete picture

---

## 4. Updated Tool: `get_dependencies`

**BEFORE:** Returns full node objects for all callers and callees. Agent makes follow-up calls.

**AFTER:** Returns compact refs with cluster context and a summary. No follow-up needed.

```go
type GetDependenciesOutput struct {
    // Compact node reference (no source code)
    Node     string   `json:"node"`      // "StartTestServer [func] test_helpers.go:42"
    Cluster  string   `json:"cluster"`   // "test-infrastructure"

    // Compact caller/callee lists (max 15 each)
    Callers  []string `json:"callers"`   // ["TestGetUser [func] user_test.go:15", ...]
    Callees  []string `json:"callees"`   // ["SetupRouter [func] router.go:10", ...]

    // Precomputed context (agent doesn't need to explore)
    CallerCount    int      `json:"caller_count"`
    CalleeCount    int      `json:"callee_count"`
    Flows          []string `json:"flows"`          // ["TestGetUser", "TestCreateOrder"]
    CrossRepo      bool     `json:"cross_repo"`
    
    // Human-readable summary
    Summary  string `json:"summary"`
    // "Test helper function in test-infrastructure cluster. 
    //  Called by 3 test functions. Calls SetupRouter, InitDB, LoadConfig.
    //  Part of 3 test flows. Same repo, low risk."
}
```

**Response size: ~500 bytes** (vs ~180KB before)

---

## 5. Updated Tool: `get_impact_analysis`

**BEFORE:** Agent does BFS manually through multiple tool calls.

**AFTER:** Returns precomputed impact in one call, grouped by depth.

```go
type ImpactAnalysisOutput struct {
    Node        string `json:"node"`          // "StartTestServer [func] test_helpers.go:42"
    RiskLevel   string `json:"risk_level"`    // "low"
    TotalAffected int  `json:"total_affected"` // 3
    CrossRepo   bool   `json:"cross_repo"`    // false
    
    // Grouped by depth — precomputed, not explored
    WillBreak       []string `json:"will_break"`        // depth 1 — direct callers
    LikelyAffected  []string `json:"likely_affected"`   // depth 2
    PossiblyAffected []string `json:"possibly_affected"` // depth 3+
    
    // Which execution flows are affected
    AffectedFlows    []string `json:"affected_flows"`
    AffectedClusters []string `json:"affected_clusters"`
    
    // One-liner summary
    Summary string `json:"summary"`
    // "Low risk. 3 test functions directly depend on this. 
    //  No production code affected. Changes may break integration tests."
}
```

---

## 6. Updated Tool: `search_graph`

**BEFORE:** Returns raw node objects matching a name search.

**AFTER:** Returns compact refs with context about each match.

```go
type SearchGraphOutput struct {
    Results []SearchResult `json:"results"`
    Total   int            `json:"total"`
}

type SearchResult struct {
    Ref       string `json:"ref"`       // "StartTestServer [func] test_helpers.go:42"
    Cluster   string `json:"cluster"`   // "test-infrastructure"
    Callers   int    `json:"callers"`   // 3
    Callees   int    `json:"callees"`   // 3
    FlowCount int    `json:"flow_count"` // 3
    Relevance string `json:"relevance"` // "exact_name_match" or "package_match"
}
```

---

## 7. NEW Tool: `get_context` (inspired by GitNexus)

360-degree view of a symbol — everything you need in one call.

```go
// NEW TOOL — add to MCP server

type GetContextInput struct {
    Name string `json:"name" jsonschema:"required,description=Function or type name"`
}

type GetContextOutput struct {
    // The symbol itself
    Symbol   string `json:"symbol"`    // "StartTestServer [func] test_helpers.go:42"
    Cluster  string `json:"cluster"`   // "test-infrastructure"
    
    // Incoming (who uses this)
    IncomingCalls   []string `json:"incoming_calls"`    // ["TestGetUser", "TestCreateOrder"]
    IncomingImports []string `json:"incoming_imports"`  // ["user_test.go", "order_test.go"]
    
    // Outgoing (what this uses)
    OutgoingCalls   []string `json:"outgoing_calls"`    // ["SetupRouter", "InitDB", "LoadConfig"]
    OutgoingImports []string `json:"outgoing_imports"`  // ["router.go", "db.go", "config.go"]
    
    // Execution flows this participates in
    Flows []FlowContext `json:"flows"`
    
    // Impact summary (precomputed)
    Impact string `json:"impact"`  // "Low risk. 3 test callers, no production impact."
    
    // Cluster neighbors (related functions)
    ClusterNeighbors []string `json:"cluster_neighbors"`  // top 5 most related functions
}

type FlowContext struct {
    FlowName string `json:"flow_name"`   // "TestGetUser"
    StepNum  int    `json:"step_num"`    // 2
    TotalSteps int  `json:"total_steps"` // 7
    Role     string `json:"role"`        // "Step 2 of 7 in TestGetUser flow"
}

// Description for MCP tool registration:
// "Get complete context for any symbol — callers, callees, flows, cluster,
//  and impact assessment — all in one call. This is the primary tool for
//  understanding a function. Use this FIRST before get_dependencies or
//  get_impact_analysis."
```

**This single tool replaces 3-4 calls** that the agent currently makes. It's the "tell me everything about this function" tool.

---

## 8. Updated Tool Descriptions (Guide Agent Behavior)

The tool descriptions in MCP registration are what the agent reads to decide how to use tools. Update them to prevent over-exploration:

```go
// get_context — THE PRIMARY TOOL
mcp.AddTool(server, &mcp.Tool{
    Name: "get_context",
    Description: "Get complete context for a function or type — callers, callees, " +
        "execution flows, cluster membership, and impact assessment. Returns everything " +
        "in one call. USE THIS FIRST for any code understanding question. " +
        "You do NOT need to call get_dependencies or get_impact_analysis separately — " +
        "this tool includes all of that information.",
}, h.HandleGetContext)

// get_dependencies — ONLY for deep exploration
mcp.AddTool(server, &mcp.Tool{
    Name: "get_dependencies",
    Description: "Get callers and callees for a function. Returns compact references " +
        "(name, file, line) — NOT source code. Usually get_context provides enough " +
        "information. Only use this if you need the full caller/callee list beyond " +
        "what get_context shows.",
}, h.HandleGetDependencies)

// get_impact_analysis — ONLY for change planning
mcp.AddTool(server, &mcp.Tool{
    Name: "get_impact_analysis",
    Description: "Analyze blast radius for a planned change. Returns precomputed " +
        "impact grouped by severity: WILL BREAK, LIKELY AFFECTED, POSSIBLY AFFECTED. " +
        "Use ONLY when planning a code change — not for understanding code.",
}, h.HandleGetImpactAnalysis)

// search_graph — for discovery
mcp.AddTool(server, &mcp.Tool{
    Name: "search_graph",
    Description: "Search for functions or types by name. Returns compact matches with " +
        "cluster and connection counts. Use when you don't know the exact name. " +
        "After finding the right symbol, call get_context for full details.",
}, h.HandleSearchGraph)
```

---

## 9. Response Size Limits

Hard limits to prevent token explosion:

```go
const (
    MaxCallersInResponse  = 15  // show top 15, note "and N more"
    MaxCalleesInResponse  = 15
    MaxFlowsInResponse    = 5
    MaxFlowSteps          = 10  // truncate long flows
    MaxSearchResults      = 10
    MaxClusterNeighbors   = 5
    MaxImpactPerDepth     = 10  // show top 10 per depth level
)

// If there are more than the limit, append a note:
// "callers": ["TestGetUser", "TestCreateOrder", ..., "and 32 more"]
```

---

## 10. What to Remove from graph.json

```go
// REMOVE from node data:
// - metadata.content   (source code — NEVER in graph)
// - metadata.imports    (can be derived from edges)
// - metadata.exports    (can be derived from edges)
// - full AST data       (not needed for MCP responses)

// KEEP in node data:
// - id, name, type, file_path, package, start_line, end_line
// - cluster (NEW)
// - caller_count, callee_count (NEW — precomputed counts)
// - flows (NEW — which flows this node participates in)
```

**Estimated graph.json size reduction:**
- Before: 1.35MB (504 nodes with source code in metadata)
- After: ~150KB (504 nodes, compact, plus clusters and flows)
- 90% smaller

---

## 11. Updated `universe init` Output

```bash
$ universe init
🔍 Scanning codebase...
   Path: /path/to/project
   Parsing with tree-sitter... 504 nodes, 2669 edges, 21 files
   Detecting clusters... 8 clusters found                    # ← NEW
   Tracing execution flows... 23 flows detected              # ← NEW
   Precomputing impact chains... 47 high-connectivity nodes   # ← NEW
   Stored: .universe/graph.json (148KB)                       # ← SMALLER
✅ Graph ready!
   Time:  4.1s
   Nodes: 504  Edges: 2669  Packages: 17
   Clusters: 8  Flows: 23  Impact nodes: 47                  # ← NEW
```

---

## 12. Expected Token Improvement

| Scenario | Before (raw graph) | After (precomputed) | Reduction |
|----------|-------------------|-------------------|-----------|
| "What does StartTestServer do?" | 534K tokens, $1.05 | <80K tokens, ~$0.15 | 85% fewer tokens |
| "What depends on AuthService?" | ~400K tokens | <60K tokens | 85% fewer |
| "Impact of changing DB.Connect?" | ~600K tokens (multi-call) | <50K tokens (one call) | 92% fewer |
| Simple code question (no graph) | 121K tokens | 121K tokens | Same (no graph used) |

The graph should ALWAYS reduce tokens, never increase them. If a tool response would add more context than the question warrants, the tool should return a shorter summary.

---

## 13. Implementation Order

1. **Remove `metadata.content` from graph nodes** — immediate token reduction, no new code needed
2. **Compact tool response format** — change output types to use NodeRef instead of full objects
3. **Add response size limits** — cap callers/callees at 15
4. **Add cluster detection to `universe init`** — label propagation algorithm
5. **Add flow detection to `universe init`** — DFS from entry points
6. **Add impact precomputation to `universe init`** — BFS upstream per important node
7. **Add `get_context` tool** — the 360-degree one-call tool
8. **Update tool descriptions** — guide agent to use `get_context` first
9. **Re-test**: ask the same question, compare tokens

---

## 14. Testing — Verify Token Reduction

```bash
# After applying fixes:
# 1. Re-index
universe init

# 2. Connect to Cursor via MCP
# 3. Ask the SAME question on both machines:
#    "Can you tell me what happens in the StartTestServer function?"
#
# 4. Compare token usage:
#    Without graph: ~121K tokens (baseline)
#    With fixed graph: should be <121K tokens (graph REDUCES tokens)
#    If graph version uses MORE tokens → tool responses are still too big
#
# 5. Check tool call count in Cursor:
#    Before fix: 5-8 tool calls per question
#    After fix: 1-2 tool calls per question (get_context covers everything)
```

---

## 15. Acceptance Criteria

- [ ] `metadata.content` removed from all graph nodes
- [ ] `graph.json` is <200KB for the 504-node repo (was 1.35MB)
- [ ] `universe init` reports cluster count and flow count
- [ ] `get_context` tool returns 360-degree view in one call
- [ ] `get_dependencies` returns max 15 callers + 15 callees (not all)
- [ ] `get_impact_analysis` returns precomputed grouped results
- [ ] No tool response contains source code
- [ ] Tool response for any single call is <5KB
- [ ] Same question uses FEWER tokens with graph than without
- [ ] Agent makes 1-2 tool calls per question, not 5-8
