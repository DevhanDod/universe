package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/skills"
)

// universe skills find <query> — shell replacement for find_skill MCP tool.
// Kept as a subcommand of the existing skillsCmd so callers see all
// skill operations in one help group.

var skillsFindCmd = &cobra.Command{
	Use:   "find <query>",
	Short: "Find a matching skill recipe (requires verification before use)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsFind,
}

func init() {
	skillsCmd.AddCommand(skillsFindCmd)
}

func runSkillsFind(_ *cobra.Command, args []string) error {
	store, err := requireSkillStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		return nil
	}
	defer store.Close()

	matcher := skills.NewMatcher(store, nil, skills.DefaultConfig())
	result, err := matcher.Match(skills.MatchQuery{
		TaskText:    args[0],
		DeveloperID: getDeveloperID(),
		Limit:       5,
	})
	if err != nil {
		return fmt.Errorf("match: %w", err)
	}

	if result.ExplorationTriggered {
		fmt.Println("No close match — exploration mode. Reason from scratch this time.")
		return nil
	}
	if result.BestMatch == nil {
		fmt.Println("No matching skill found.")
		return nil
	}

	s := result.BestMatch
	fmt.Printf("Skill: %s v%d (%s)\n", s.Name, s.Version, s.Evolution)
	fmt.Printf("Success: %.0f%% (%d uses) | Confidence: %.0f%% | Lang: %s\n",
		s.SuccessRate*100, s.TimesApplied, s.Confidence*100, s.Language)
	if s.RequiresVerification {
		fmt.Println()
		fmt.Println("⚠️  REQUIRES VERIFICATION — premium model must review before use")
		if s.VerificationPrompt != "" {
			fmt.Println()
			fmt.Println("Verification prompt:")
			fmt.Println(s.VerificationPrompt)
		}
	}
	if len(result.Candidates) > 1 {
		fmt.Println()
		fmt.Printf("Other candidates (%d):\n", len(result.Candidates)-1)
		for i, c := range result.Candidates {
			if i == 0 || i > 4 {
				continue
			}
			fmt.Printf("  %s v%d (score=%.2f)\n", c.Name, c.Version, c.SearchScore)
		}
	}
	return nil
}
