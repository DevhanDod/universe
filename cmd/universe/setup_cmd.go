package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/orchestrator"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure Universe — pick models, generate workspace files and Cursor rules",
	Long: `Interactive setup that configures:
  - Your premium model (for planning and verification)
  - Your execution model (for coding and testing)
  - Cursor workspace files (auto-sets model per window)
  - Cursor rules (planner, executor, compression)
  - MCP connection config

Run this once per project. Change models later with:
  universe config set premium_model <name>`,
	Run: runSetup,
}

var setupRulesCmd = &cobra.Command{
	Use:   "setup-rules",
	Short: "Regenerate Cursor rules files only (without full setup)",
	Long:  "Regenerates .cursor/rules/ files using your current model configuration.",
	Run:   runSetupRules,
}

var setupPremium string
var setupExecution string
var setupDB string

func init() {
	setupCmd.Flags().StringVar(&setupPremium, "premium", "", "Premium model name (skip interactive)")
	setupCmd.Flags().StringVar(&setupExecution, "execution", "", "Execution model name (skip interactive)")
	setupCmd.Flags().StringVar(&setupDB, "db", "", "Database URL (skip interactive)")
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(setupRulesCmd)
}

// modelPresets lists known models with pricing for the interactive picker.
var modelPresets = []struct {
	Name           string
	Provider       string
	InputCostPerM  float64
	OutputCostPerM float64
	Tier           string // "premium", "execution", or "both"
}{
	{"claude-opus-4", "anthropic", 15.0, 75.0, "premium"},
	{"gpt-4o", "openai", 5.0, 15.0, "premium"},
	{"gemini-2.5-pro", "google", 1.25, 10.0, "premium"},
	{"o3", "openai", 10.0, 40.0, "premium"},
	{"claude-sonnet-4", "anthropic", 3.0, 15.0, "both"},
	{"claude-haiku-3.5", "anthropic", 0.25, 1.25, "execution"},
	{"gpt-4o-mini", "openai", 0.15, 0.60, "execution"},
	{"gemini-2.5-flash", "google", 0.15, 0.60, "execution"},
}

