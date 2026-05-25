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
	Version      string
	DatabaseURL  string
	Graph        *graph.Graph
	MemoryStore  *memory.Store
	Retriever    *memory.Retriever
	SessionMgr   *memory.SessionManager
	SkillStore   *skills.Store
	SkillMatcher *skills.Matcher
	SkillExec    *skills.Executor
	Orchestrator *orchestrator.Orchestrator
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
		orchestrator: config.Orchestrator,
	}

	// Engine 1 — Knowledge Graph
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_dependencies",
		Description: "Get all functions that call or are called by a given function. Returns the dependency tree from the knowledge graph.",
	}, h.HandleGetDependencies)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_impact_analysis",
		Description: "Analyze what will break if a given function is changed. Returns all affected functions and the risk level.",
	}, h.HandleGetImpactAnalysis)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_graph",
		Description: "Search the knowledge graph by function name, type name, or package name. Returns matching nodes with their connections.",
	}, h.HandleSearchGraph)

	// Engine 2 — Persistent Memory
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
		Description: "Store an observation — a pattern, decision, fix, or convention that should be remembered across sessions.",
	}, h.HandleStoreObservation)

	// Engine 3 — Self-Evolving Skills
	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_skill",
		Description: "Search for a matching skill recipe for the current task. If found, follow the skill instruction instead of reasoning from scratch — saves tokens and time.",
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
	orchestrator *orchestrator.Orchestrator
}
