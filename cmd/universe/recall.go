package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/memory"
)

// universe recall — shell replacement for the recall_memory + get_observation_details
// MCP tools. Lives in shell because the agent only needs to read; the
// inputs are short strings, not structured data — so we can ship them via
// argv instead of a per-turn MCP schema. See Planing/mcp-restructuring.md.

var (
	recallNode  string
	recallID    string
	recallLimit int
)

var recallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Search your past observations (memory engine)",
	Long: `Search your personal memory — past fixes, patterns, decisions stored via
store_observation. Returns compact summaries.

  universe recall "type mismatch"
  universe recall --node auth:ValidateToken
  universe recall --id <observation-id>`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRecall,
}

func init() {
	recallCmd.Flags().StringVar(&recallNode, "node", "", "filter by graph node ID")
	recallCmd.Flags().StringVar(&recallID, "id", "", "fetch full detail for a specific observation")
	recallCmd.Flags().IntVar(&recallLimit, "limit", 5, "max results")
	rootCmd.AddCommand(recallCmd)
}

func runRecall(_ *cobra.Command, args []string) error {
	dbURL := GetDBURL()
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "No database configured. Run: universe config set db postgres://...")
		return nil
	}
	store, err := memory.NewStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect memory store: %w", err)
	}
	defer store.Close()

	devID := getDeveloperID()

	// Detail mode: full payload for a single observation.
	if recallID != "" {
		obs, err := store.GetByID(recallID)
		if err != nil || obs == nil {
			fmt.Fprintf(os.Stderr, "Not found: %v\n", err)
			return nil
		}
		fmt.Printf("[%s] %s\n", obs.Category, obs.Summary)
		fmt.Printf("  Node: %s\n", obs.GraphNodeID)
		fmt.Printf("  Created: %s\n", obs.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("  Confidence: %.0f%%\n", obs.Confidence*100)
		if obs.Detail != "" {
			fmt.Printf("  Detail:\n%s\n", obs.Detail)
		}
		return nil
	}

	// Node-filtered list.
	if recallNode != "" {
		results, err := store.GetByGraphNode(recallNode, devID, recallLimit)
		if err != nil {
			return fmt.Errorf("list by node: %w", err)
		}
		printObservationSummaries(results)
		return nil
	}

	// Keyword search.
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	if query == "" {
		fmt.Fprintln(os.Stderr, "Provide a query, --node, or --id.")
		return nil
	}
	results, err := store.SearchKeyword(query, devID, recallLimit)
	if err != nil {
		return fmt.Errorf("search memory: %w", err)
	}
	printObservationSummaries(results)
	return nil
}

func printObservationSummaries(rows []memory.ObservationSummary) {
	if len(rows) == 0 {
		fmt.Println("No observations found.")
		return
	}
	fmt.Printf("Observations (%d):\n\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  [%s] %s\n", r.Category, r.Summary)
		fmt.Printf("    id=%s node=%s conf=%.0f%% %s\n",
			r.ID, r.GraphNodeID, r.Confidence*100,
			r.CreatedAt.Format("Jan 2"))
	}
}

// getDeveloperID resolves the developer ID once from config, falling back
// to an empty string (memory queries treat that as "any developer").
func getDeveloperID() string {
	return LoadConfig().DeveloperID
}
