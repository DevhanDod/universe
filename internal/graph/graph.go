package graph

import (
	"encoding/json"
	"os"
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
