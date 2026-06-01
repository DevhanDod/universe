package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/orchestrator"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Open the planner Cursor window (premium model)",
	Long:  "Opens a Cursor window configured with your premium model for planning and verification.",
	Run:   runPlan,
}

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Open the executor Cursor window (execution model)",
	Long:  "Opens a Cursor window configured with your execution model for coding and testing.",
	Run:   runExec,
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Open both planner and executor Cursor windows",
	Long:  "Opens two Cursor windows — planner (premium) and executor (cheap).",
	Run:   runStart,
}

func init() {
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(startCmd)
}

func runPlan(cmd *cobra.Command, args []string) {
	projectDir, _ := os.Getwd()
	wsPath := filepath.Join(projectDir, ".universe", "workspaces", "planner.code-workspace")

	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		fmt.Println("Planner workspace not found.")
		fmt.Println("Run 'universe setup' first to generate workspace files.")
		os.Exit(1)
	}

	cfg := LoadConfig()
	model := cfg.PremiumModel.Name
	if model == "" {
		model = "premium model"
	}
	fmt.Printf("Opening planner window (%s)...\n", model)

	if err := orchestrator.OpenPlannerWorkspace(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening Cursor: %v\n", err)
		fmt.Println("Manual: cursor " + wsPath)
		os.Exit(1)
	}
}

func runExec(cmd *cobra.Command, args []string) {
	projectDir, _ := os.Getwd()
	wsPath := filepath.Join(projectDir, ".universe", "workspaces", "executor.code-workspace")

	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		fmt.Println("Executor workspace not found.")
		fmt.Println("Run 'universe setup' first to generate workspace files.")
		os.Exit(1)
	}

	cfg := LoadConfig()
	model := cfg.ExecutionModel.Name
	if model == "" {
		model = "execution model"
	}
	fmt.Printf("Opening executor window (%s)...\n", model)

	if err := orchestrator.OpenExecutorWorkspace(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening Cursor: %v\n", err)
		fmt.Println("Manual: cursor " + wsPath)
		os.Exit(1)
	}
}

func runStart(cmd *cobra.Command, args []string) {
	projectDir, _ := os.Getwd()
	cfg := LoadConfig()

	fmt.Println("Opening Universe workspaces...")
	premium := cfg.PremiumModel.Name
	execution := cfg.ExecutionModel.Name
	if premium == "" {
		premium = "premium model"
	}
	if execution == "" {
		execution = "execution model"
	}
	fmt.Printf("  Planner:  %s\n", premium)
	fmt.Printf("  Executor: %s\n", execution)
	fmt.Println()

	if err := orchestrator.OpenPlannerWorkspace(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: couldn't open planner: %v\n", err)
	}

	if err := orchestrator.OpenExecutorWorkspace(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: couldn't open executor: %v\n", err)
	}

	fmt.Println("Both windows should be open.")
	fmt.Println()
	fmt.Println("Workflow:")
	fmt.Println("  1. In Planner: describe the task")
	fmt.Println("  2. In Executor: say 'execute'")
	fmt.Println("  3. In Planner: say 'verify'")
}
