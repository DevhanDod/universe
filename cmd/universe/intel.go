package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// This file holds the read-only graph CLI commands. They exist because
// every MCP tool registered with Cursor inflates the per-turn token cost
// by tens of thousands of tokens (schema injection). Read operations
// don't need structured agent input — running a shell command is cheaper
// for the model and identical in output.
//
// MCP keeps only the write-style tools (store_observation, store_plan,
// store_plan_result, verify_plan, report_skill_execution); everything
// below is shell-only.

const intelMaxList = 15

func loadIntelGraph() (*graph.Graph, error) {
	path := filepath.Join(LocalDataDir(), "graph.json")
	g, err := loadGraph(path)
	if err != nil {
		return nil, fmt.Errorf("%w\nRun `universe init` first", err)
	}
	return g, nil
}

func formatNodeRef(n *models.Node) string {
	if n == nil {
		return ""
	}
	base := fmt.Sprintf("%s [%s] %s:%d", n.Name, n.Type, n.FilePath, n.StartLine)
	if n.Cluster != "" {
		base += " (" + n.Cluster + ")"
	}
	return base
}

// findIntelNode looks up a graph node by exact ID, then by name match.
// Returns the chosen node and a slice of close matches we used as
// fallback suggestions when nothing matches exactly.
func findIntelNode(g *graph.Graph, name string) (*models.Node, []*models.Node) {
	if n := g.GetNode(name); n != nil {
		return n, nil
	}
	results := g.Search(name)
	if len(results) == 0 {
		return nil, nil
	}
	for _, r := range results {
		if strings.EqualFold(r.Name, name) {
			return r, nil
		}
	}
	return nil, results
}

func printSuggestions(w *os.File, name string, matches []*models.Node) {
	fmt.Fprintf(w, "No exact match for %q.", name)
	if len(matches) == 0 {
		fmt.Fprintln(w, " Try `universe search <term>`.")
		return
	}
	fmt.Fprintln(w, " Closest matches:")
	for i, m := range matches {
		if i >= 5 {
			break
		}
		fmt.Fprintf(w, "  %s\n", formatNodeRef(m))
	}
}

// ──────────────────────────────────────────────────────────────────────
// universe query <name> — 360° view, replaces get_context MCP tool
// ──────────────────────────────────────────────────────────────────────

var intelQueryCmd = &cobra.Command{
	Use:   "query <name>",
	Short: "360° view of a symbol — callers, callees, flows, cluster, impact",
	Long: `Get complete context for one symbol in a single call.

Output is plain text, capped at ~2KB, designed for AI agents to read
cheaply. Source code is never printed — open the source file at the
file:line locations the output gives you if you need the body.

Examples:
  universe query StartTestServer
  universe query Foo`,
	Args: cobra.ExactArgs(1),
	RunE: runIntelQuery,
}

func runIntelQuery(_ *cobra.Command, args []string) error {
	g, err := loadIntelGraph()
	if err != nil {
		return err
	}
	node, matches := findIntelNode(g, args[0])
	if node == nil {
		printSuggestions(os.Stdout, args[0], matches)
		return nil
	}

	callers := g.GetDependents(node.ID)
	callees := g.GetDependencies(node.ID)

	fmt.Println(formatNodeRef(node))
	if node.Cluster != "" {
		fmt.Printf("Cluster: %s\n", node.Cluster)
	}
	fmt.Println()

	printRefList("Callers", callers, intelMaxList)
	fmt.Println()
	printRefList("Callees", callees, intelMaxList)

	if len(node.Flows) > 0 {
		fmt.Println()
		fmt.Printf("Flows: %s\n", strings.Join(node.Flows, ", "))
	}

	if sum := g.GetImpact(node.ID); sum != nil {
		fmt.Println()
		fmt.Printf("Impact: %s — %d affected node(s)\n", sum.RiskLevel, sum.TotalAffected)
		if sum.Summary != "" {
			fmt.Println(sum.Summary)
		}
	} else {
		risk := "low"
		switch {
		case len(callers) > 10:
			risk = "high"
		case len(callers) > 5:
			risk = "medium"
		}
		fmt.Println()
		fmt.Printf("Impact: %s — %d callers, %d callees\n", risk, len(callers), len(callees))
	}
	return nil
}

func printRefList(label string, nodes []*models.Node, max int) {
	if len(nodes) == 0 {
		fmt.Printf("%s: none\n", label)
		return
	}
	fmt.Printf("%s (%d):\n", label, len(nodes))
	refs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		refs = append(refs, formatNodeRef(n))
	}
	sort.Strings(refs)
	for i, r := range refs {
		if i >= max {
			fmt.Printf("  ...and %d more\n", len(refs)-max)
			return
		}
		fmt.Printf("  %s\n", r)
	}
}

