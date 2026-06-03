package analyzer

import (
	"sort"
	"strings"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// DetectFlows traces execution paths from likely entry points (main, init,
// HTTP handlers, test functions, CLI commands) through `calls` edges.
//
// Each flow is one DFS from one entry point, capped at MaxFlowSteps so
// deeply-recursive graphs don't blow up the payload. Nodes are also stamped
// with the list of flows they participate in so MCP responses can answer
// "which flows touch this function?" without a second pass.
const MaxFlowSteps = 12

func DetectFlows(g *graph.Graph) []models.Flow {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}

	entries := findEntryPoints(g)
	flows := make([]models.Flow, 0, len(entries))

	for _, entryID := range entries {
		entry := g.Nodes[entryID]
		if entry == nil {
			continue
		}
		steps := traceFlow(g, entryID)
		if len(steps) == 0 {
			continue
		}

		clusters := uniqueClusters(steps)
		f := models.Flow{
			Name:       entry.Name,
			EntryPoint: entryID,
			Steps:      steps,
			StepCount:  len(steps),
			Clusters:   clusters,
		}
		flows = append(flows, f)

		// Stamp every node along the path with this flow's name.
		for _, s := range steps {
			n := g.Nodes[s.NodeID]
			if n == nil {
				continue
			}
			if !contains(n.Flows, f.Name) {
				n.Flows = append(n.Flows, f.Name)
			}
		}
	}

	sort.Slice(flows, func(i, j int) bool {
		if flows[i].StepCount != flows[j].StepCount {
			return flows[i].StepCount > flows[j].StepCount
		}
		return flows[i].Name < flows[j].Name
	})
	return flows
}

func findEntryPoints(g *graph.Graph) []string {
	out := []string{}
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		if n.Type != models.NodeFunction && n.Type != models.NodeMethod {
			continue
		}
		if isEntryPointName(n.Name) {
			out = append(out, id)
			continue
		}
		// Heuristic: a function with no callers but multiple callees often
		// looks like an entry point (cli command, top-level handler).
		if len(g.GetDependents(id)) == 0 && len(g.GetDependencies(id)) >= 2 {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func isEntryPointName(name string) bool {
	if name == "main" || name == "init" {
		return true
	}
	if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Example") {
		return true
	}
	if strings.HasSuffix(name, "Handler") || strings.HasSuffix(name, "Endpoint") {
		return true
	}
	if strings.HasPrefix(name, "Handle") {
		return true
	}
	return false
}

func traceFlow(g *graph.Graph, startID string) []models.FlowStep {
	steps := []models.FlowStep{}
	visited := map[string]struct{}{}

	var dfs func(id string, depth int)
	dfs = func(id string, depth int) {
		if depth >= MaxFlowSteps {
			return
		}
		if _, seen := visited[id]; seen {
			return
		}
		visited[id] = struct{}{}
		n := g.Nodes[id]
		if n == nil {
			return
		}
		steps = append(steps, models.FlowStep{
			NodeID:  id,
			Name:    n.Name,
			File:    n.FilePath,
			Line:    n.StartLine,
			Cluster: n.Cluster,
			StepNum: len(steps) + 1,
		})
		// Walk only `calls` edges so the flow stays an execution path.
		for _, e := range g.GetEdgesFrom(id) {
			if e.Type != models.EdgeCalls {
				continue
			}
			dfs(e.To, depth+1)
			if len(steps) >= MaxFlowSteps {
				return
			}
		}
	}

	dfs(startID, 0)
	return steps
}

func uniqueClusters(steps []models.FlowStep) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range steps {
		if s.Cluster == "" {
			continue
		}
		if _, ok := seen[s.Cluster]; ok {
			continue
		}
		seen[s.Cluster] = struct{}{}
		out = append(out, s.Cluster)
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
