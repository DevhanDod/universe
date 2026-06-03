package report

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// GenerateReport writes UNIVERSE_REPORT.md from the graph's precomputed
// clusters, flows, and impact data. The file is meant to be read by the
// agent for a codebase overview without invoking any tool — replacing
// the cost of multiple MCP get_context calls for the broad "what is
// this codebase" question.
//
// Target size: 3–8KB. We cap every list aggressively because a 50KB
// "report" defeats its own purpose.
func GenerateReport(g *graph.Graph, outputPath string) error {
	if g == nil {
		return fmt.Errorf("graph is nil")
	}

	stats := g.Stats()
	var b strings.Builder

	fmt.Fprintf(&b, "# Universe — Codebase Intelligence Report\n\n")
	fmt.Fprintf(&b, "Generated: %s | Nodes: %d | Edges: %d | Files: %d | Packages: %d\n\n",
		time.Now().Format("2006-01-02 15:04"),
		stats.TotalNodes, stats.TotalEdges, stats.FileCount, stats.PackageCount)

	writeClusters(&b, g)
	writeGodNodes(&b, g)
	writeFlows(&b, g)
	writeAgentGuide(&b)

	return os.WriteFile(outputPath, []byte(b.String()), 0o644)
}

func writeClusters(b *strings.Builder, g *graph.Graph) {
	if len(g.Clusters) == 0 {
		return
	}
	fmt.Fprintln(b, "## Clusters")
	fmt.Fprintln(b, "")
	fmt.Fprintln(b, "| Cluster | Nodes | Key files | Entry points |")
	fmt.Fprintln(b, "|---------|-------|-----------|--------------|")
	max := 10
	for i, c := range g.Clusters {
		if i >= max {
			fmt.Fprintf(b, "_…and %d more clusters_\n", len(g.Clusters)-max)
			break
		}
		keyFiles := truncatedJoin(c.KeyFiles, 3, ", ")
		entries := truncatedJoin(c.EntryPoints, 3, ", ")
		if entries == "" {
			entries = "—"
		}
		fmt.Fprintf(b, "| %s | %d | %s | %s |\n", c.Name, c.NodeCount, keyFiles, entries)
	}
	fmt.Fprintln(b, "")
}

func writeGodNodes(b *strings.Builder, g *graph.Graph) {
	type ranked struct {
		node *models.Node
		risk string
	}
	var top []ranked
	for _, n := range g.Nodes {
		if n == nil || n.CallerCount < 3 {
			continue
		}
		risk := "low"
		switch {
		case n.CallerCount > 10:
			risk = "high"
		case n.CallerCount > 5:
			risk = "medium"
		}
		top = append(top, ranked{n, risk})
	}
	sort.Slice(top, func(i, j int) bool {
		return top[i].node.CallerCount > top[j].node.CallerCount
	})
	if len(top) == 0 {
		return
	}
	fmt.Fprintln(b, "## High-Impact Nodes")
	fmt.Fprintln(b, "")
	fmt.Fprintln(b, "Functions and types with many incoming callers. Change carefully.")
	fmt.Fprintln(b, "")
	max := 10
	for i, r := range top {
		if i >= max {
			break
		}
		fmt.Fprintf(b, "- **%s** (%s:%d) — %d callers, %d callees, risk: %s\n",
			r.node.Name, r.node.FilePath, r.node.StartLine,
			r.node.CallerCount, r.node.CalleeCount, r.risk)
	}
	fmt.Fprintln(b, "")
}

func writeFlows(b *strings.Builder, g *graph.Graph) {
	if len(g.Flows) == 0 {
		return
	}
	fmt.Fprintln(b, "## Key Execution Flows")
	fmt.Fprintln(b, "")
	max := 8
	for i, f := range g.Flows {
		if i >= max {
			fmt.Fprintf(b, "_…and %d more flows_\n", len(g.Flows)-max)
			break
		}
		names := make([]string, 0, len(f.Steps))
		for j, s := range f.Steps {
			if j >= 6 {
				names = append(names, fmt.Sprintf("…(+%d)", len(f.Steps)-6))
				break
			}
			names = append(names, s.Name)
		}
		fmt.Fprintf(b, "%d. **%s** — %s\n", i+1, f.Name, strings.Join(names, " → "))
	}
	fmt.Fprintln(b, "")
}

func writeAgentGuide(b *strings.Builder) {
	fmt.Fprintln(b, "## For AI Agents")
	fmt.Fprintln(b, "")
	fmt.Fprintln(b, "To explore the codebase, run these in the terminal:")
	fmt.Fprintln(b, "")
	fmt.Fprintln(b, "    universe query <name>       full context (callers, callees, flows, impact)")
	fmt.Fprintln(b, "    universe deps <name>        callers + callees only")
	fmt.Fprintln(b, "    universe impact <name>      blast radius for planned changes")
	fmt.Fprintln(b, "    universe search <term>      find symbols by name")
	fmt.Fprintln(b, "    universe recall <query>     past session memory")
	fmt.Fprintln(b, "    universe skills find <q>    matching skill recipes")
	fmt.Fprintln(b, "    universe plans get          latest pending plan")
	fmt.Fprintln(b, "    universe cost               cost savings summary")
	fmt.Fprintln(b, "")
	fmt.Fprintln(b, "Do NOT read .universe/graph.json directly.")
}

func truncatedJoin(items []string, max int, sep string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) <= max {
		return strings.Join(items, sep)
	}
	return strings.Join(items[:max], sep) + fmt.Sprintf("%s…(+%d)", sep, len(items)-max)
}
