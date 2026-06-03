package mcpserver

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/memory"
	"github.com/Universe/universe/internal/orchestrator"
	"github.com/Universe/universe/internal/skills"
)

// ServerConfig holds all engine dependencies the MCP server needs.
type ServerConfig struct {
	Version     string
	DatabaseURL string

	// Engine 1: Knowledge Graph
	Graph *graph.Graph

	// Engine 2: Personal Memory
	MemoryStore *memory.Store
	Retriever   *memory.Retriever
	SessionMgr  *memory.SessionManager

	// Engine 3: Skills
	SkillStore   *skills.Store
	SkillMatcher *skills.Matcher
	SkillExec    *skills.Executor

	// Engine 5: Plan Bridge + Cost Tracking
	PlanStore          *orchestrator.PlanStore // plan bridge
	Router             *orchestrator.Router    // recommendation engine
	Tracker            *orchestrator.Tracker   // cost tracking
	OrchestratorConfig *orchestrator.Config    // workspace paths and model config
}

// RunStdio starts the MCP server over stdin/stdout and blocks until
// the client disconnects or ctx is cancelled.
func RunStdio(ctx context.Context, config ServerConfig) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "universe", Version: config.Version},
		nil,
	)

	h := &Handlers{
		graph:        config.Graph,
		memoryStore:  config.MemoryStore,
		retriever:    config.Retriever,
		sessionMgr:   config.SessionMgr,
		skillStore:   config.SkillStore,
		skillMatcher: config.SkillMatcher,
		skillExec:    config.SkillExec,
		planStore:    config.PlanStore,
		router:       config.Router,
		tracker:      config.Tracker,
		orchConfig:   config.OrchestratorConfig,
	}

	// Engine 1 — Knowledge Graph
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_context",
		Description: "PRIMARY tool for code understanding. Returns a 360° view of a " +
			"function or type in ONE call: callers, callees, execution flows, cluster, " +
			"and impact summary. Use this FIRST for any \"what does X do / who uses X\" " +
			"question — you do NOT need to call get_dependencies or get_impact_analysis " +
			"after this. Compact refs only, no source code.",
	}, h.HandleGetContext)

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_dependencies",
		Description: "Get compact caller/callee refs (name + file:line) for a function. " +
			"Usually get_context covers this — only call this if you need the full list " +
			"beyond what get_context returns. Lists are capped at 15 per side.",
	}, h.HandleGetDependencies)

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_impact_analysis",
		Description: "Precomputed blast radius for a planned change: WILL BREAK (direct " +
			"callers), LIKELY AFFECTED (depth 2), POSSIBLY AFFECTED (depth 3). Use ONLY " +
			"when planning a change, not for understanding code.",
	}, h.HandleGetImpactAnalysis)

	mcp.AddTool(server, &mcp.Tool{
		Name: "search_graph",
		Description: "Find functions or types by name when you don't know the exact symbol. " +
			"Returns compact refs with cluster and connection counts. After picking the " +
			"right match, call get_context for full details.",
	}, h.HandleSearchGraph)

	// Engine 2 — Persistent Memory
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recall_memory",
		Description: "Search YOUR past observations from previous sessions. Returns compact summaries. Use get_observation_details to load full details for specific IDs. This is your personal memory — only your own past sessions are searched.",
	}, h.HandleRecallMemory)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_observation_details",
		Description: "Get full details for specific observations by their IDs. Use recall_memory first to find relevant IDs, then call this for full detail.",
	}, h.HandleGetObservationDetails)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "store_observation",
		Description: "Store an observation from your current session — a pattern, decision, fix, or convention. Saves to YOUR personal memory and will be recalled in your future sessions when you work on the same code.",
	}, h.HandleStoreObservation)

	// Engine 3 — Self-Evolving Skills
	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_skill",
		Description: "Search for a matching skill recipe. If found, the PLANNING agent (premium model) must verify the skill is still correct before the EXECUTION agent (low-cost model) uses it. Skills are reference knowledge, not auto-applied recipes.",
	}, h.HandleFindSkill)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_skill_execution",
		Description: "Report whether a skill application succeeded or failed. This feedback evolves and improves skills over time.",
	}, h.HandleReportSkillExecution)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_skills",
		Description: "List all active skills, optionally filtered by language or graph node. Shows skill name, version, success rate, and confidence.",
	}, h.HandleListSkills)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_skill_lineage",
		Description: "Get the full evolution history of a skill — all versions from first capture to current.",
	}, h.HandleGetSkillLineage)

	// Engine 5 — Plan bridge
	mcp.AddTool(server, &mcp.Tool{
		Name:        "store_plan",
		Description: "Store a step-by-step plan created by the PLANNING agent. The executor agent will retrieve it with get_plan. Called after analyzing the task, checking skills, and recalling memory.",
	}, h.HandleStorePlan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_plan",
		Description: "Retrieve the latest pending plan for execution. Called by the EXECUTION agent. Automatically marks the plan as executing. Pass plan_id to retrieve a specific plan.",
	}, h.HandleGetPlan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "store_plan_result",
		Description: "Store the result of executing a plan. Called by the EXECUTION agent after completing all plan steps. Marks the plan as completed or failed.",
	}, h.HandleStorePlanResult)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_plan_result",
		Description: "Retrieve the executor's result for a plan. Called by the PLANNING agent to verify the work. Returns the result summary, files changed, and test status.",
	}, h.HandleGetPlanResult)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "verify_plan",
		Description: "Approve or reject the executor's result. Called by the PLANNING agent after reviewing the work. Marks the plan as verified or rejected.",
	}, h.HandleVerifyPlan)

	// Engine 5 — Cost tracking
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_cost_summary",
		Description: "Get cost and savings summary — actual cost vs what it would have been, routing breakdown, and trend over time.",
	}, h.HandleGetCostSummary)

	fmt.Fprintf(os.Stderr, "Universe MCP server starting (stdio)...\n")
	return server.Run(ctx, &mcp.StdioTransport{})
}

// Handlers holds references to all engine instances.
type Handlers struct {
	graph        *graph.Graph
	memoryStore  *memory.Store
	retriever    *memory.Retriever
	sessionMgr   *memory.SessionManager
	skillStore   *skills.Store
	skillMatcher *skills.Matcher
	skillExec    *skills.Executor
	planStore    *orchestrator.PlanStore
	router       *orchestrator.Router
	tracker      *orchestrator.Tracker
	orchConfig   *orchestrator.Config
}
