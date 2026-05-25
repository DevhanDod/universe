# Universe MCP Server

## Build Specification for Claude Code

**Name:** MCP server — the glue connecting all 5 engines to Cursor and Claude Code  
**Command:** `universe mcp --stdio`  
**Protocol:** MCP (Model Context Protocol) over stdio (stdin/stdout)  
**SDK:** Official Go SDK — `github.com/modelcontextprotocol/go-sdk/mcp`  
**Estimated effort:** 1-2 days  
**Dependencies:** All 5 engines built and compiling. PostgreSQL running.  

---

## 1. What This Does (Plain English)

When a developer uses Cursor and asks "fix the type mismatch in auth.ValidateToken", Cursor's AI agent needs tools to answer that well. The MCP server is how Cursor discovers and calls those tools.

Cursor launches `universe mcp --stdio` as a subprocess. They talk via stdin/stdout using JSON-RPC 2.0. Universe tells Cursor "I have 10 tools" (get_dependencies, recall_memory, find_skill, etc.). Cursor's agent calls those tools when needed. Universe runs the tool logic (queries the graph, searches memory, finds skills) and returns the result.

The developer doesn't see any of this. They just ask questions in Cursor and get better answers because the agent has access to the knowledge graph, memory, skills, and orchestrator.

---

## 2. SDK Choice

Use the **official MCP Go SDK**: `github.com/modelcontextprotocol/go-sdk/mcp`

This is maintained by the MCP team in collaboration with Google. Version 1.6.0 is the latest, supporting MCP spec 2025-11-25 with backward compatibility. It handles all protocol plumbing — JSON-RPC, session management, transport — so we just define tools and their handler functions.

```bash
# Add to go.mod:
go get github.com/modelcontextprotocol/go-sdk@latest
```

Key SDK patterns we use:

```go
// Create server
server := mcp.NewServer(&mcp.Implementation{Name: "universe", Version: "0.1.0"}, nil)

// Add a tool (typed input/output via generics)
mcp.AddTool(server, &mcp.Tool{Name: "get_dependencies", Description: "..."}, handleGetDependencies)

// Run over stdio
server.Run(context.Background(), &mcp.StdioTransport{})
```

---

## 3. Project Structure

```
internal/
└── mcpserver/
    ├── server.go              # Server setup, tool registration, startup
    ├── tools_graph.go         # Engine 1 tools: get_dependencies, get_impact_analysis
    ├── tools_memory.go        # Engine 2 tools: recall_memory, get_observation_details, store_observation
    ├── tools_skills.go        # Engine 3 tools: find_skill, report_skill_execution, list_skills, get_skill_lineage
    ├── tools_orchestrator.go  # Engine 5 tools: execute_task, get_cost_summary
    ├── context.go             # Session tracking, auto-inject memory at start
    └── mcpserver_test.go      # Tests
```

---

## 4. File: `internal/mcpserver/server.go`

The main entry point. Creates the MCP server, registers all tools, and runs over stdio.

