package graph

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Universe/universe/internal/models"
)

type Graph struct {
	mu       sync.RWMutex
	Nodes    map[string]*models.Node     `json:"nodes"`
	Edges    []*models.Edge              `json:"edges"`
	Files    map[string]*models.FileInfo `json:"files"`
	Coverage models.Coverage             `json:"coverage"`

	// Precomputed at index time. MCP tools read these directly so the agent
	// gets cluster/flow/impact context without follow-up tool calls.
	Clusters []models.Cluster                  `json:"clusters,omitempty"`
	Flows    []models.Flow                     `json:"flows,omitempty"`
	Impact   map[string]*models.ImpactSummary  `json:"impact,omitempty"`
}

func (g *Graph) SetClusters(c []models.Cluster) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Clusters = c
}

func (g *Graph) SetFlows(f []models.Flow) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Flows = f
}

func (g *Graph) SetImpact(im map[string]*models.ImpactSummary) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Impact = im
}

func (g *Graph) GetImpact(id string) *models.ImpactSummary {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.Impact == nil {
		return nil
	}
	return g.Impact[id]
}

func (g *Graph) SetCoverage(c models.Coverage) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Coverage = c
}

func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[string]*models.Node),
		Edges: make([]*models.Edge, 0),
		Files: make(map[string]*models.FileInfo),
	}
}

func (g *Graph) AddNode(node *models.Node) {
	if node == nil || node.ID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Nodes[node.ID] = node
}

func (g *Graph) AddEdge(edge *models.Edge) {
	if edge == nil || edge.From == "" || edge.To == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Edges = append(g.Edges, edge)
}

func (g *Graph) AddFile(info *models.FileInfo) {
	if info == nil || info.Path == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Files[info.Path] = info
}

func (g *Graph) GetNode(id string) *models.Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.Nodes[id]
}

func isDependencyEdge(t models.EdgeType) bool {
	switch t {
	case models.EdgeImports, models.EdgeCalls, models.EdgeDependsOn:
		return true
	default:
		return false
	}
}

