package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/orchestrator"
)

// universe plans / universe cost — shell replacements for get_plan,
// get_plan_result, get_cost_summary MCP tools. These are read-only,
// so they cost nothing as shell commands but would inflate the MCP
// tool surface (and per-turn schema overhead) if registered there.

var plansCmd = &cobra.Command{
	Use:   "plans",
	Short: "View execution plans (read-only — store_plan stays MCP)",
	Run:   func(cmd *cobra.Command, _ []string) { cmd.Help() },
}

var plansGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the latest pending plan",
	RunE:  runPlansGet,
}

var plansResultCmd = &cobra.Command{
	Use:   "result [plan-id]",
	Short: "Show execution result for a plan (defaults to latest completed)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPlansResult,
}

func init() {
	plansCmd.AddCommand(plansGetCmd, plansResultCmd)
	rootCmd.AddCommand(plansCmd)
}

func requirePlanStore() (*orchestrator.PlanStore, error) {
	dbURL := GetDBURL()
	if dbURL == "" {
		return nil, fmt.Errorf("no database configured\n  Run: universe config set db postgres://...")
	}
	ps, err := orchestrator.NewPlanStore(dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect plan store: %w", err)
	}
	return ps, nil
}

func runPlansGet(_ *cobra.Command, _ []string) error {
	ps, err := requirePlanStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer ps.Close()

	plan, err := ps.GetLatestPlan(getDeveloperID())
	if err != nil || plan == nil {
		fmt.Println("No pending plan found.")
		return nil
	}
	printPlan(plan)
	return nil
}

func runPlansResult(_ *cobra.Command, args []string) error {
	ps, err := requirePlanStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer ps.Close()

	var plan *orchestrator.Plan
	if len(args) > 0 {
		plan, err = ps.GetPlanByID(args[0])
	} else {
		plan, err = ps.GetLatestCompletedPlan(getDeveloperID())
	}
	if err != nil || plan == nil {
		fmt.Println("No completed plan found.")
		return nil
	}
	printPlan(plan)
	return nil
}

func printPlan(plan *orchestrator.Plan) {
	fmt.Printf("Plan: %s\n", plan.Title)
	fmt.Printf("ID: %s | Status: %s | Risk: %s\n", plan.ID, plan.Status, plan.RiskLevel)
	if len(plan.Steps) > 0 {
		fmt.Println("\nSteps:")
		for i, step := range plan.Steps {
			fmt.Printf("  %d. %s\n", i+1, step)
		}
	}
	if len(plan.FilesToChange) > 0 {
		fmt.Printf("\nFiles to change: %v\n", plan.FilesToChange)
	}
	if plan.ResultSuccess != nil {
		fmt.Println()
		if *plan.ResultSuccess {
			fmt.Println("Result: SUCCESS")
		} else {
			fmt.Println("Result: FAILED")
		}
		if plan.ResultSummary != "" {
			fmt.Printf("Summary: %s\n", plan.ResultSummary)
		}
		if plan.ResultError != "" {
			fmt.Printf("Error: %s\n", plan.ResultError)
		}
		if len(plan.ResultFiles) > 0 {
			fmt.Printf("Changed: %v\n", plan.ResultFiles)
		}
	}
	if plan.Verified != nil {
		fmt.Println()
		if *plan.Verified {
			fmt.Println("Verification: APPROVED")
		} else {
			fmt.Println("Verification: REJECTED")
		}
		if plan.VerificationNote != "" {
			fmt.Printf("Note: %s\n", plan.VerificationNote)
		}
	}
}

// universe cost — shell replacement for get_cost_summary MCP tool.

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Cost savings summary across plans",
	RunE:  runCost,
}

func init() {
	rootCmd.AddCommand(costCmd)
}

func runCost(_ *cobra.Command, _ []string) error {
	dbURL := GetDBURL()
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "No database configured.")
		return nil
	}
	tr, err := orchestrator.NewTracker(dbURL)
	if err != nil {
		return fmt.Errorf("connect tracker: %w", err)
	}
	defer tr.Close()

	devID := getDeveloperID()
	if devID == "" {
		summary, err := tr.GetMonthlySummary()
		if err != nil {
			return fmt.Errorf("monthly summary: %w", err)
		}
		if len(summary) == 0 {
			fmt.Println("No cost data yet.")
			return nil
		}
		fmt.Println("Monthly cost summary:")
		for _, m := range summary {
			fmt.Printf("  %s — plans=%d, actual=$%.2f, would-have=$%.2f, saved=$%.2f (%.0f%%)\n",
				m.Month, m.TotalPlans, m.ActualCost, m.WouldHaveCost, m.Savings, m.SavingsPercent)
		}
		return nil
	}

	rows, err := tr.GetDeveloperSummary(devID)
	if err != nil {
		return fmt.Errorf("developer summary: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No cost data yet for this developer.")
		return nil
	}
	fmt.Printf("Cost summary — developer %s:\n", devID)
	for _, r := range rows {
		fmt.Printf("  %s — plans=%d, actual=$%.2f, saved=$%.2f\n",
			r.Week, r.TotalPlans, r.ActualCost, r.Savings)
	}
	return nil
}