```go
package mcpserver

import (
    "context"
    "fmt"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "universe/internal/compress"
    "universe/internal/graph"
    "universe/internal/memory"
    "universe/internal/orchestrator"
    "universe/internal/skills"
)

// ServerConfig holds all dependencies the MCP server needs.
type ServerConfig struct {
    // Version of the Universe binary
    Version string

    // Database URL for PostgreSQL
    DatabaseURL string

    // Engine instances (initialized before server starts)
    Graph        *graph.Graph           // Engine 1
    MemoryStore  *memory.Store          // Engine 2
    Retriever    *memory.Retriever      // Engine 2
    SessionMgr   *memory.SessionManager // Engine 2
    SkillStore   *skills.Store          // Engine 3
    SkillMatcher *skills.Matcher        // Engine 3
    SkillExec    *skills.Executor       // Engine 3
    Orchestrator *orchestrator.Orchestrator // Engine 5 (optional — can be nil for basic mode)
}

// RunStdio starts the MCP server over stdin/stdout.
// This is called by the CLI: `universe mcp --stdio`
//
// It blocks until the client disconnects (Cursor closes the session).
//
// Implementation:
//   1. Create the MCP server instance
//   2. Register all tools (one per engine function)
//   3. Run over stdio transport
//   4. Block until client disconnects or context is cancelled
func RunStdio(ctx context.Context, config ServerConfig) error {
    // Create server
    server := mcp.NewServer(
        &mcp.Implementation{
            Name:    "universe",
            Version: config.Version,
        },
        nil, // no server options needed for basic setup
    )

    // Create the handler context with all engine references
    h := &Handlers{
        graph:        config.Graph,
        memoryStore:  config.MemoryStore,
        retriever:    config.Retriever,
        sessionMgr:   config.SessionMgr,
        skillStore:   config.SkillStore,
        skillMatcher: config.SkillMatcher,
        skillExec:    config.SkillExec,
        orchestrator: config.Orchestrator,
    }

    // ========================================================
    // Register Engine 1 tools: Knowledge Graph
    // ========================================================
    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_dependencies",
        Description: "Get all functions that call or are called by a given function. Returns the dependency tree from the knowledge graph.",
    }, h.HandleGetDependencies)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_impact_analysis",
        Description: "Analyze what will break if a given function is changed. Returns all affected functions, repos, and the risk level.",
    }, h.HandleGetImpactAnalysis)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "search_graph",
        Description: "Search the knowledge graph by function name, type name, or package name. Returns matching nodes with their connections.",
    }, h.HandleSearchGraph)

    // ========================================================
    // Register Engine 2 tools: Persistent Memory
    // ========================================================
    mcp.AddTool(server, &mcp.Tool{
        Name:        "recall_memory",
        Description: "Search past observations from the memory store. Returns compact summaries. Use get_observation_details to load full details for specific IDs.",
    }, h.HandleRecallMemory)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_observation_details",
        Description: "Get full details for specific observations by their IDs. Use recall_memory first to find relevant IDs, then call this for full detail.",
    }, h.HandleGetObservationDetails)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "store_observation",
        Description: "Manually store an observation — a pattern, decision, convention, or fix that should be remembered across sessions.",
    }, h.HandleStoreObservation)

    // ========================================================
    // Register Engine 3 tools: Self-Evolving Skills
    // ========================================================
    mcp.AddTool(server, &mcp.Tool{
        Name:        "find_skill",
        Description: "Search for a matching skill recipe for the current task. If found, follow the skill instruction instead of reasoning from scratch — it saves tokens and time.",
    }, h.HandleFindSkill)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "report_skill_execution",
        Description: "Report whether a skill application succeeded or failed. This feedback is used to evolve and improve skills over time.",
    }, h.HandleReportSkillExecution)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "list_skills",
        Description: "List all active skills, optionally filtered by language or graph node. Shows skill name, version, success rate, and confidence.",
    }, h.HandleListSkills)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_skill_lineage",
        Description: "Get the full evolution history of a skill — all versions from first capture to current, including derived variants.",
    }, h.HandleGetSkillLineage)

    // ========================================================
    // Register Engine 5 tools: Orchestrator
    // ========================================================
    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_cost_summary",
        Description: "Get cost and savings summary — actual cost vs what it would have been, routing breakdown, and trend over time.",
    }, h.HandleGetCostSummary)

    // Run over stdio — blocks until client disconnects
    fmt.Fprintf(os.Stderr, "Universe MCP server starting (stdio)...\n")
    return server.Run(ctx, &mcp.StdioTransport{})
}

// Handlers holds references to all engine instances.
// Each tool handler method is defined in its respective file.
type Handlers struct {
    graph        *graph.Graph
    memoryStore  *memory.Store
    retriever    *memory.Retriever
    sessionMgr   *memory.SessionManager
    skillStore   *skills.Store
    skillMatcher *skills.Matcher
    skillExec    *skills.Executor
    orchestrator *orchestrator.Orchestrator
}
```

---

## 5. File: `internal/mcpserver/tools_graph.go`

Engine 1 tool handlers — knowledge graph queries.

