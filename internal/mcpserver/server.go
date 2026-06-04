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

	// v0.2.8: reads are back on MCP because shell-only reads were
	// getting bypassed (agent reaches for Grep when there's no MCP
	// tool to discover). 6 read tools is a discoverable surface
	// without the 15-tool schema bloat of v0.2.6.
	//
	// Writes moved to shell (universe store-observation, report-skill,
	// store-plan, store-plan-result, verify-plan) because they take
	// structured input that fits stdin/flags cleanly and they don't
	// need to be in the per-turn schema list.

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_context",
		Description: "PRIMARY tool. Combines knowledge graph (callers, callees, " +
			"flows, impact), developer memory (past observations), and skill " +
			"recipes into one compact response. Call this FIRST for any code " +
			"question. Optional max_depth / min_confidence to scope the result.",
	}, h.HandleContextV28)

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_query",
		Description: "Quick single-symbol lookup: callers, callees, flows, impact. " +
			"Use when you know the exact name and don't need memory/skills.",
	}, h.HandleQueryV28)

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_impact",
		Description: "Blast-radius analysis. Walks the call graph upstream " +
			"(or downstream) up to max_depth, filtered by min_confidence. " +
			"Use before making structural changes.",
	}, h.HandleImpactV28)

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_search",
		Description: "Search the graph by name / file path / package. Use when " +
			"the exact symbol name isn't known. Returns up to `limit` (default " +
			"10, max 25) refs.",
	}, h.HandleSearchV28)

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_recall",
		Description: "Search developer memory for past observations — fixes, " +
			"patterns, decisions. Use the `node` field to scope to a specific " +
			"graph node.",
	}, h.HandleRecallV28)

	mcp.AddTool(server, &mcp.Tool{
		Name: "universe_skill_find",
		Description: "Find a skill recipe that matches what you're trying to do. " +
			"Returns step-by-step instructions from past successful patterns.",
	}, h.HandleSkillFindV28)

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
