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

	// Per Planing/mcp-restructuring.md, only write-style tools stay on MCP
	// because they need structured agent input that benefits from schema
	// validation. Every read tool that used to live here is now a shell
	// command (universe query / deps / impact / search / recall / skills
	// find / plans get / plans result / cost). That keeps the per-turn
	// MCP schema injection down from ~85K tokens to ~15K tokens.

	mcp.AddTool(server, &mcp.Tool{
		Name: "store_observation",
		Description: "Save an observation from this session — a pattern, decision, " +
			"or fix — to your personal memory.",
	}, h.HandleStoreObservation)

	mcp.AddTool(server, &mcp.Tool{
		Name: "report_skill_execution",
		Description: "Report whether applying a skill succeeded or failed. " +
			"Feeds the skill evolver.",
	}, h.HandleReportSkillExecution)

	mcp.AddTool(server, &mcp.Tool{
		Name: "store_plan",
		Description: "Store a step-by-step plan for the executor agent. " +
			"Planner-side tool.",
	}, h.HandleStorePlan)

	mcp.AddTool(server, &mcp.Tool{
		Name: "store_plan_result",
		Description: "Report the result of executing a plan — files changed, " +
			"tests passed/failed, error detail.",
	}, h.HandleStorePlanResult)

	mcp.AddTool(server, &mcp.Tool{
		Name: "verify_plan",
		Description: "Approve or reject an executor's plan result.",
	}, h.HandleVerifyPlan)

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
