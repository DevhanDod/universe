package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Universe/universe/internal/orchestrator"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Universe configuration",
	Run:   func(cmd *cobra.Command, _ []string) { cmd.Help() },
}

// config show — display current model routing config
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current model configuration",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := orchestrator.LoadUserConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "Universe configuration\n")
		fmt.Fprintf(w, "  premium:   %s\n", cfg.PremiumModel)
		fmt.Fprintf(w, "  low_cost:  %s\n", cfg.LowCostModel)
		fmt.Fprintf(w, "  db_url:    %s\n", MaskPassword(GetDBURL()))
		fmt.Fprintf(w, "  config:    %s\n", ConfigFilePath())
		return nil
	},
}

// config set db <url>
var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a configuration value (key: db)",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

// config get db
var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a configuration value (key: db)",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

// config reset
var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration to defaults",
	RunE:  runConfigReset,
}

func init() {
	configCmd.AddCommand(configShowCmd, configSetCmd, configGetCmd, configResetCmd)
}

func runConfigSet(_ *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	switch key {
	case "db":
		if !strings.HasPrefix(value, "postgres://") && !strings.HasPrefix(value, "postgresql://") {
			fmt.Fprintln(os.Stderr, "❌ Invalid database URL")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Expected format:")
			fmt.Fprintln(os.Stderr, "   postgres://user:password@host:port/database")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Example:")
			fmt.Fprintln(os.Stderr, "   universe config set db postgres://universe_admin:universe_secret_2024@localhost:5433/universe")
			return nil
		}
		cfg := LoadConfig()
		cfg.DBURL = value
		cfg.Mode = "team"
		if err := SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Println("✅ Database URL saved")
		fmt.Printf("   URL:    %s\n", MaskPassword(value))
		fmt.Printf("   Config: %s\n", ConfigFilePath())
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("   universe db status      Test the connection")
		fmt.Println("   universe db migrate     Run migrations (first time)")

	case "premium_model":
		cfg := LoadConfig()
		cfg.PremiumModel = findModelPreset(value)
		if err := SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		projectDir, _ := os.Getwd()
		orchestrator.GenerateWorkspaces(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name) //nolint:errcheck
		fmt.Printf("✅ Premium model set to: %s\n", value)
		fmt.Println("   Workspace files regenerated.")

	case "execution_model":
		cfg := LoadConfig()
		cfg.ExecutionModel = findModelPreset(value)
		if err := SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		projectDir, _ := os.Getwd()
		orchestrator.GenerateWorkspaces(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name) //nolint:errcheck
		orchestrator.SetDefaultModel(projectDir, value)                                              //nolint:errcheck
		fmt.Printf("✅ Execution model set to: %s\n", value)
		fmt.Println("   Workspace files and default model regenerated.")

	case "developer_id":
		cfg := LoadConfig()
		cfg.DeveloperID = value
		if err := SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("✅ Developer ID set to: %s\n", value)

	default:
		fmt.Fprintf(os.Stderr, "Unknown config key %q — supported: db, premium_model, execution_model, developer_id\n", key)
	}
	return nil
}

func runConfigGet(_ *cobra.Command, args []string) error {
	switch args[0] {
	case "db":
		dbURL := GetDBURL()
		if dbURL != "" {
			fmt.Printf("Database URL: %s\n", MaskPassword(dbURL))
			fmt.Printf("Config file:  %s\n", ConfigFilePath())
		} else {
			fmt.Println("Database: not configured")
			fmt.Println()
			fmt.Println("To connect:")
			fmt.Println("   universe config set db postgres://user:pass@host:5432/universe")
		}
	case "models":
		cfg := LoadConfig()
		fmt.Println("Model configuration:")
		if cfg.PremiumModel.Name != "" {
			fmt.Printf("  Premium:   %s (%s) — $%.1f/$%.1f per 1M tokens\n",
				cfg.PremiumModel.Name, cfg.PremiumModel.Provider,
				cfg.PremiumModel.InputCostPerM, cfg.PremiumModel.OutputCostPerM)
			fmt.Printf("  Execution: %s (%s) — $%.2f/$%.2f per 1M tokens\n",
				cfg.ExecutionModel.Name, cfg.ExecutionModel.Provider,
				cfg.ExecutionModel.InputCostPerM, cfg.ExecutionModel.OutputCostPerM)
		} else {
			fmt.Println("  Not configured — run 'universe setup'")
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown config key %q — supported: db, models\n", args[0])
	}
	return nil
}

func runConfigReset(_ *cobra.Command, _ []string) error {
	cfg := UniverseConfig{Mode: "local"}
	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Println("✅ Configuration reset to local mode")
	fmt.Printf("   Config: %s\n", ConfigFilePath())
	return nil
}