// ──────────────────────────────────────────────────────────────────────
// universe deps <name> — callers + callees only, replaces get_dependencies
// ──────────────────────────────────────────────────────────────────────

var intelDepsCmd = &cobra.Command{
	Use:   "deps <name>",
	Short: "Callers and callees for a function — no flows, no impact",
	Args:  cobra.ExactArgs(1),
	RunE:  runIntelDeps,
}

func runIntelDeps(_ *cobra.Command, args []string) error {
	g, err := loadIntelGraph()
	if err != nil {
		return err
	}
	node, matches := findIntelNode(g, args[0])
	if node == nil {
		printSuggestions(os.Stdout, args[0], matches)
		return nil
	}
	callers := g.GetDependents(node.ID)
	callees := g.GetDependencies(node.ID)
	fmt.Println(formatNodeRef(node))
	fmt.Println()
	printRefList("Callers", callers, intelMaxList)
	fmt.Println()
	printRefList("Callees", callees, intelMaxList)
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// universe impact <name> — replaces get_impact_analysis
// ──────────────────────────────────────────────────────────────────────

var intelImpactCmd = &cobra.Command{
	Use:   "impact <name>",
	Short: "Blast radius — what breaks if this symbol changes",
	Args:  cobra.ExactArgs(1),
	RunE:  runIntelImpact,
}

func runIntelImpact(_ *cobra.Command, args []string) error {
	g, err := loadIntelGraph()
	if err != nil {
		return err
	}
	node, matches := findIntelNode(g, args[0])
	if node == nil {
		printSuggestions(os.Stdout, args[0], matches)
		return nil
	}

	// If any scoping flag was set, run a live filtered traversal so
	// the agent gets exactly the slice it asked for. Otherwise the
	// precomputed summary from `universe init` is fine.
	if intelMaxDepth != 3 || intelMinConfidence != 0.5 || intelImpactDir == "downstream" {
		direction := intelImpactDir
		if direction == "" {
			direction = "upstream"
		}
		var raw []graph.AffectedNode
		if direction == "downstream" {
			raw = g.GetDependenciesFiltered(node.ID, intelMinConfidence, intelMaxDepth)
		} else {
			raw = g.GetDependentsFiltered(node.ID, intelMinConfidence, intelMaxDepth)
		}
		byDepth := map[int][]models.Impact{}
		for _, a := range raw {
			byDepth[a.Depth] = append(byDepth[a.Depth], models.Impact{
				NodeID: a.Node.ID, Name: a.Node.Name,
				File: a.Node.FilePath, Line: a.Node.StartLine,
				Confidence: a.Confidence, Relation: string(a.Relation),
			})
		}
		risk := "low"
		switch {
		case len(raw) == 0:
			risk = "none"
		case len(raw) > 10:
			risk = "high"
		case len(raw) > 5:
			risk = "medium"
		}
		sum := &models.ImpactSummary{
			NodeID: node.ID, NodeName: node.Name,
			TotalAffected: len(raw), RiskLevel: risk, ByDepth: byDepth,
			Summary: fmt.Sprintf("%s affects %d node(s) at depth<=%d conf>=%.2f.",
				node.Name, len(raw), intelMaxDepth, intelMinConfidence),
		}
		fmt.Println(formatNodeRef(node))
		fmt.Printf("Risk: %s\n", sum.RiskLevel)
		fmt.Printf("Total affected: %d\n\n", sum.TotalAffected)
		printImpactBucket("WILL BREAK (depth 1)", sum.ByDepth[1])
		printImpactBucket("LIKELY AFFECTED (depth 2)", sum.ByDepth[2])
		printImpactBucket("POSSIBLY AFFECTED (depth 3)", sum.ByDepth[3])
		fmt.Println(sum.Summary)
		return nil
	}

	sum := g.GetImpact(node.ID)
	// Fall back to a quick BFS for nodes init didn't precompute (low
	// caller counts). Keeps the command useful for utility helpers.
	if sum == nil {
		sum = liveImpactCLI(g, node)
	}

	fmt.Println(formatNodeRef(node))
	fmt.Printf("Risk: %s\n", sum.RiskLevel)
	fmt.Printf("Total affected: %d\n\n", sum.TotalAffected)

	printImpactBucket("WILL BREAK (depth 1)", sum.ByDepth[1])
	printImpactBucket("LIKELY AFFECTED (depth 2)", sum.ByDepth[2])
	printImpactBucket("POSSIBLY AFFECTED (depth 3)", sum.ByDepth[3])

	if len(sum.AffectedFlows) > 0 {
		fmt.Printf("Affected flows: %s\n", strings.Join(sum.AffectedFlows, ", "))
	}
	if len(sum.AffectedClusters) > 0 {
		fmt.Printf("Affected clusters: %s\n", strings.Join(sum.AffectedClusters, ", "))
	}
	if sum.Summary != "" {
		fmt.Println()
		fmt.Println(sum.Summary)
	}
	return nil
}

func printImpactBucket(title string, items []models.Impact) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("%s:\n", title)
	for i, im := range items {
		if i >= intelMaxList {
			fmt.Printf("  ...and %d more\n", len(items)-intelMaxList)
			break
		}
		fmt.Printf("  %s %s:%d\n", im.Name, im.File, im.Line)
	}
	fmt.Println()
}