func (g *Graph) GetDependents(nodeID string) []*models.Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	seen := make(map[string]struct{})
	var out []*models.Node
	for _, e := range g.Edges {
		if e == nil || e.To != nodeID || !isDependencyEdge(e.Type) {
			continue
		}
		if _, ok := seen[e.From]; ok {
			continue
		}
		if n := g.Nodes[e.From]; n != nil {
			seen[e.From] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

func (g *Graph) GetDependencies(nodeID string) []*models.Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	seen := make(map[string]struct{})
	var out []*models.Node
	for _, e := range g.Edges {
		if e == nil || e.From != nodeID {
			continue
		}
		if _, ok := seen[e.To]; ok {
			continue
		}
		if n := g.Nodes[e.To]; n != nil {
			seen[e.To] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

func (g *Graph) GetByType(nodeType models.NodeType) []*models.Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*models.Node
	for _, n := range g.Nodes {
		if n != nil && n.Type == nodeType {
			out = append(out, n)
		}
	}
	return out
}

func (g *Graph) GetEdgesFrom(nodeID string) []*models.Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*models.Edge
	for _, e := range g.Edges {
		if e != nil && e.From == nodeID {
			out = append(out, e)
		}
	}
	return out
}

func (g *Graph) GetEdgesTo(nodeID string) []*models.Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*models.Edge
	for _, e := range g.Edges {
		if e != nil && e.To == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// AffectedNode is one node in a confidence-filtered traversal, with the
// depth at which it was discovered and the edge confidence that got us
// here. Used by Get*Filtered to give MCP responses a graded view of the
// blast radius.
type AffectedNode struct {
	Node       *models.Node
	Depth      int
	Confidence float64
	Relation   models.EdgeType
}

func effectiveConfidence(e *models.Edge) float64 {
	if e.Confidence <= 0 {
		// Older graphs (pre-v0.2.8) stored no confidence — treat as
		// fully trusted so existing data keeps working.
		return 1.0
	}
	return e.Confidence
}

// GetDependentsFiltered walks reverse edges from nodeID, keeping only
// edges whose confidence is >= minConfidence, up to maxDepth levels.
// Returned nodes are sorted by (depth asc, confidence desc).
func (g *Graph) GetDependentsFiltered(nodeID string, minConfidence float64, maxDepth int) []AffectedNode {
	return g.bfsFiltered(nodeID, minConfidence, maxDepth, true)
}

// GetDependenciesFiltered walks forward edges from nodeID (what this node
// uses), same shape as GetDependentsFiltered.
func (g *Graph) GetDependenciesFiltered(nodeID string, minConfidence float64, maxDepth int) []AffectedNode {
	return g.bfsFiltered(nodeID, minConfidence, maxDepth, false)
}

func (g *Graph) bfsFiltered(nodeID string, minConfidence float64, maxDepth int, reverse bool) []AffectedNode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if maxDepth <= 0 {
		maxDepth = 3
	}
	visited := map[string]int{nodeID: 0}
	queue := []string{nodeID}
	var out []AffectedNode

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		dist := visited[current]
		if dist >= maxDepth {
			continue
		}
		for _, e := range g.Edges {
			if e == nil {
				continue
			}
			var nextID string
			if reverse {
				if e.To != current || !isDependencyEdge(e.Type) {
					continue
				}
				nextID = e.From
			} else {
				if e.From != current {
					continue
				}
				nextID = e.To
			}
			conf := effectiveConfidence(e)
			if conf < minConfidence {
				continue
			}
			if _, seen := visited[nextID]; seen {
				continue
			}
			visited[nextID] = dist + 1
			queue = append(queue, nextID)
			if n := g.Nodes[nextID]; n != nil {
				out = append(out, AffectedNode{
					Node: n, Depth: dist + 1, Confidence: conf, Relation: e.Type,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

// SearchNodes does a relevance-ranked name/file/package search.
// Used by tools_read.go and tools_unified.go so MCP tools and shell
// commands share a single sort order.
func (g *Graph) SearchNodes(query string, limit int) []*models.Node {
	if limit <= 0 {
		limit = 10
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	type ranked struct {
		n     *models.Node
		score int
	}
	var all []ranked
	for _, n := range g.Nodes {
		if n == nil {
			continue
		}
		name := strings.ToLower(n.Name)
		switch {
		case name == q:
			all = append(all, ranked{n, 0})
		case strings.HasPrefix(name, q):
			all = append(all, ranked{n, 1})
		case strings.Contains(name, q):
			all = append(all, ranked{n, 2})
		case strings.Contains(strings.ToLower(n.FilePath), q):
			all = append(all, ranked{n, 3})
		case strings.Contains(strings.ToLower(n.Package), q):
			all = append(all, ranked{n, 4})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score < all[j].score
		}
		return all[i].n.Name < all[j].n.Name
	})
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]*models.Node, 0, len(all))
	for _, r := range all {
		out = append(out, r.n)
	}
	return out
}

func (g *Graph) Search(query string) []*models.Node {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*models.Node
	for _, n := range g.Nodes {
		if n == nil {
			continue
		}
		if strings.Contains(strings.ToLower(n.Name), q) {
			out = append(out, n)
		}
	}
	return out
}

func (g *Graph) ExportJSON(filePath string) error {
	g.mu.RLock()
	data, err := json.MarshalIndent(g, "", "  ")
	g.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (g *Graph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	stats := GraphStats{
		TotalNodes:  len(g.Nodes),
		TotalEdges:  len(g.Edges),
		NodesByType: make(map[string]int),
		EdgesByType: make(map[string]int),
	}
	packages := make(map[string]struct{})
	files := make(map[string]struct{})
	for _, n := range g.Nodes {
		if n == nil {
			continue
		}
		stats.NodesByType[string(n.Type)]++
		if n.Package != "" {
			packages[n.Package] = struct{}{}
		}
		if n.FilePath != "" {
			files[n.FilePath] = struct{}{}
		}
	}
	for _, e := range g.Edges {
		if e == nil {
			continue
		}
		stats.EdgesByType[string(e.Type)]++
	}
	stats.PackageCount = len(packages)
	stats.FileCount = len(files)
	stats.Coverage = g.Coverage
	return stats
}

type GraphStats struct {
	TotalNodes   int             `json:"total_nodes"`
	TotalEdges   int             `json:"total_edges"`
	NodesByType  map[string]int  `json:"nodes_by_type"`
	EdgesByType  map[string]int  `json:"edges_by_type"`
	PackageCount int             `json:"package_count"`
	FileCount    int             `json:"file_count"`
	Coverage     models.Coverage `json:"coverage"`
}
