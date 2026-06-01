package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/skills"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage self-evolving skills",
	Run:   func(cmd *cobra.Command, _ []string) { cmd.Help() },
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active skills",
	RunE:  runSkillsList,
}

var skillsLineageCmd = &cobra.Command{
	Use:   "lineage [skill-id]",
	Short: "Show the evolution history of a skill",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsLineage,
}

var skillsFreezeCmd = &cobra.Command{
	Use:   "freeze [skill-id]",
	Short: "Freeze a skill (stop it from evolving)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsFreeze,
}

var skillsUnfreezeCmd = &cobra.Command{
	Use:   "unfreeze [skill-id]",
	Short: "Unfreeze a skill (allow it to evolve again)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsUnfreeze,
}

func init() {
	skillsCmd.AddCommand(skillsListCmd, skillsLineageCmd, skillsFreezeCmd, skillsUnfreezeCmd)
}

func requireSkillStore() (*skills.Store, error) {
	dbURL := GetDBURL()
	if dbURL == "" {
		return nil, fmt.Errorf("no database configured\n  Run: universe config set db postgres://...")
	}
	store, err := skills.NewStore(dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect to skills db: %w", err)
	}
	return store, nil
}

func runSkillsList(_ *cobra.Command, _ []string) error {
	store, err := requireSkillStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer store.Close()

	stats, err := store.GetStats()
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}

	skList, err := store.GetByGraphNodes([]string{}, "", 0, 50)
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	fmt.Println("📋 Active Skills")
	fmt.Println("═══════════════════════════════════════════════")

	for _, sk := range skList {
		if !sk.IsActive {
			continue
		}
		sr := 0.0
		if sk.TimesApplied > 0 {
			sr = float64(sk.TimesSucceeded) / float64(sk.TimesApplied) * 100
		}
		frozen := ""
		if sk.IsFrozen {
			frozen = " 🧊"
		}
		fmt.Printf("  %-36s  v%-3d  %-10s  %-8s  %.0f%% success%s\n",
			sk.ID[:min(36, len(sk.ID))],
			sk.Version,
			sk.Evolution,
			sk.Language,
			sr,
			frozen,
		)
		if sk.Name != "" {
			fmt.Printf("    %s\n", sk.Name)
		}
	}

	fmt.Printf("\n  %d active, %d frozen\n", stats.TotalActive, stats.TotalFrozen)
	return nil
}

func runSkillsLineage(_ *cobra.Command, args []string) error {
	store, err := requireSkillStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer store.Close()

	lineage, err := store.GetLineage(args[0])
	if err != nil {
		return fmt.Errorf("get lineage: %w", err)
	}
	derived, err := store.GetChildren(args[0])
	if err != nil {
		return fmt.Errorf("get children: %w", err)
	}

	fmt.Println("🌳 Skill Lineage")
	fmt.Println("═══════════════════════════════════════════════")

	for i, sk := range lineage {
		prefix := "  "
		if i == len(lineage)-1 {
			prefix = "  ▶ "
		}
		fmt.Printf("%s[v%d] %s  (%s)  %s\n", prefix, sk.Version, sk.Name, sk.Evolution, sk.ID)
	}

	if len(derived) > 0 {
		fmt.Println("\n  Derived variants:")
		for _, sk := range derived {
			fmt.Printf("    ↳ [v%d] %s  (%s)  %s\n", sk.Version, sk.Name, sk.Evolution, sk.ID)
		}
	}

	return nil
}

func runSkillsFreeze(_ *cobra.Command, args []string) error {
	store, err := requireSkillStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer store.Close()

	if err := store.FreezeSkill(args[0]); err != nil {
		return fmt.Errorf("freeze skill: %w", err)
	}
	fmt.Printf("🧊 Skill %s frozen. It will continue working but stop evolving.\n", args[0])
	return nil
}

func runSkillsUnfreeze(_ *cobra.Command, args []string) error {
	store, err := requireSkillStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer store.Close()

	if err := store.UnfreezeSkill(args[0]); err != nil {
		return fmt.Errorf("unfreeze skill: %w", err)
	}
	fmt.Printf("🔥 Skill %s unfrozen. Evolution re-enabled.\n", args[0])
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
