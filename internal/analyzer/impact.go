package analyzer

import (
	"fmt"
	"sort"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// PrecomputeImpact walks the call graph upstream from each "important" node
// and stores a compact blast-radius summary. MCP's get_impact_analysis tool
// then returns the summary directly instead of re-doing BFS per request.
//
// Only nodes that look load-bearing (>= 3 callers OR participate in
// multiple flows) get precomputed — full precomputation would balloon
// graph.json for little gain on leaf utilities.
const (
	ImpactMaxDepth      = 3
	ImpactMinCallers    = 3
	ImpactMaxPerDepth   = 10
)

func PrecomputeImpact(g *graph.Graph) map[string]*models.ImpactSummary {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}

	// Stamp caller/callee counts on every node — cheap and useful for
	// MCP responses even on nodes we don't precompute impact for.
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		n.CallerCount = len(g.GetDependents(id))
		n.CalleeCount = len(g.GetDependencies(id))
	}

	out := map[string]*models.ImpactSummary{}
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		if n.CallerCount < ImpactMinCallers && len(n.Flows) < 2 {
			continue
		}
		out[id] = buildImpactSummary(g, n)
	}
	return out
}

func buildImpactSummary(g *graph.Graph, root *models.Node) *models.ImpactSummary {
	visited := map[string]int{root.ID: 0}
	queue := []string{root.ID}
	byDepth := map[int][]models.Impact{}
	affectedFlows := map[string]struct{}{}
	affectedClusters := map[string]struct{}{}
	total := 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		dist := visited[current]
		if dist >= ImpactMaxDepth {
			continue
		}
		for _, c := range g.GetDependents(current) {
			if _, seen := visited[c.ID]; seen {
				continue
			}
			visited[c.ID] = dist + 1
			queue = append(queue, c.ID)

			depth := dist + 1
			confidence := 1.0 - float64(depth-1)*0.25
			if confidence < 0.25 {
				confidence = 0.25
			}
			byDepth[depth] = append(byDepth[depth], models.Impact{
				NodeID:     c.ID,
				Name:       c.Name,
				File:       c.FilePath,
				Line:       c.StartLine,
				Confidence: confidence,
				Relation:   "calls",
			})
			total++
			for _, f := range c.Flows {
				affectedFlows[f] = struct{}{}
			}
			if c.Cluster != "" {
				affectedClusters[c.Cluster] = struct{}{}
			}
		}
	}

	// Cap each depth bucket to keep tool responses small.
	for d := range byDepth {
		sort.Slice(byDepth[d], func(i, j int) bool {
			return byDepth[d][i].Name < byDepth[d][j].Name
		})
		if len(byDepth[d]) > ImpactMaxPerDepth {
			byDepth[d] = byDepth[d][:ImpactMaxPerDepth]
		}
	}

	risk := riskLevelFor(total)
	return &models.ImpactSummary{
		NodeID:           root.ID,
		NodeName:         root.Name,
		TotalAffected:    total,
		RiskLevel:        risk,
		ByDepth:          byDepth,
		AffectedFlows:    keysSorted(affectedFlows),
		AffectedClusters: keysSorted(affectedClusters),
		Summary:          impactSummaryText(root.Name, total, risk),
	}
}

func riskLevelFor(total int) string {
	switch {
	case total == 0:
		return "none"
	case total <= 2:
		return "low"
	case total <= 5:
		return "medium"
	default:
		return "high"
	}
}

func impactSummaryText(name string, total int, risk string) string {
	if total == 0 {
		return fmt.Sprintf("%s has no detected callers. Safe to change.", name)
	}
	return fmt.Sprintf("Changing %s affects %d node(s). Risk: %s.", name, total, risk)
}

func keysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
