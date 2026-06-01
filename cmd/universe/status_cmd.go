package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/memory"
	"github.com/Universe/universe/internal/skills"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of all 5 engines",
	RunE:  runStatus,
}

func runStatus(_ *cobra.Command, _ []string) error {
	dbURL := GetDBURL()

	fmt.Println("🌌 Universe Status")
	fmt.Println("═══════════════════════════════════════════════")

	// ── Engine 1: Knowledge Graph ────────────────────────────────────────────
	g, err := loadGraph(resolvedGraphPath())
	if err != nil {
		PrintEngine(1, "Knowledge Graph", "Unavailable", "Run 'universe init' to scan your codebase")
	} else {
		stats := g.Stats()
		detail := fmt.Sprintf("%d nodes, %d edges, %d packages", stats.TotalNodes, stats.TotalEdges, stats.PackageCount)
		PrintEngine(1, "Knowledge Graph", "Active", detail)
	}

	// ── Engines 2-5 need database ────────────────────────────────────────────
	if dbURL == "" {
		PrintEngine(2, "Persistent Memory", "Unavailable", "Connect a database: universe config set db postgres://...")
		PrintEngine(3, "Evolving Skills", "Unavailable", "Connect a database: universe config set db postgres://...")
		PrintEngine(4, "Compression", "Active", "compact mode (no database needed)")
		PrintEngine(5, "Orchestrator", "Unavailable", "Connect a database: universe config set db postgres://...")
		printStatusFooter("local", "")
		return nil
	}

	// ── Engine 2: Memory ─────────────────────────────────────────────────────
	memStore, err := memory.NewStore(dbURL)
	if err != nil {
		PrintEngine(2, "Persistent Memory", "Error", err.Error())
	} else {
		defer memStore.Close()
		memStats, err := memStore.GetStats()
		if err != nil {
			PrintEngine(2, "Persistent Memory", "Error", err.Error())
		} else {
			recallRate := 0.0
			if memStats.TotalObservations > 0 && memStats.TotalRecalls > 0 {
				recallRate = float64(memStats.TotalRecalls) / float64(memStats.TotalObservations) * 100
			}
			detail := fmt.Sprintf("%d personal observations, %.0f%% recall rate",
				memStats.TotalObservations, recallRate)
			PrintEngine(2, "Persistent Memory", "Active", detail)
		}
	}

	// ── Engine 3: Skills ─────────────────────────────────────────────────────
	skillStore, err := skills.NewStore(dbURL)
	if err != nil {
		PrintEngine(3, "Evolving Skills", "Error", err.Error())
	} else {
		defer skillStore.Close()
		skStats, err := skillStore.GetStats()
		if err != nil {
			PrintEngine(3, "Evolving Skills", "Error", err.Error())
		} else {
			detail := fmt.Sprintf("%d active, %d frozen, avg %.0f%% success",
				skStats.TotalActive, skStats.TotalFrozen, skStats.AvgSuccessRate*100)
			PrintEngine(3, "Evolving Skills", "Active", detail)
		}
	}

	// ── Engine 4: Compression ────────────────────────────────────────────────
	PrintEngine(4, "Compression", "Active", "compact mode, graph shorthand enabled")

	// ── Engine 5: Orchestrator ───────────────────────────────────────────────
	costStats, err := queryTodayCostStats(dbURL)
	if err != nil {
		PrintEngine(5, "Orchestrator", "Active", "plan bridge ready (no plans today)")
	} else {
		detail := fmt.Sprintf("%d plans today, $%.4f estimated, %.0f%% low-cost",
			costStats.TasksToday, costStats.CostToday, costStats.HaikuPct)
		PrintEngine(5, "Orchestrator", "Active", detail)
	}

	printStatusFooter("team", dbURL)
	return nil
}

func printStatusFooter(mode, dbURL string) {
	cfg := LoadConfig()
	fmt.Println("═══════════════════════════════════════════════")
	if mode == "local" {
		fmt.Println("  Mode:     local")
		fmt.Println("  Database: not connected")
	} else {
		fmt.Printf("  Mode:     team (PostgreSQL)\n")
		fmt.Printf("  Database: %s\n", MaskPassword(dbURL))
	}
	if cfg.PremiumModel.Name != "" {
		fmt.Printf("  Premium:  %s\n", cfg.PremiumModel.Name)
		fmt.Printf("  Execution: %s\n", cfg.ExecutionModel.Name)
	} else {
		fmt.Println("  Models:   not configured (run 'universe setup')")
	}
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Version:  %s\n", Version)
}

type TodayCostStats struct {
	TasksToday int
	CostToday  float64
	HaikuPct   float64
}

func queryTodayCostStats(dbURL string) (*TodayCostStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	var stats TodayCostStats
	err = conn.QueryRow(ctx, `
		SELECT
			COUNT(DISTINCT plan_id),
			COALESCE(SUM(estimated_total_cost), 0),
			COALESCE(
				COUNT(*) FILTER (WHERE executor_model ILIKE '%haiku%')::float
				/ GREATEST(COUNT(*), 1) * 100,
			0)
		FROM plan_costs
		WHERE created_at >= CURRENT_DATE`).
		Scan(&stats.TasksToday, &stats.CostToday, &stats.HaikuPct)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}