```go
package mcpserver

import (
    "context"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================
// Tool: get_dependencies
// ============================================================

// GetDependenciesInput is the typed input for the tool.
// The SDK auto-generates the JSON schema from struct tags.
type GetDependenciesInput struct {
    // The function or type name to look up (e.g., "ValidateToken" or "auth.ValidateToken")
    Name string `json:"name" jsonschema:"required,description=Function or type name to look up"`

    // How many levels of dependencies to traverse (default: 1)
    Depth int `json:"depth,omitempty" jsonschema:"description=How many levels deep to traverse (default 1)"`
}

// GetDependenciesOutput is the typed output.
type GetDependenciesOutput struct {
    Node     NodeInfo   `json:"node"`
    Callers  []NodeInfo `json:"callers"`
    Callees  []NodeInfo `json:"callees"`
    Message  string     `json:"message,omitempty"`
}

type NodeInfo struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Kind    string `json:"kind"`    // "function", "struct", "interface", "method"
    Package string `json:"package"`
    Repo    string `json:"repo"`
    File    string `json:"file"`
    Line    int    `json:"line"`
}

// HandleGetDependencies is the tool handler.
//
// Implementation:
//   1. Search the graph for a node matching input.Name
//      - Try exact match first (package.Name format)
//      - Then try name-only match
//      - If multiple matches, return the most specific one
//   2. If no match found, return a helpful error with suggestions
//   3. Get callers: traverse incoming edges (who calls this function)
//   4. Get callees: traverse outgoing edges (what does this function call)
//   5. If input.Depth > 1, recursively get dependencies of callers/callees
//   6. Return the node info + callers + callees
//
// Also: notify the session manager (Engine 2) that this graph node was accessed.
// This helps memory auto-injection at session start.
func (h *Handlers) HandleGetDependencies(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetDependenciesInput,
) (*mcp.CallToolResult, GetDependenciesOutput, error) {

    if input.Depth == 0 {
        input.Depth = 1
    }

    // Search graph for the node
    // Adapt this to your graph.Graph API — the function names below
    // are placeholders matching your existing graph.go methods.
    node := h.graph.FindNode(input.Name)
    if node == nil {
        return nil, GetDependenciesOutput{
            Message: "No function or type found matching '" + input.Name + "'. Try a different name or use search_graph to browse.",
        }, nil
    }

    // Get callers and callees from the graph
    callers := h.graph.GetCallers(node.ID)
    callees := h.graph.GetCallees(node.ID)

    // Convert to NodeInfo format
    output := GetDependenciesOutput{
        Node:    toNodeInfo(node),
        Callers: toNodeInfoList(callers),
        Callees: toNodeInfoList(callees),
    }

    // Track this access for session context (Engine 2)
    if h.sessionMgr != nil {
        h.sessionMgr.OnToolCall("", "get_dependencies", node.ID, input.Name, "", true, "")
    }

    return nil, output, nil
}

// ============================================================
// Tool: get_impact_analysis
// ============================================================

type ImpactAnalysisInput struct {
    // Graph node ID or function name to analyze
    Name string `json:"name" jsonschema:"required,description=Function or type name to analyze impact for"`
}

type ImpactAnalysisOutput struct {
    RootNode      NodeInfo       `json:"root_node"`
    AffectedNodes []AffectedNode `json:"affected_nodes"`
    CrossRepo     bool           `json:"cross_repo"`
    RiskLevel     string         `json:"risk_level"` // "low", "medium", "high"
    Summary       string         `json:"summary"`
}

type AffectedNode struct {
    NodeInfo
    Impact   string `json:"impact"`   // "direct", "indirect"
    Distance int    `json:"distance"` // hops from root
}

// HandleGetImpactAnalysis traverses the graph to find everything
// affected by a change to the given function.
//
// Implementation:
//   1. Find the node in the graph
//   2. BFS traversal: follow caller edges outward (who calls this, who calls them, etc.)
//   3. Mark direct callers (distance 1) as "direct" impact
//   4. Mark callers-of-callers (distance 2+) as "indirect" impact
//   5. Check if any affected nodes are in different repos → cross_repo = true
//   6. Calculate risk level:
//      - 1-2 affected nodes, same repo → "low"
//      - 3-5 affected nodes OR cross-repo → "medium"
//      - 6+ affected nodes AND cross-repo → "high"
//   7. Generate summary: "Changing X directly affects N functions in M repos"
func (h *Handlers) HandleGetImpactAnalysis(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input ImpactAnalysisInput,
) (*mcp.CallToolResult, ImpactAnalysisOutput, error) {
    // Implementation follows the steps above
    // ...
    return nil, ImpactAnalysisOutput{}, nil
}

// ============================================================
// Tool: search_graph
// ============================================================

type SearchGraphInput struct {
    Query string `json:"query" jsonschema:"required,description=Search term — function name or type name or package name"`
    Limit int    `json:"limit,omitempty" jsonschema:"description=Max results (default 10)"`
}

type SearchGraphOutput struct {
    Results []NodeInfo `json:"results"`
    Total   int        `json:"total"`
}

// HandleSearchGraph searches graph nodes by name.
//
// Implementation:
//   1. Search all nodes where Name, Package, or File contains the query (case-insensitive)
//   2. Rank by relevance: exact name match > prefix match > contains match
//   3. Return top N results
func (h *Handlers) HandleSearchGraph(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input SearchGraphInput,
) (*mcp.CallToolResult, SearchGraphOutput, error) {
    // Implementation
    // ...
    return nil, SearchGraphOutput{}, nil
}

// ============================================================
// HELPERS
// ============================================================

// toNodeInfo converts your graph's internal node type to the MCP output format.
// Adapt this to match your actual models.Node fields.
func toNodeInfo(node *graph.Node) NodeInfo {
    return NodeInfo{
        ID:      node.ID,
        Name:    node.Name,
        Kind:    node.Kind,
        Package: node.Package,
        Repo:    node.Repo,
        File:    node.File,
        Line:    node.Line,
    }
}

func toNodeInfoList(nodes []*graph.Node) []NodeInfo {
    result := make([]NodeInfo, len(nodes))
    for i, n := range nodes {
        result[i] = toNodeInfo(n)
    }
    return result
}
```

---

## 6. File: `internal/mcpserver/tools_memory.go`

Engine 2 tool handlers — persistent memory.