func liveImpactCLI(g *graph.Graph, root *models.Node) *models.ImpactSummary {
	visited := map[string]int{root.ID: 0}
	queue := []string{root.ID}
	byDepth := map[int][]models.Impact{}
	total := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		dist := visited[current]
		if dist >= 3 {
			continue
		}
		for _, c := range g.GetDependents(current) {
			if _, seen := visited[c.ID]; seen {
				continue
			}
			visited[c.ID] = dist + 1
			queue = append(queue, c.ID)
			byDepth[dist+1] = append(byDepth[dist+1], models.Impact{
				NodeID: c.ID, Name: c.Name, File: c.FilePath,
				Line: c.StartLine, Relation: "calls",
			})
			total++
		}
	}
	risk := "low"
	switch {
	case total == 0:
		risk = "none"
	case total > 10:
		risk = "high"
	case total > 5:
		risk = "medium"
	}
	return &models.ImpactSummary{
		NodeID: root.ID, NodeName: root.Name, TotalAffected: total,
		RiskLevel: risk, ByDepth: byDepth,
		Summary: fmt.Sprintf("%s affects %d node(s). Risk: %s.", root.Name, total, risk),
	}
}

// ──────────────────────────────────────────────────────────────────────
// universe search <term> — replaces search_graph
// ──────────────────────────────────────────────────────────────────────

var intelSearchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Find functions / types by name",
	Args:  cobra.ExactArgs(1),
	RunE:  runIntelSearch,
}

func runIntelSearch(_ *cobra.Command, args []string) error {
	g, err := loadIntelGraph()
	if err != nil {
		return err
	}
	term := args[0]
	tl := strings.ToLower(term)
	results := g.Search(term)
	if len(results) == 0 {
		fmt.Printf("No results for %q\n", term)
		return nil
	}

	type ranked struct {
		n     *models.Node
		order int
	}
	all := make([]ranked, 0, len(results))
	for _, n := range results {
		nl := strings.ToLower(n.Name)
		ord := 2
		switch {
		case nl == tl:
			ord = 0
		case strings.HasPrefix(nl, tl):
			ord = 1
		}
		all = append(all, ranked{n, ord})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].order != all[j].order {
			return all[i].order < all[j].order
		}
		return all[i].n.Name < all[j].n.Name
	})

	limit := 10
	if len(all) < limit {
		limit = len(all)
	}
	fmt.Printf("Results for %q (showing %d of %d):\n\n", term, limit, len(all))
	for i := 0; i < limit; i++ {
		n := all[i].n
		fmt.Printf("  %s — %d caller(s), %d callee(s)\n",
			formatNodeRef(n), n.CallerCount, n.CalleeCount)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// scoping flags
// ──────────────────────────────────────────────────────────────────────

// v0.2.8 added per-edge Confidence so the agent can ask for tightly-
// scoped traversals. These flags expose that on the shell side; the
// MCP tools take the same values via JSON input.
var (
	intelMaxDepth      int
	intelMinConfidence float64
	intelSearchLimit   int
	intelImpactDir     string
)

// ──────────────────────────────────────────────────────────────────────
// registration
// ──────────────────────────────────────────────────────────────────────

func init() {
	intelQueryCmd.Use = "query <name>"
	intelImpactCmd.Flags().IntVar(&intelMaxDepth, "max-depth", 3, "max traversal depth (1-5)")
	intelImpactCmd.Flags().Float64Var(&intelMinConfidence, "min-confidence", 0.5, "minimum edge confidence (0.0-1.0)")
	intelImpactCmd.Flags().StringVar(&intelImpactDir, "direction", "upstream", "upstream | downstream")
	intelDepsCmd.Flags().Float64Var(&intelMinConfidence, "min-confidence", 0.5, "minimum edge confidence (0.0-1.0)")
	intelSearchCmd.Flags().IntVar(&intelSearchLimit, "limit", 10, "max results (1-25)")

	rootCmd.AddCommand(intelQueryCmd)
	rootCmd.AddCommand(intelDepsCmd)
	rootCmd.AddCommand(intelImpactCmd)
	rootCmd.AddCommand(intelSearchCmd)
}
