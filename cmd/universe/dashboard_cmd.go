package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Universe/universe/internal/dashboard"
	"github.com/Universe/universe/internal/graph"
	"github.com/spf13/cobra"
)

var (
	dashboardPort   int
	dashboardNoOpen bool
	dashboardRepo   string
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the Universe Dashboard in a browser",
	Long: `Starts the Universe Dashboard on localhost and opens it in your browser.

The dashboard shows engine status, memory observations, skills, compression
samples, and routing traces. PostgreSQL data is loaded from DATABASE_URL.
Pass --repo to also load graph data from that repository's .universe/graph.json.`,
	RunE: runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 3001, "port to listen on")
	dashboardCmd.Flags().BoolVar(&dashboardNoOpen, "no-open", false, "don't auto-open browser")
	dashboardCmd.Flags().StringVar(&dashboardRepo, "repo", "", "path to repository (loads graph.json)")
}

func runDashboard(cmd *cobra.Command, _ []string) error {
	loadDotEnv(".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = GetDBURL()
	}
	if dbURL == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "note: DATABASE_URL not set — running without database")
	}

	// Determine project directory — default to cwd if --repo not provided
	projectDir := dashboardRepo
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	projectDir = filepath.Clean(projectDir)

	// Always try to load graph from project directory
	var graphArg *graph.Graph
	gf := filepath.Join(projectDir, ".universe", "graph.json")
	if loaded, err := loadGraph(gf); err == nil {
		graphArg = loaded
		fmt.Fprintf(cmd.OutOrStdout(), "Loaded graph: %d nodes from %s\n", len(loaded.Nodes), gf)
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(cmd.ErrOrStderr(), "note: could not load graph from %s: %v\n", gf, err)
	}

	srv, err := dashboard.NewServer(dbURL, dashboardPort, projectDir, graphArg)
	if err != nil {
		return fmt.Errorf("create dashboard server: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d", dashboardPort)
	fmt.Fprintf(cmd.OutOrStdout(), "Universe Dashboard running at %s\n", url)

	if !dashboardNoOpen {
		dashboard.OpenBrowser(url)
	}

	return srv.Start()
}