```go
package mcpserver

import (
    "context"
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "universe/internal/memory"
)

// ============================================================
// Tool: recall_memory
// ============================================================

type RecallMemoryInput struct {
    Query        string   `json:"query,omitempty" jsonschema:"description=Text to search for (keyword + semantic). Optional if graph_node_ids provided."`
    GraphNodeIDs []string `json:"graph_node_ids,omitempty" jsonschema:"description=Graph node IDs to search by. Also searches callers and callees."`
    Categories   []string `json:"categories,omitempty" jsonschema:"description=Filter by category: fix pattern decision failure convention"`
    Limit        int      `json:"limit,omitempty" jsonschema:"description=Max results (default 10)"`
}

type RecallMemoryOutput struct {
    Observations []ObservationSummaryOut `json:"observations"`
    TotalCount   int                     `json:"total_count"`
    SearchMethod string                  `json:"search_method"`
}

type ObservationSummaryOut struct {
    ID          string  `json:"id"`
    GraphNodeID string  `json:"graph_node_id"`
    Category    string  `json:"category"`
    Summary     string  `json:"summary"`
    Confidence  float64 `json:"confidence"`
    CreatedAt   string  `json:"created_at"`
    Score       float64 `json:"score"`
}

// HandleRecallMemory searches memory using Engine 2's 3-way hybrid search.
//
// Implementation:
//   1. Build a memory.SearchQuery from the input
//   2. Call retriever.Search(query)
//   3. Convert results to output format
//   4. Track tool call via session manager
func (h *Handlers) HandleRecallMemory(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input RecallMemoryInput,
) (*mcp.CallToolResult, RecallMemoryOutput, error) {

    limit := input.Limit
    if limit == 0 {
        limit = 10
    }

    query := memory.SearchQuery{
        Text:                  input.Query,
        GraphNodeIDs:          input.GraphNodeIDs,
        Categories:            input.Categories,
        Limit:                 limit,
        IncludeGraphNeighbors: true,
        // DeveloperID is set from session context
    }

    result, err := h.retriever.Search(query)
    if err != nil {
        return nil, RecallMemoryOutput{}, err
    }

    // Convert to output
    obs := make([]ObservationSummaryOut, len(result.Summaries))
    for i, s := range result.Summaries {
        obs[i] = ObservationSummaryOut{
            ID:          s.ID,
            GraphNodeID: s.GraphNodeID,
            Category:    s.Category,
            Summary:     s.Summary,
            Confidence:  s.Confidence,
            CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
            Score:       s.Score,
        }
    }

    return nil, RecallMemoryOutput{
        Observations: obs,
        TotalCount:   result.TotalCount,
        SearchMethod: result.SearchMethod,
    }, nil
}

// ============================================================
// Tool: get_observation_details
// ============================================================

type GetObservationDetailsInput struct {
    IDs []string `json:"ids" jsonschema:"required,description=List of observation UUIDs to retrieve full details for"`
}

type GetObservationDetailsOutput struct {
    Observations []ObservationDetailOut `json:"observations"`
}

type ObservationDetailOut struct {
    ID          string `json:"id"`
    GraphNodeID string `json:"graph_node_id"`
    Category    string `json:"category"`
    Summary     string `json:"summary"`
    Detail      string `json:"detail"`
    DeveloperID string `json:"developer_id"`
    RepoID      string `json:"repo_id"`
    CreatedAt   string `json:"created_at"`
}

// HandleGetObservationDetails retrieves full observation details.
// Layer 3 of progressive disclosure.
func (h *Handlers) HandleGetObservationDetails(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetObservationDetailsInput,
) (*mcp.CallToolResult, GetObservationDetailsOutput, error) {

    observations, err := h.retriever.GetFullObservations(input.IDs)
    if err != nil {
        return nil, GetObservationDetailsOutput{}, err
    }

    out := make([]ObservationDetailOut, len(observations))
    for i, o := range observations {
        out[i] = ObservationDetailOut{
            ID:          o.ID,
            GraphNodeID: o.GraphNodeID,
            Category:    o.Category,
            Summary:     o.Summary,
            Detail:      o.Detail,
            DeveloperID: o.DeveloperID,
            RepoID:      o.RepoID,
            CreatedAt:   o.CreatedAt.Format("2006-01-02T15:04:05Z"),
        }
    }

    return nil, GetObservationDetailsOutput{Observations: out}, nil
}

// ============================================================
// Tool: store_observation
// ============================================================

type StoreObservationInput struct {
    GraphNodeID string `json:"graph_node_id" jsonschema:"required,description=The graph node this observation relates to"`
    Category    string `json:"category" jsonschema:"required,description=Category: fix pattern decision failure convention"`
    Content     string `json:"content" jsonschema:"required,description=The observation text. Will be AI-compressed into a summary."`
    Shared      bool   `json:"shared,omitempty" jsonschema:"description=Make visible to the whole team (default false)"`
}

type StoreObservationOutput struct {
    ID      string `json:"id"`
    Summary string `json:"summary"`
    Message string `json:"message"`
}

// HandleStoreObservation stores a manually created observation.
//
// Implementation:
//   1. Compress the content using Engine 2's compressor (calls Haiku)
//   2. Generate embedding for the compressed summary
//   3. Insert into the observations table
//   4. Return the generated ID and compressed summary
func (h *Handlers) HandleStoreObservation(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input StoreObservationInput,
) (*mcp.CallToolResult, StoreObservationOutput, error) {
    // Implementation
    // ...
    return nil, StoreObservationOutput{
        Message: "Observation stored and will be recalled in future sessions.",
    }, nil
}
```

