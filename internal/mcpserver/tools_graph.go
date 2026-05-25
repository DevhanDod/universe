package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/models"
)

// NodeInfo is the MCP-facing representation of a graph node.
type NodeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Package   string `json:"package"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
}

// ============================================================
// Tool: get_dependencies
// ============================================================

type GetDependenciesInput struct {
	Name  string `json:"name" jsonschema:"required,description=Function or type name to look up (e.g. ValidateToken or auth.ValidateToken)"`
	Depth int    `json:"depth,omitempty" jsonschema:"description=How many levels deep to traverse (default 1)"`
}

type GetDependenciesOutput struct {
	Node     *NodeInfo  `json:"node"`
	Callers  []NodeInfo `json:"callers"`
	Callees  []NodeInfo `json:"callees"`
	Message  string     `json:"message,omitempty"`
}

func (h *Handlers) HandleGetDependencies(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetDependenciesInput,
) (*mcp.CallToolResult, GetDependenciesOutput, error) {
	if h.graph == nil {
		return nil, GetDependenciesOutput{Message: "Graph not loaded. Run 'universe analyze <repo>' first."}, nil
	}
	if input.Depth == 0 {
		input.Depth = 1
	}

	node := findNodeByName(h, input.Name)
	if node == nil {
		return nil, GetDependenciesOutput{
			Message: "No function or type found matching '" + input.Name + "'. Try search_graph to browse.",
		}, nil
	}

	callers := h.graph.GetDependents(node.ID)
	callees := h.graph.GetDependencies(node.ID)

	ni := toNodeInfo(node)
	out := GetDependenciesOutput{
		Node:    &ni,
		Callers: toNodeInfoList(callers),
		Callees: toNodeInfoList(callees),
	}

	if h.sessionMgr != nil {
		h.sessionMgr.OnToolCall("", "get_dependencies", node.ID, input.Name, "", true, "")
	}

	return nil, out, nil
}

// ============================================================
// Tool: get_impact_analysis
// ============================================================

type ImpactAnalysisInput struct {
	Name string `json:"name" jsonschema:"required,description=Function or type name to analyze impact for"`
}

type ImpactAnalysisOutput struct {
	RootNode      *NodeInfo      `json:"root_node"`
	AffectedNodes []AffectedNode `json:"affected_nodes"`
	RiskLevel     string         `json:"risk_level"`
	Summary       string         `json:"summary"`
}

type AffectedNode struct {
	NodeInfo
	Impact   string `json:"impact"`
	Distance int    `json:"distance"`
}

func (h *Handlers) HandleGetImpactAnalysis(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ImpactAnalysisInput,
) (*mcp.CallToolResult, ImpactAnalysisOutput, error) {
	if h.graph == nil {
		return nil, ImpactAnalysisOutput{Summary: "Graph not loaded. Run 'universe analyze <repo>' first."}, nil
	}

	node := findNodeByName(h, input.Name)
	if node == nil {
		return nil, ImpactAnalysisOutput{
			Summary: "No function or type found matching '" + input.Name + "'.",
		}, nil
	}

	// BFS over caller edges to find everything that depends on this node
	visited := map[string]int{node.ID: 0}
	queue := []string{node.ID}
	var affected []AffectedNode

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		dist := visited[current]
		if dist >= 3 { // cap BFS at 3 hops
			continue
		}
		callers := h.graph.GetDependents(current)
		for _, c := range callers {
			if _, seen := visited[c.ID]; seen {
				continue
			}
			visited[c.ID] = dist + 1
			queue = append(queue, c.ID)
			impact := "indirect"
			if dist+1 == 1 {
				impact = "direct"
			}
			ni := toNodeInfo(c)
			affected = append(affected, AffectedNode{NodeInfo: ni, Impact: impact, Distance: dist + 1})
		}
	}

	riskLevel := riskFor(len(affected))
	ni := toNodeInfo(node)
	return nil, ImpactAnalysisOutput{
		RootNode:      &ni,
		AffectedNodes: affected,
		RiskLevel:     riskLevel,
		Summary:       riskSummary(input.Name, len(affected), riskLevel),
	}, nil
}

// ============================================================
// Tool: search_graph
// ============================================================

type SearchGraphInput struct {
	Query string `json:"query" jsonschema:"required,description=Search term — function name, type name, or package name"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Max results (default 10)"`
}

type SearchGraphOutput struct {
	Results []NodeInfo `json:"results"`
	Total   int        `json:"total"`
}

func (h *Handlers) HandleSearchGraph(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SearchGraphInput,
) (*mcp.CallToolResult, SearchGraphOutput, error) {
	if h.graph == nil {
		return nil, SearchGraphOutput{}, nil
	}
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}

	nodes := h.graph.Search(input.Query)

	// Rank: exact name match first, then prefix, then contains
	var exact, prefix, rest []NodeInfo
	q := strings.ToLower(input.Query)
	for _, n := range nodes {
		ni := toNodeInfo(n)
		nl := strings.ToLower(n.Name)
		switch {
		case nl == q:
			exact = append(exact, ni)
		case strings.HasPrefix(nl, q):
			prefix = append(prefix, ni)
		default:
			rest = append(rest, ni)
		}
	}
	ranked := append(append(exact, prefix...), rest...)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return nil, SearchGraphOutput{Results: ranked, Total: len(nodes)}, nil
}

// ============================================================
// helpers
// ============================================================

// findNodeByName searches the graph for the best match for a name.
// Tries exact ID, then "package.Name", then name-only search.
func findNodeByName(h *Handlers, name string) *models.Node {
	// Try direct ID lookup first
	if n := h.graph.GetNode(name); n != nil {
		return n
	}
	// Fall back to name search
	results := h.graph.Search(name)
	if len(results) == 0 {
		return nil
	}
	// Prefer exact name match
	for _, n := range results {
		if strings.EqualFold(n.Name, name) {
			return n
		}
	}
	return results[0]
}

func toNodeInfo(n *models.Node) NodeInfo {
	return NodeInfo{
		ID:        n.ID,
		Name:      n.Name,
		Kind:      string(n.Type),
		Package:   n.Package,
		File:      n.FilePath,
		StartLine: n.StartLine,
	}
}

func toNodeInfoList(nodes []*models.Node) []NodeInfo {
	out := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			out = append(out, toNodeInfo(n))
		}
	}
	return out
}

func riskFor(count int) string {
	switch {
	case count <= 2:
		return "low"
	case count <= 5:
		return "medium"
	default:
		return "high"
	}
}

func riskSummary(name string, count int, risk string) string {
	if count == 0 {
		return "Changing " + name + " has no detected callers. Safe to modify."
	}
	return "Changing " + name + " directly or indirectly affects " +
		itoa(count) + " node(s). Risk: " + risk + "."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
