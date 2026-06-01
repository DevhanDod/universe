package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
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
	// includeSource=true so the dashboard's Code Inspector and detail endpoint
	// can return real source slices instead of empty strings.
	an := buildAnalyzer(false, true)
	g, err := an.Analyze(absPath)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	stats := g.Stats()
	fmt.Printf(" %d nodes, %d edges, %d files\n", stats.TotalNodes, stats.TotalEdges, stats.FileCount)

	if err := EnsureLocalDataDir(); err != nil {
		return fmt.Errorf("create .universe dir: %w", err)
	}

	localPath := filepath.Join(LocalDataDir(), "graph.json")
	if err := g.ExportJSON(localPath); err != nil {
		return fmt.Errorf("save graph: %w", err)
	}
	fmt.Printf("   Stored: %s\n", localPath)

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Println()
	fmt.Println("✅ Graph ready!")
	fmt.Printf("   Time:  %s\n", elapsed)
	fmt.Printf("   Nodes: %d  Edges: %d  Packages: %d\n", stats.TotalNodes, stats.TotalEdges, stats.PackageCount)

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("   universe status        Check all engines")
	fmt.Println("   universe mcp --stdio   Start MCP server for Cursor")
	fmt.Println("   universe dashboard     Open the dashboard")

	_ = os.Stderr // silence unused import if any
	return nil
}