---

## 7. File: `internal/mcpserver/tools_skills.go`

Engine 3 tool handlers — self-evolving skills.

```go
package mcpserver

import (
    "context"
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "universe/internal/skills"
)

// ============================================================
// Tool: find_skill
// ============================================================

type FindSkillInput struct {
    TaskText     string   `json:"task_text" jsonschema:"required,description=Description of the task to find a skill for"`
    GraphNodeIDs []string `json:"graph_node_ids,omitempty" jsonschema:"description=Graph node IDs from the current context"`
    Language     string   `json:"language,omitempty" jsonschema:"description=Programming language of the current repo (go python typescript)"`
}

type FindSkillOutput struct {
    Found              bool   `json:"found"`
    SkillID            string `json:"skill_id,omitempty"`
    SkillName          string `json:"skill_name,omitempty"`
    Version            int    `json:"version,omitempty"`
    Instruction        string `json:"instruction,omitempty"`
    SuccessRate        float64 `json:"success_rate,omitempty"`
    Confidence         float64 `json:"confidence,omitempty"`
    ExplorationSkipped bool   `json:"exploration_skipped"`
    Message            string `json:"message"`
}

// HandleFindSkill searches for a matching skill recipe.
//
// Implementation:
//   1. Build a skills.MatchQuery from the input
//   2. Call skillMatcher.Match(query)
//   3. If ExplorationTriggered: return with message "Exploration mode — reasoning from scratch"
//   4. If BestMatch found: return the skill ID, name, instruction, and stats
//   5. If no match: return with message "No matching skill. Reason from scratch."
//
// IMPORTANT: When a skill is found, the instruction text is returned.
// The agent should follow this instruction as its approach to the task.
func (h *Handlers) HandleFindSkill(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input FindSkillInput,
) (*mcp.CallToolResult, FindSkillOutput, error) {

    query := skills.MatchQuery{
        TaskText:     input.TaskText,
        GraphNodeIDs: input.GraphNodeIDs,
        Language:     input.Language,
        Limit:        3,
    }

    result, err := h.skillMatcher.Match(query)
    if err != nil {
        return nil, FindSkillOutput{}, err
    }

    if result.ExplorationTriggered {
        return nil, FindSkillOutput{
            Found:              false,
            ExplorationSkipped: true,
            Message:            "Exploration mode activated (10% chance). Reason from scratch — if your approach works well, it may be captured as a new skill.",
        }, nil
    }

    if result.BestMatch == nil {
        return nil, FindSkillOutput{
            Found:   false,
            Message: "No matching skill found. Reason from scratch. If the task succeeds, the approach may be captured as a new skill.",
        }, nil
    }

    // Get full skill with instruction
    skill, err := h.skillStore.GetByID(result.BestMatch.ID)
    if err != nil {
        return nil, FindSkillOutput{}, err
    }

    return nil, FindSkillOutput{
        Found:       true,
        SkillID:     skill.ID,
        SkillName:   skill.Name,
        Version:     skill.Version,
        Instruction: skill.Instruction,
        SuccessRate: result.BestMatch.SuccessRate,
        Confidence:  result.BestMatch.Confidence,
        Message:     "Skill found. Follow the instruction below for this task.",
    }, nil
}

// ============================================================
// Tool: report_skill_execution
// ============================================================

type ReportSkillExecutionInput struct {
    SkillID     string `json:"skill_id" jsonschema:"required,description=The skill that was applied"`
    Success     bool   `json:"success" jsonschema:"required,description=Whether the skill application succeeded"`
    ErrorDetail string `json:"error_detail,omitempty" jsonschema:"description=Error message if the skill failed"`
    TokensUsed  int    `json:"tokens_used,omitempty" jsonschema:"description=Tokens consumed during execution"`
}

type ReportSkillExecutionOutput struct {
    Message string `json:"message"`
}

// HandleReportSkillExecution logs a skill execution result.
// Triggers quality monitoring and potential skill evolution.
//
// Implementation:
//   1. Create a SkillExecution record
//   2. Call skillExec.RecordExecution(skillID, execution)
//      This updates metrics and may trigger FIX evolution if the skill failed
//   3. Return confirmation message
func (h *Handlers) HandleReportSkillExecution(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input ReportSkillExecutionInput,
) (*mcp.CallToolResult, ReportSkillExecutionOutput, error) {

    exec := skills.SkillExecution{
        SkillID:     input.SkillID,
        Success:     input.Success,
        ErrorDetail: input.ErrorDetail,
        TokensUsed:  input.TokensUsed,
    }

    if err := h.skillExec.RecordExecution(input.SkillID, exec); err != nil {
        return nil, ReportSkillExecutionOutput{}, err
    }

    msg := "Skill execution recorded. "
    if input.Success {
        msg += "Success logged — skill confidence increased."
    } else {
        msg += "Failure logged — will be reviewed for potential skill improvement."
    }

    return nil, ReportSkillExecutionOutput{Message: msg}, nil
}

// ============================================================
// Tool: list_skills
// ============================================================

type ListSkillsInput struct {
    GraphNodeID string `json:"graph_node_id,omitempty" jsonschema:"description=Filter by skills covering this graph node"`
    Language    string `json:"language,omitempty" jsonschema:"description=Filter by language"`
}

type ListSkillsOutput struct {
    Skills []SkillSummaryOut `json:"skills"`
    Total  int               `json:"total"`
}

type SkillSummaryOut struct {
    ID          string  `json:"id"`
    Name        string  `json:"name"`
    Version     int     `json:"version"`
    Evolution   string  `json:"evolution"`
    Language    string  `json:"language"`
    TriggerDesc string  `json:"trigger_desc"`
    SuccessRate float64 `json:"success_rate"`
    Confidence  float64 `json:"confidence"`
    Applied     int     `json:"times_applied"`
    IsFrozen    bool    `json:"is_frozen"`
}

func (h *Handlers) HandleListSkills(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input ListSkillsInput,
) (*mcp.CallToolResult, ListSkillsOutput, error) {
    // Query skills from store with filters
    // Convert to output format
    // ...
    return nil, ListSkillsOutput{}, nil
}

// ============================================================
// Tool: get_skill_lineage
// ============================================================

type GetSkillLineageInput struct {
    SkillID string `json:"skill_id" jsonschema:"required,description=Any version ID — the full evolution history is returned"`
}

type GetSkillLineageOutput struct {
    Lineage []SkillVersionOut `json:"lineage"`
    Derived []SkillVersionOut `json:"derived"`
}

type SkillVersionOut struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Version   int    `json:"version"`
    Evolution string `json:"evolution"`
    ParentID  string `json:"parent_id,omitempty"`
    CreatedBy string `json:"created_by"`
    CreatedAt string `json:"created_at"`
}

func (h *Handlers) HandleGetSkillLineage(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetSkillLineageInput,
) (*mcp.CallToolResult, GetSkillLineageOutput, error) {
    // Call skillStore.GetLineage(input.SkillID)
    // Convert to output format
    // ...
    return nil, GetSkillLineageOutput{}, nil
}
```

