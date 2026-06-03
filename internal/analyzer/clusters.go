package analyzer

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// DetectClusters groups graph nodes into functional communities using
// a simple label-propagation pass over call/import/depends-on edges.
//
// Seed labels: each node starts in a cluster named after its package
// (or for files, the parent directory). Then each node adopts the most
// common label among its graph neighbours. We iterate a few times — by
// then labels stabilise on dense subgraphs (test infra, auth, db, …)
// while disconnected pieces keep their package label.
//
// We also tag every node in place with its cluster so MCP responses can
// answer "what cluster does this belong to" without a follow-up lookup.
func DetectClusters(g *graph.Graph) []models.Cluster {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}

	// Seed labels from package / directory.
	label := make(map[string]string, len(g.Nodes))
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		label[id] = seedClusterLabel(n)
	}

	// Build adjacency from edges we care about. Ignore edges to/from nodes
	// that aren't in the graph (defensive — extractors sometimes emit refs
	// to symbols we never indexed).
	adj := make(map[string][]string, len(g.Nodes))
	for _, e := range g.Edges {
		if e == nil || !isClusterEdge(e.Type) {
			continue
		}
		if _, ok := g.Nodes[e.From]; !ok {
			continue
		}
		if _, ok := g.Nodes[e.To]; !ok {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	// Up to 5 sweeps of label propagation. Process in a stable order so
	// the result is deterministic across runs.
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for iter := 0; iter < 5; iter++ {
		changed := false
		for _, id := range ids {
			neighbours := adj[id]
			if len(neighbours) == 0 {
				continue
			}
			counts := map[string]int{}
			for _, nid := range neighbours {
				counts[label[nid]]++
			}
			best := label[id]
			bestCount := counts[best]
			for lbl, c := range counts {
				if c > bestCount || (c == bestCount && lbl < best) {
					best = lbl
					bestCount = c
				}
			}
			if best != label[id] {
				label[id] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Assemble clusters from the final labelling, and stamp every node
	// with its cluster name.
	byLabel := map[string][]string{}
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		lbl := label[id]
		n.Cluster = lbl
		byLabel[lbl] = append(byLabel[lbl], id)
	}

	out := make([]models.Cluster, 0, len(byLabel))
	for lbl, members := range byLabel {
		sort.Strings(members)
		c := models.Cluster{
			Name:      lbl,
			NodeCount: len(members),
			NodeIDs:   members,
		}
		c.KeyFiles = keyFilesForCluster(g, members)
		c.EntryPoints = entryPointsForCluster(g, members, label)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NodeCount != out[j].NodeCount {
			return out[i].NodeCount > out[j].NodeCount
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func isClusterEdge(t models.EdgeType) bool {
	switch t {
	case models.EdgeCalls, models.EdgeImports, models.EdgeDependsOn, models.EdgeContains:
		return true
	}
	return false
}

func seedClusterLabel(n *models.Node) string {
	if n.Package != "" {
		return n.Package
	}
	if n.FilePath != "" {
		dir := filepath.ToSlash(filepath.Dir(n.FilePath))
		if dir == "" || dir == "." {
			return "root"
		}
		// Use the leaf directory — short, readable.
		parts := strings.Split(dir, "/")
		return parts[len(parts)-1]
	}
	return "unknown"
}

func keyFilesForCluster(g *graph.Graph, memberIDs []string) []string {
	// "Key" = files with the most members from this cluster.
	counts := map[string]int{}
	for _, id := range memberIDs {
		n := g.Nodes[id]
		if n == nil || n.FilePath == "" {
			continue
		}
		counts[n.FilePath]++
	}
	type fc struct {
		file  string
		count int
	}
	all := make([]fc, 0, len(counts))
	for f, c := range counts {
		all = append(all, fc{f, c})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count != all[j].count {
			return all[i].count > all[j].count
		}
		return all[i].file < all[j].file
	})
	out := make([]string, 0, 5)
	for i := 0; i < len(all) && i < 5; i++ {
		out = append(out, all[i].file)
	}
	return out
}

func entryPointsForCluster(g *graph.Graph, memberIDs []string, label map[string]string) []string {
	// A member is an entry point if at least one caller lives in a
	// different cluster — that's how external code reaches into this one.
	memberSet := make(map[string]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		memberSet[id] = struct{}{}
	}
	out := []string{}
	for _, id := range memberIDs {
		callers := g.GetDependents(id)
		for _, c := range callers {
			if _, ok := memberSet[c.ID]; ok {
				continue
			}
			out = append(out, id)
			break
		}
	}
	sort.Strings(out)
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}
