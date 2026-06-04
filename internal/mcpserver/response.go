package mcpserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// Response formatting lives in one place so every MCP read tool ships
// the same shape. The agent learns ONE format ("Name [type] file:line
// (cluster) [conf%]") and reads it back the same way regardless of
// which tool produced it.
//
// All limits and budgets are deliberately small. The whole point of the
// v0.2.8 swing back to MCP is that the agent calls structured tools
// instead of slurping files — we can't undo that win by returning
// 5KB of JSON per call.

const (
	defaultMaxNodes  = 15
	defaultMaxTokens = 800 // approximation: 1 token ≈ 4 chars
)

// ResponseConfig controls per-call format. Tools that want different
// behavior (more depth, no flows, etc.) pass an overridden copy.
type ResponseConfig struct {
	MaxNodes      int
	MaxTokens     int
	IncludeFlows  bool
	IncludeImpact bool
	IncludeConf   bool // append [85%] to node refs
}

func DefaultResponseConfig() ResponseConfig {
	return ResponseConfig{
		MaxNodes:      defaultMaxNodes,
		MaxTokens:     defaultMaxTokens,
		IncludeFlows:  true,
		IncludeImpact: true,
		IncludeConf:   true,
	}
}

// FormatNodeRef builds the one-line node string used everywhere.
// The optional confidence is appended as " [N%]" when >0; we never
// print [100%] for plain unscored AST edges so the agent doesn't have
// to filter cosmetic noise.
func FormatNodeRef(n *models.Node, conf float64, includeConf bool) string {
	if n == nil {
		return ""
	}
	s := fmt.Sprintf("%s [%s] %s:%d", n.Name, n.Type, n.FilePath, n.StartLine)
	if n.Cluster != "" {
		s += " (" + n.Cluster + ")"
	}
	if includeConf && conf > 0 && conf < 1.0 {
		s += fmt.Sprintf(" [%.0f%%]", conf*100)
	}
	return s
}

// FormatNodeResponse builds the full "context" block for one node:
// header, callers, callees, flows, impact. Used by universe_query
// and as the graph section of universe_context.
func FormatNodeResponse(node *models.Node, g *graph.Graph, cfg ResponseConfig) string {
	var b strings.Builder
	b.WriteString(FormatNodeRef(node, 1.0, false))
	b.WriteString("\n")

	if cfg.IncludeImpact {
		callers := g.GetDependents(node.ID)
		callees := g.GetDependencies(node.ID)
		writeRefList(&b, "Callers", callers, cfg)
		writeRefList(&b, "Callees", callees, cfg)
	}

	if cfg.IncludeFlows && len(node.Flows) > 0 {
		flows := node.Flows
		if len(flows) > 5 {
			flows = append([]string{}, flows[:5]...)
			flows = append(flows, fmt.Sprintf("…(+%d)", len(node.Flows)-5))
		}
		fmt.Fprintf(&b, "Flows: %s\n", strings.Join(flows, ", "))
	}

	if cfg.IncludeImpact {
		if sum := g.GetImpact(node.ID); sum != nil {
			fmt.Fprintf(&b, "Impact: %s — %d affected node(s)\n", sum.RiskLevel, sum.TotalAffected)
		}
	}
	return truncateToBudget(b.String(), cfg.MaxTokens)
}

func writeRefList(b *strings.Builder, label string, nodes []*models.Node, cfg ResponseConfig) {
	if len(nodes) == 0 {
		return
	}
	refs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		refs = append(refs, FormatNodeRef(n, 0, false))
	}
	sort.Strings(refs)
	max := cfg.MaxNodes
	if max <= 0 {
		max = defaultMaxNodes
	}
	if len(refs) <= max {
		fmt.Fprintf(b, "%s (%d): %s\n", label, len(refs), strings.Join(refs, "; "))
		return
	}
	fmt.Fprintf(b, "%s (%d): %s …(+%d)\n",
		label, len(refs), strings.Join(refs[:max], "; "), len(refs)-max)
}

// FormatNodeList renders a search-result list. One short line per node
// keeps the response compact even when relevance is mid.
func FormatNodeList(nodes []*models.Node, query string, cfg ResponseConfig) string {
	if len(nodes) == 0 {
		return fmt.Sprintf("No results for %q.\n", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Results for %q (%d):\n", query, len(nodes))
	max := cfg.MaxNodes
	if max <= 0 {
		max = defaultMaxNodes
	}
	for i, n := range nodes {
		if i >= max {
			fmt.Fprintf(&b, "…and %d more\n", len(nodes)-max)
			break
		}
		fmt.Fprintf(&b, "  %s — %d caller(s), %d callee(s)\n",
			FormatNodeRef(n, 0, false), n.CallerCount, n.CalleeCount)
	}
	return truncateToBudget(b.String(), cfg.MaxTokens)
}

// FormatImpactResponse groups blast-radius results by depth so the
// agent sees "WILL BREAK" first and stops reading once it has enough.
func FormatImpactResponse(target *models.Node, affected []graph.AffectedNode, cfg ResponseConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Target: %s\n", FormatNodeRef(target, 1.0, false))
	fmt.Fprintf(&b, "Affected: %d node(s)\n\n", len(affected))

	byDepth := map[int][]graph.AffectedNode{}
	for _, a := range affected {
		byDepth[a.Depth] = append(byDepth[a.Depth], a)
	}
	labels := map[int]string{
		1: "WILL BREAK (direct callers)",
		2: "LIKELY AFFECTED (depth 2)",
		3: "POSSIBLY AFFECTED (depth 3)",
	}
	max := cfg.MaxNodes
	if max <= 0 {
		max = defaultMaxNodes
	}
	for d := 1; d <= 3; d++ {
		items := byDepth[d]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s:\n", labels[d])
		for i, a := range items {
			if i >= max {
				fmt.Fprintf(&b, "  …and %d more\n", len(items)-max)
				break
			}
			fmt.Fprintf(&b, "  %s\n", FormatNodeRef(a.Node, a.Confidence, cfg.IncludeConf))
		}
		b.WriteString("\n")
	}
	return truncateToBudget(b.String(), cfg.MaxTokens)
}

// truncateToBudget chops the tail of a response if it overshoots the
// ~tokens limit. Tokens are estimated at 4 chars each — coarse, but
// fine for protecting against runaway lists.
func truncateToBudget(s string, maxTokens int) string {
	if maxTokens <= 0 {
		return s
	}
	maxChars := maxTokens * 4
	if len(s) <= maxChars {
		return s
	}
	// Trim at a line boundary so the agent doesn't read half a ref.
	cut := strings.LastIndex(s[:maxChars], "\n")
	if cut <= 0 {
		cut = maxChars
	}
	return s[:cut] + "\n…(truncated)\n"
}