---

## 8. File: `internal/mcpserver/tools_orchestrator.go`

Engine 5 tool handler — cost tracking.

```go
package mcpserver

import (
    "context"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================
// Tool: get_cost_summary
// ============================================================

type GetCostSummaryInput struct {
    Period      string `json:"period,omitempty" jsonschema:"description=Time period: week month all (default month)"`
    DeveloperID string `json:"developer_id,omitempty" jsonschema:"description=Filter by developer. Empty for team-wide."`
}

type GetCostSummaryOutput struct {
    ActualCost      float64            `json:"actual_cost"`
    WouldHaveCost   float64            `json:"would_have_cost"`
    SavingsUSD      float64            `json:"savings_usd"`
    SavingsPercent  float64            `json:"savings_percent"`
    TotalTasks      int                `json:"total_tasks"`
    HaikuPercent    float64            `json:"haiku_percent"`
    SkillUses       int                `json:"skill_uses"`
    MemoryHits      int                `json:"memory_hits"`
    Takeovers       int                `json:"takeovers"`
    RoutingBreakdown map[string]int    `json:"routing_breakdown"`
}

// HandleGetCostSummary returns cost savings data from Engine 5's tracker.
func (h *Handlers) HandleGetCostSummary(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetCostSummaryInput,
) (*mcp.CallToolResult, GetCostSummaryOutput, error) {
    // Query from orchestrator.Tracker or directly from materialized views
    // ...
    return nil, GetCostSummaryOutput{}, nil
}
```

---

## 9. File: `internal/mcpserver/context.go`

Session tracking — auto-injects memory when a new session starts.

