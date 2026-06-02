package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Universe/universe/internal/languages"
	"github.com/spf13/cobra"
)

var languagesFormat string

var languagesCmd = &cobra.Command{
	Use:   "languages",
	Short: "List languages universe can analyze, grouped by support tier",
	RunE:  runLanguages,
}

func init() {
	languagesCmd.Flags().StringVar(&languagesFormat, "format", "text", "output format: text or json")
}

func runLanguages(cmd *cobra.Command, args []string) error {
	format := strings.ToLower(strings.TrimSpace(languagesFormat))
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(languages.Catalog)
	case "", "text":
		printLanguagesText(cmd.OutOrStdout())
		return nil
	default:
		return fmt.Errorf("unknown --format %q (use text or json)", languagesFormat)
	}
}

func printLanguagesText(w interface{ Write([]byte) (int, error) }) {
	grouped := languages.ByTier()
	for _, tier := range languages.TierOrder {
		entries := grouped[tier]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		fmt.Fprintf(w, "\n%s\n", languages.TierLabel(tier))
		for _, l := range entries {
			tags := append([]string{}, l.Extensions...)
			tags = append(tags, l.Filenames...)
			parser := l.Parser
			if parser == "" {
				parser = "—"
			}
			fmt.Fprintf(w, "  %-14s %-30s parser=%s\n", l.Name, strings.Join(tags, " "), parser)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Files in any other language still appear in the graph as inventory-only nodes.")
}
