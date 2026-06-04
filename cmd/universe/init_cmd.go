package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/report"
)

var initPath string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scan codebase and build the knowledge graph",
	Long: `Scans all source files in the current (or specified) directory using tree-sitter.
Parses functions, types, imports, and builds a dependency graph.
Stores the graph locally in .universe/graph.json.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVarP(&initPath, "path", "p", ".", "Path to scan (default: current directory)")
}

func runInit(_ *cobra.Command, _ []string) error {
	start := time.Now()

	absPath, err := filepath.Abs(initPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	fmt.Println("🔍 Scanning codebase...")
	fmt.Printf("   Path: %s\n", absPath)

	dbURL := GetDBURL()
	if dbURL != "" {
		fmt.Println("   Mode: team (PostgreSQL)")
	} else {
		fmt.Println("   Mode: local")
	}
	fmt.Println()

	fmt.Print("   Parsing with tree-sitter...")
	// includeSource=false: source is read from disk on demand (by the
	// dashboard and MCP tools that need it). Storing it in graph.json
	// duplicated every source byte and made the file ~10x larger than
	// the structural data warranted.
	an := buildAnalyzer(false, false)
	g, err := an.Analyze(absPath)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	stats := g.Stats()
	fmt.Printf(" %d nodes, %d edges, %d files\n", stats.TotalNodes, stats.TotalEdges, stats.FileCount)
	fmt.Printf("   Clusters: %d  Flows: %d  Impact nodes: %d\n",
		len(g.Clusters), len(g.Flows), len(g.Impact))

	if err := EnsureLocalDataDir(); err != nil {
		return fmt.Errorf("create .universe dir: %w", err)
	}

	localPath := filepath.Join(LocalDataDir(), "graph.json")
	if err := g.ExportJSON(localPath); err != nil {
		return fmt.Errorf("save graph: %w", err)
	}
	fmt.Printf("   Stored: %s\n", localPath)

	// Generate UNIVERSE_REPORT.md — a 3-8KB codebase overview the agent
	// can read once instead of issuing many MCP/shell calls for broad
	// "what is this codebase" questions.
	reportPath := filepath.Join(LocalDataDir(), "UNIVERSE_REPORT.md")
	if err := report.GenerateReport(g, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: couldn't generate report: %v\n", err)
	} else {
		fmt.Printf("   Report: %s\n", reportPath)
	}

	// Drop a Cursor rule that steers the agent to shell commands and the
	// report file instead of reading .universe/graph.json raw.
	if wrote, rulePath, err := writeCursorRule(absPath); err == nil && wrote {
		fmt.Printf("   Cursor rule: %s\n", rulePath)
	}

	// Generate .cursor/hooks.json — PreToolUse hook that reminds the
	// agent the graph has data when it's about to Read/Grep/Search.
	// Skipped if the user already has a hooks.json (they may have
	// customised it).
	if wrote, hooksPath, err := writeCursorHooks(absPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: hooks.json: %v\n", err)
	} else if wrote {
		fmt.Printf("   Cursor hooks: %s\n", hooksPath)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Println()
	fmt.Println("✅ Graph ready!")
	fmt.Printf("   Time:  %s\n", elapsed)
	fmt.Printf("   Nodes: %d  Edges: %d  Packages: %d\n", stats.TotalNodes, stats.TotalEdges, stats.PackageCount)

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("   universe status         Check all engines")
	fmt.Println("   universe mcp --repo .   Start MCP server (Cursor runs this automatically via .cursor/mcp.json)")
	fmt.Println("   universe dashboard      Open the dashboard")

	_ = os.Stderr // silence unused import if any
	return nil
}