```go
package mcpserver

// SessionContext manages per-session state.
// When Cursor connects, a new session starts.
// On first tool call, we auto-inject relevant memory based on the
// developer's current context (graph nodes from their open file).

// InitializeSessionContext is called when the MCP session starts.
// It queries Engine 2 for relevant observations based on the
// developer's recent activity and injects them as context.
//
// In the current SDK, this is handled by the first tool call
// detecting there's no active session, then calling
// retriever.GetSessionContext() to pre-load relevant memory.
//
// The session manager (Engine 2) tracks tool calls automatically
// via the OnToolCall hook embedded in each tool handler.
```

---

## 10. CLI Integration: `cmd/universe/mcp.go`

Wire the `universe mcp --stdio` command into the cobra CLI.

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"

    "github.com/spf13/cobra"

    "universe/internal/graph"
    "universe/internal/mcpserver"
    "universe/internal/memory"
    "universe/internal/skills"
)

var mcpCmd = &cobra.Command{
    Use:   "mcp",
    Short: "Run the MCP server for AI agent integration",
    Long:  "Starts the Universe MCP server. Connect from Cursor or Claude Code.",
    Run:   runMCP,
}

func init() {
    mcpCmd.Flags().Bool("stdio", true, "Use stdio transport (stdin/stdout)")
    rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) {
    // Get database URL from config or environment
    dbURL := getDBURL()

    // Initialize all engines
    // Engine 1: Load graph from the .universe directory or PostgreSQL
    g, err := graph.Load(".universe/graph.db")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error loading graph: %v\n", err)
        fmt.Fprintf(os.Stderr, "Run 'universe init' first to scan your codebase.\n")
        os.Exit(1)
    }

    // Engine 2: Memory (only if database is configured)
    var memStore *memory.Store
    var retriever *memory.Retriever
    var sessionMgr *memory.SessionManager

    if dbURL != "" {
        memStore, err = memory.NewStore(dbURL)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Warning: memory engine unavailable: %v\n", err)
        } else {
            // Set up retriever with embedder and graph querier
            // embedder and graphQuerier would be initialized here
            retriever = memory.NewRetriever(memStore, embedder, graphQuerier, memory.DefaultConfig())
            sessionMgr = memory.NewSessionManager(memStore, compressor, embedder, memory.DefaultConfig())
        }
    }

    // Engine 3: Skills (only if database is configured)
    var skillStore *skills.Store
    var skillMatcher *skills.Matcher
    var skillExec *skills.Executor

    if dbURL != "" {
        skillStore, err = skills.NewStore(dbURL)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Warning: skills engine unavailable: %v\n", err)
        } else {
            skillMatcher = skills.NewMatcher(skillStore, embedder, skills.DefaultConfig())
            skillExec = skills.NewExecutor(skillStore, skills.DefaultConfig())
        }
    }

    // Build server config
    config := mcpserver.ServerConfig{
        Version:      Version,
        DatabaseURL:  dbURL,
        Graph:        g,
        MemoryStore:  memStore,
        Retriever:    retriever,
        SessionMgr:   sessionMgr,
        SkillStore:   skillStore,
        SkillMatcher: skillMatcher,
        SkillExec:    skillExec,
    }

    // Handle graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    // Start MCP server over stdio
    if err := mcpserver.RunStdio(ctx, config); err != nil {
        fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
        os.Exit(1)
    }
}
```

---

## 11. Cursor Configuration

After building, developers add this to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "universe": {
      "command": "universe",
      "args": ["mcp", "--stdio"],
      "env": {
        "UNIVERSE_DB_URL": "postgres://universe_admin:universe_secret_2024@localhost:5432/universe"
      }
    }
  }
}
```

Cursor launches `universe mcp --stdio` as a subprocess on startup. The MCP handshake happens automatically. Cursor discovers all 10 tools and makes them available to its AI agent.

**For Claude Code**, same config at `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "universe": {
      "command": "universe",
      "args": ["mcp", "--stdio"],
      "env": {
        "UNIVERSE_DB_URL": "postgres://universe_admin:universe_secret_2024@localhost:5432/universe"
      }
    }
  }
}
```

---

## 12. Graceful Degradation

The MCP server works even if some engines are unavailable. If there's no database configured, Engine 2 (memory) and Engine 3 (skills) tools return helpful messages instead of errors:

```go
// In each handler, check if the engine is available:
func (h *Handlers) HandleRecallMemory(...) (...) {
    if h.retriever == nil {
        return nil, RecallMemoryOutput{
            Message: "Memory engine not available. Connect a database: universe config set db postgres://...",
        }, nil
    }
    // ... normal handler logic
}
```

**Degradation hierarchy:**
- No database → Engine 1 (graph) works locally, Engines 2-5 return "connect a database" messages
- Database connected but empty → All engines work but return empty results, memory and skills grow as the developer uses it
- Full setup → All engines fully operational

