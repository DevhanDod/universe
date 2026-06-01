package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/analyzer"
	"github.com/Universe/universe/internal/extractor"
	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/mcpserver"
	"github.com/Universe/universe/internal/memory"
	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/orchestrator"
	"github.com/Universe/universe/internal/parser"
	"github.com/Universe/universe/internal/skills"
)

var mcpRepoPath string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the Universe MCP server for Cursor integration (stdio)",
	Long: `Starts a Model Context Protocol server over stdin/stdout.
Cursor connects to this process automatically via .cursor/mcp.json.

All diagnostic output goes to stderr. Never write to stdout except
for JSON-RPC responses — Cursor uses stdout as the protocol wire.`,
	RunE: runMCP,
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRepoPath, "repo", "", "path to the repository to serve (required)")
	_ = mcpCmd.MarkFlagRequired("repo")
}

func runMCP(cmd *cobra.Command, _ []string) error {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lshortfile)

	repoPath := filepath.Clean(mcpRepoPath)

	loadDotEnv(filepath.Join(repoPath, ".env"))
	loadDotEnv(".env")

	log.Printf("universe mcp: starting for repo %s", repoPath)

	// ── Engine 1: Graph ──────────────────────────────────────────────────────
	g := loadOrBuildGraph(repoPath)
	log.Printf("universe mcp: graph loaded — %d nodes", len(g.Nodes))

	// ── DB connection ────────────────────────────────────────────────────────
	// Resolves UNIVERSE_DB_URL → DATABASE_URL → ~/.universe/config.json so the
	// URL saved by `universe db start` is picked up automatically.
	dbURL := GetDBURL()
	if dbURL == "" {
		log.Printf("universe mcp: no db_url configured — memory/skills disabled (run: universe db start)")
	}

	// ── Engine 2: Memory ─────────────────────────────────────────────────────
	var memStore *memory.Store
	var memRetriever *memory.Retriever
	var sessionMgr *memory.SessionManager

	if dbURL != "" {
		ms, err := memory.NewStore(dbURL)
		if err != nil {
			log.Printf("universe mcp: memory store failed (%v) — memory disabled", err)
		} else {
			memStore = ms
			graphQ := &memGraphAdapter{g: g}
			memRetriever = memory.NewRetriever(memStore, nil, graphQ, memory.DefaultConfig())
			log.Printf("universe mcp: memory store connected")
		}
	}

	// ── Engine 3: Skills ─────────────────────────────────────────────────────
	var skillStore *skills.Store
	var skillMatcher *skills.Matcher
	var skillExec *skills.Executor

	if dbURL != "" {
		sk, err := skills.NewStore(dbURL)
		if err != nil {
			log.Printf("universe mcp: skill store failed (%v) — skills disabled", err)
		} else {
			skillStore = sk
			skillMatcher = skills.NewMatcher(skillStore, nil, skills.DefaultConfig())
			skillExec = skills.NewExecutor(skillStore, skills.DefaultConfig())
			log.Printf("universe mcp: skill store connected")
		}
	}

	// ── Engine 5: Plan Store + Router + Tracker ─────────────────────────────
	var planStore *orchestrator.PlanStore
	var planRouter *orchestrator.Router
	var planTracker *orchestrator.Tracker
	orchConfig := orchestrator.DefaultConfig()
	orchConfig.DatabaseURL = dbURL

	if dbURL != "" {
		ps, err := orchestrator.NewPlanStore(dbURL)
		if err != nil {
			log.Printf("universe mcp: plan store failed (%v) — plan tools disabled", err)
		} else {
			planStore = ps
			defer planStore.Close() //nolint:errcheck
			log.Printf("universe mcp: plan store connected")
		}
		tr, err := orchestrator.NewTracker(dbURL)
		if err != nil {
			log.Printf("universe mcp: tracker failed (%v) — cost tracking disabled", err)
		} else {
			planTracker = tr
			defer planTracker.Close()
			log.Printf("universe mcp: cost tracker connected")
		}
	}
	// Router is nil-safe on all fields; adapters for skill/memory interfaces can be wired later.
	planRouter = orchestrator.NewRouter(nil, nil, nil)

	// ── Build config and run ─────────────────────────────────────────────────
	config := mcpserver.ServerConfig{
		Version:            "0.1.0",
		DatabaseURL:        dbURL,
		Graph:              g,
		MemoryStore:        memStore,
		Retriever:          memRetriever,
		SessionMgr:         sessionMgr,
		SkillStore:         skillStore,
		SkillMatcher:       skillMatcher,
		SkillExec:          skillExec,
		PlanStore:          planStore,
		Router:             planRouter,
		Tracker:            planTracker,
		OrchestratorConfig: &orchConfig,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return mcpserver.RunStdio(ctx, config)
}

// ── Graph helpers ─────────────────────────────────────────────────────────────

func loadOrBuildGraph(repoPath string) *graph.Graph {
	graphFile := filepath.Join(repoPath, ".universe", "graph.json")

	if g, err := loadGraph(graphFile); err == nil {
		return g
	}

	log.Printf("universe mcp: no graph.json found, scanning %s ...", repoPath)
	reg := parser.NewRegistry()
	reg.Register(parser.NewGoParser())
	reg.Register(parser.NewPythonParser())
	exts := []extractor.Extractor{
		extractor.NewGoExtractor(),
		extractor.NewPythonExtractor(),
	}
	an := analyzer.NewAnalyzer(reg, exts, analyzer.Config{})
	g, err := an.Analyze(repoPath)
	if err != nil {
		log.Printf("universe mcp: graph build failed: %v — starting with empty graph", err)
		return &graph.Graph{
			Nodes: make(map[string]*models.Node),
			Files: make(map[string]*models.FileInfo),
		}
	}

	if err := os.MkdirAll(filepath.Dir(graphFile), 0o755); err == nil {
		_ = g.ExportJSON(graphFile)
		log.Printf("universe mcp: graph saved to %s", graphFile)
	}
	return g
}

// ── memGraphAdapter implements memory.GraphQuerier ───────────────────────────

type memGraphAdapter struct{ g *graph.Graph }

func (a *memGraphAdapter) GetCallerIDs(nodeID string) ([]string, error) {
	deps := a.g.GetDependents(nodeID)
	ids := make([]string, 0, len(deps))
	for _, n := range deps {
		ids = append(ids, n.ID)
	}
	return ids, nil
}

func (a *memGraphAdapter) GetCalleeIDs(nodeID string) ([]string, error) {
	deps := a.g.GetDependencies(nodeID)
	ids := make([]string, 0, len(deps))
	for _, n := range deps {
		ids = append(ids, n.ID)
	}
	return ids, nil
}

// ── .env loader ──────────────────────────────────────────────────────────────

func loadDotEnv(path string) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}

// keep fmt imported (used in other files in this package)
var _ = fmt.Sprintf