func runSetup(cmd *cobra.Command, args []string) {
	fmt.Println("Universe Setup")
	fmt.Println("===============================================")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// ── Step 1: Pick premium model ──────────────────────────────────────────
	var premiumModel orchestrator.ModelConfig

	if setupPremium != "" {
		premiumModel = findModelPreset(setupPremium)
	} else {
		fmt.Println("Choose your PREMIUM model (for planning & verification):")
		fmt.Println()
		premiumOptions := filterModels("premium")
		for i, m := range premiumOptions {
			fmt.Printf("  %d. %-25s (%s) — $%.1f/$%.1f per 1M tokens\n",
				i+1, m.Name, m.Provider, m.InputCostPerM, m.OutputCostPerM)
		}
		fmt.Printf("  %d. Custom (type model name)\n", len(premiumOptions)+1)
		fmt.Println()
		fmt.Print("> ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		idx, err := strconv.Atoi(choice)
		if err == nil && idx >= 1 && idx <= len(premiumOptions) {
			p := premiumOptions[idx-1]
			premiumModel = orchestrator.ModelConfig{
				Name: p.Name, Provider: p.Provider,
				InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
			}
		} else {
			fmt.Print("Model name: ")
			name, _ := reader.ReadString('\n')
			premiumModel = orchestrator.ModelConfig{
				Name: strings.TrimSpace(name), Provider: "custom",
				InputCostPerM: 10.0, OutputCostPerM: 30.0,
			}
		}
	}
	fmt.Printf("  Premium: %s\n\n", premiumModel.Name)

	// ── Step 2: Pick execution model ────────────────────────────────────────
	var executionModel orchestrator.ModelConfig

	if setupExecution != "" {
		executionModel = findModelPreset(setupExecution)
	} else {
		fmt.Println("Choose your EXECUTION model (for coding & testing):")
		fmt.Println()
		execOptions := filterModels("execution")
		for i, m := range execOptions {
			fmt.Printf("  %d. %-25s (%s) — $%.2f/$%.2f per 1M tokens\n",
				i+1, m.Name, m.Provider, m.InputCostPerM, m.OutputCostPerM)
		}
		fmt.Printf("  %d. Custom (type model name)\n", len(execOptions)+1)
		fmt.Println()
		fmt.Print("> ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		idx, err := strconv.Atoi(choice)
		if err == nil && idx >= 1 && idx <= len(execOptions) {
			p := execOptions[idx-1]
			executionModel = orchestrator.ModelConfig{
				Name: p.Name, Provider: p.Provider,
				InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
			}
		} else {
			fmt.Print("Model name: ")
			name, _ := reader.ReadString('\n')
			executionModel = orchestrator.ModelConfig{
				Name: strings.TrimSpace(name), Provider: "custom",
				InputCostPerM: 0.25, OutputCostPerM: 1.25,
			}
		}
	}
	fmt.Printf("  Execution: %s\n\n", executionModel.Name)

	// ── Step 3: Database URL ────────────────────────────────────────────────
	dbURL := setupDB
	if dbURL == "" {
		dbURL = GetDBURL()
	}
	if dbURL == "" {
		fmt.Println("Database URL (leave empty to skip — configure later with 'universe config set db'):")
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		dbURL = strings.TrimSpace(input)
	}

	// ── Step 4: Save config ─────────────────────────────────────────────────
	cfg := LoadConfig()
	cfg.DBURL = dbURL
	cfg.Mode = "local"
	if dbURL != "" {
		cfg.Mode = "team"
	}
	cfg.PremiumModel = premiumModel
	cfg.ExecutionModel = executionModel
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save config: %v\n", err)
	}

	// ── Step 5: Generate workspace files and Cursor rules ───────────────────
	projectDir, _ := os.Getwd()
	if err := orchestrator.RunSetup(
		projectDir,
		premiumModel.Name,
		executionModel.Name,
		premiumModel,
		executionModel,
		dbURL,
	); err != nil {
		fmt.Fprintf(os.Stderr, "Error during setup: %v\n", err)
		os.Exit(1)
	}

	// ── Step 6: Summary ─────────────────────────────────────────────────────
	fmt.Println("===============================================")
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Println("Created:")
	fmt.Println("  ~/.universe/config.json                        (model preferences)")
	fmt.Println("  .universe/workspaces/planner.code-workspace    (opens with " + premiumModel.Name + ")")
	fmt.Println("  .universe/workspaces/executor.code-workspace   (opens with " + executionModel.Name + ")")
	fmt.Println("  .cursor/rules/universe-planner.mdc             (planning agent rules)")
	fmt.Println("  .cursor/rules/universe-executor.mdc            (execution agent rules)")
	fmt.Println("  .cursor/rules/universe-compression.mdc         (caveman output mode)")
	fmt.Println("  .cursor/mcp.json                               (MCP server connection)")
	fmt.Println("  .vscode/settings.json                          (default model: " + executionModel.Name + ")")
	fmt.Println()
	fmt.Println("Quick start:")
	fmt.Println("  universe plan      Open planner window (" + premiumModel.Name + ")")
	fmt.Println("  universe exec      Open executor window (" + executionModel.Name + ")")
	fmt.Println("  universe start     Open both windows")
	fmt.Println()
	if dbURL != "" {
		fmt.Println("  universe db status   Test database connection")
	} else {
		fmt.Println("  universe config set db postgres://...   Connect database later")
	}
}

func runSetupRules(cmd *cobra.Command, args []string) {
	cfg := LoadConfig()
	if cfg.PremiumModel.Name == "" || cfg.ExecutionModel.Name == "" {
		fmt.Println("No models configured. Run 'universe setup' first.")
		os.Exit(1)
	}

	projectDir, _ := os.Getwd()
	if err := orchestrator.GenerateCursorRules(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name); err != nil {
		fmt.Fprintf(os.Stderr, "Error regenerating rules: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Cursor rules regenerated:")
	fmt.Println("  .cursor/rules/universe-planner.mdc")
	fmt.Println("  .cursor/rules/universe-executor.mdc")
	fmt.Println("  .cursor/rules/universe-compression.mdc")
}

func findModelPreset(name string) orchestrator.ModelConfig {
	for _, p := range modelPresets {
		if p.Name == name {
			return orchestrator.ModelConfig{
				Name: p.Name, Provider: p.Provider,
				InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
			}
		}
	}
	return orchestrator.ModelConfig{Name: name, Provider: "custom", InputCostPerM: 5.0, OutputCostPerM: 15.0}
}

func filterModels(tier string) []struct {
	Name           string
	Provider       string
	InputCostPerM  float64
	OutputCostPerM float64
	Tier           string
} {
	var result []struct {
		Name           string
		Provider       string
		InputCostPerM  float64
		OutputCostPerM float64
		Tier           string
	}
	for _, m := range modelPresets {
		if m.Tier == tier || m.Tier == "both" {
			result = append(result, m)
		}
	}
	return result
}