---

## 13. Go Module Dependencies

```bash
# Add the official MCP SDK
go get github.com/modelcontextprotocol/go-sdk@latest

# Cobra for CLI (if not already added)
go get github.com/spf13/cobra@latest

# Already in go.mod from Engine 2/3/5:
# github.com/jackc/pgx/v5
# github.com/pgvector/pgvector-go
# github.com/google/uuid
```

---

## 14. Testing

```go
package mcpserver

import "testing"

// Test 1: Server creates successfully with all engines
func TestNewServer_AllEngines(t *testing.T) {
    // Create mock engines, build config, verify server starts without error
}

// Test 2: Server creates with graph only (no database)
func TestNewServer_GraphOnly(t *testing.T) {
    // Config with only graph, nil for everything else
    // Verify server starts (graceful degradation)
}

// Test 3: get_dependencies returns correct callers/callees
func TestHandleGetDependencies(t *testing.T) {
    // Mock graph with known nodes and edges
    // Call handler, verify output matches
}

// Test 4: get_dependencies handles missing node gracefully
func TestHandleGetDependencies_NotFound(t *testing.T) {
    // Query for non-existent node
    // Verify helpful error message, not a crash
}

// Test 5: get_impact_analysis calculates risk level correctly
func TestHandleGetImpactAnalysis_RiskLevels(t *testing.T) {
    // 1-2 nodes same repo → low
    // 3-5 nodes OR cross-repo → medium
    // 6+ nodes AND cross-repo → high
}

// Test 6: recall_memory delegates to retriever
func TestHandleRecallMemory(t *testing.T) {
    // Mock retriever returns known results
    // Verify output format
}

// Test 7: recall_memory returns helpful message when engine unavailable
func TestHandleRecallMemory_NoEngine(t *testing.T) {
    // Handlers with nil retriever
    // Verify message says "connect a database"
}

// Test 8: find_skill returns instruction when match found
func TestHandleFindSkill_Match(t *testing.T) {
    // Mock matcher returns a skill
    // Verify output includes instruction text
}

// Test 9: find_skill handles exploration mode
func TestHandleFindSkill_Exploration(t *testing.T) {
    // Mock matcher with exploration triggered
    // Verify ExplorationSkipped = true
}

// Test 10: report_skill_execution updates metrics
func TestHandleReportSkillExecution(t *testing.T) {
    // Report success → verify message
    // Report failure → verify message mentions review
}

// Test 11: store_observation creates observation
func TestHandleStoreObservation(t *testing.T) {
    // Store an observation → verify ID returned
}

// Test 12: Tools registered with correct names
func TestToolRegistration(t *testing.T) {
    // Create server, verify all 10 tools are registered
    // Tool names must match exactly what Cursor expects
}
```

---

## 15. Acceptance Criteria

- [ ] `universe mcp --stdio` starts without error
- [ ] MCP handshake completes (Cursor discovers all tools)
- [ ] `get_dependencies` returns callers and callees from the graph
- [ ] `get_impact_analysis` returns affected nodes with risk level
- [ ] `search_graph` finds nodes by name
- [ ] `recall_memory` returns observations from Engine 2
- [ ] `get_observation_details` returns full observation detail
- [ ] `store_observation` creates a new observation
- [ ] `find_skill` returns matching skill with instruction text
- [ ] `find_skill` handles exploration mode (10% skip)
- [ ] `report_skill_execution` logs success/failure and triggers monitoring
- [ ] `list_skills` returns filtered skill list
- [ ] `get_skill_lineage` returns the version DAG
- [ ] `get_cost_summary` returns savings data
- [ ] Graceful degradation: graph-only mode works without database
- [ ] Session manager tracks tool calls for memory capture
- [ ] Cursor successfully connects and calls tools
- [ ] All 12 tests pass
- [ ] `go build ./...` succeeds

---

## 16. What NOT to Build

- Do NOT implement HTTP/SSE transport — stdio is all Cursor needs for now
- Do NOT add authentication — stdio runs as a local subprocess, already sandboxed
- Do NOT implement MCP resources or prompts — tools are sufficient for V1
- Do NOT build the orchestrator's execute_task tool yet — let the agent call individual tools, add orchestration later
- Do NOT add streaming — the SDK handles stdio streaming internally

---

## 17. Future Improvements

1. **HTTP transport** — `universe mcp --http --port 8080` for team server mode where multiple developers connect
2. **MCP Resources** — expose the graph as a browsable MCP resource (not just tools)
3. **MCP Prompts** — pre-built prompt templates for common tasks ("analyze impact", "find and fix")
4. **Orchestrator tool** — `execute_task` that runs the full Engine 5 pipeline through MCP
5. **Session persistence** — save/restore session state across Cursor restarts
6. **Tool analytics** — track which tools are called most and optimize
