package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/models"
)

// Hard caps on MCP response sizes. The tokens an agent has to read on every
// follow-up turn grow with every item we return, so cap aggressively and
// let the agent ask for more if it actually needs it.
const (
	MaxCallersInResponse = 15
	MaxCalleesInResponse = 15
	MaxFlowsInResponse   = 5
	MaxClusterNeighbors  = 5
	MaxSearchResults     = 10
)

// NodeInfo is the legacy compact node representation. Older tests still
// reference it; new tools return formatted ref strings via formatRef.
type NodeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Package   string `json:"package"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
}

// formatRef is the one-line representation used in every list response.
// Example: "StartTestServer [function] test_helpers.go:42 (test-infra)".
func formatRef(n *models.Node) string {
	if n == nil {
		return ""
	}
	base := fmt.Sprintf("%s [%s] %s:%d", n.Name, n.Type, n.FilePath, n.StartLine)
	if n.Cluster != "" {
		return base + " (" + n.Cluster + ")"
	}
	return base
}

func formatRefs(nodes []*models.Node, max int) ([]string, int) {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		out = append(out, formatRef(n))
	}
	sort.Strings(out)
	if len(out) > max {
		extra := len(out) - max
		out = out[:max]
		out = append(out, fmt.Sprintf("...and %d more", extra))
	}
	return out, len(nodes)
}

// ============================================================
// Tool: get_dependencies
// ============================================================

type GetDependenciesInput struct {
	Name  string `json:"name"`
	Depth int    `json:"depth,omitempty"`
}

type GetDependenciesOutput struct {
	Node        string     `json:"node,omitempty"`
	Cluster     string     `json:"cluster,omitempty"`
	Callers     []string   `json:"callers,omitempty"`
	Callees     []string   `json:"callees,omitempty"`
	CallerCount int        `json:"caller_count"`
	CalleeCount int        `json:"callee_count"`
	Flows       []string   `json:"flows,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Message     string     `json:"message,omitempty"`
	// LegacyNode is kept on the wire for tests / older clients. New
	// integrations should read the string fields above.
	LegacyNode *NodeInfo  `json:"legacy_node,omitempty"`
	LegacyCallers []NodeInfo `json:"-"`
	LegacyCallees []NodeInfo `json:"-"`
}

func (h *Handlers) HandleGetDependencies(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetDependenciesInput,
) (*mcp.CallToolResult, GetDependenciesOutput, error) {
	if h.graph == nil {
		return nil, GetDependenciesOutput{Message: "Graph not loaded. Run 'universe init' first."}, nil
	}

	node := findNodeByName(h, input.Name)
	if node == nil {
		return nil, GetDependenciesOutput{
			Message: "No function or type found matching '" + input.Name + "'. Try search_graph to browse.",
		}, nil
	}

	callers := h.graph.GetDependents(node.ID)
	callees := h.graph.GetDependencies(node.ID)

	callerStrs, callerN := formatRefs(callers, MaxCallersInResponse)
	calleeStrs, calleeN := formatRefs(callees, MaxCalleesInResponse)

	out := GetDependenciesOutput{
		Node:        formatRef(node),
		Cluster:     node.Cluster,
		Callers:     callerStrs,
		Callees:     calleeStrs,
		CallerCount: callerN,
		CalleeCount: calleeN,
		Flows:       trimList(node.Flows, MaxFlowsInResponse),
		Summary:     dependencySummary(node, callerN, calleeN),
	}
	ni := toNodeInfo(node)
	out.LegacyNode = &ni

	if h.sessionMgr != nil {
		h.sessionMgr.OnToolCall("", "get_dependencies", node.ID, input.Name, "", true, "")
	}
	return nil, out, nil
}

func dependencySummary(n *models.Node, callerN, calleeN int) string {
	parts := []string{}
	if n.Cluster != "" {
		parts = append(parts, fmt.Sprintf("cluster %q", n.Cluster))
	}
	parts = append(parts, fmt.Sprintf("%d caller(s)", callerN))
	parts = append(parts, fmt.Sprintf("%d callee(s)", calleeN))
	if len(n.Flows) > 0 {
		parts = append(parts, fmt.Sprintf("in %d flow(s)", len(n.Flows)))
	}
	return n.Name + ": " + strings.Join(parts, ", ") + "."
}

// ============================================================
// Tool: get_impact_analysis
// ============================================================

type ImpactAnalysisInput struct {
	Name string `json:"name"`
}

type ImpactAnalysisOutput struct {
	Node             string   `json:"node,omitempty"`
	RiskLevel        string   `json:"risk_level,omitempty"`
	TotalAffected    int      `json:"total_affected"`
	WillBreak        []string `json:"will_break,omitempty"`
	LikelyAffected   []string `json:"likely_affected,omitempty"`
	PossiblyAffected []string `json:"possibly_affected,omitempty"`
	AffectedFlows    []string `json:"affected_flows,omitempty"`
	AffectedClusters []string `json:"affected_clusters,omitempty"`
	Summary          string   `json:"summary,omitempty"`
}

func (h *Handlers) HandleGetImpactAnalysis(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ImpactAnalysisInput,
) (*mcp.CallToolResult, ImpactAnalysisOutput, error) {
	if h.graph == nil {
		return nil, ImpactAnalysisOutput{Summary: "Graph not loaded. Run 'universe init' first."}, nil
	}
	node := findNodeByName(h, input.Name)
	if node == nil {
		return nil, ImpactAnalysisOutput{
			Summary: "No function or type found matching '" + input.Name + "'.",
		}, nil
	}

	// Prefer the precomputed impact summary; fall back to live BFS for nodes
	// init didn't precompute (low-connectivity helpers).
	sum := h.graph.GetImpact(node.ID)
	if sum == nil {
		sum = liveImpact(h, node)
	}

	return nil, ImpactAnalysisOutput{
		Node:             formatRef(node),
		RiskLevel:        sum.RiskLevel,
		TotalAffected:    sum.TotalAffected,
		WillBreak:        impactsAt(sum, 1),
		LikelyAffected:   impactsAt(sum, 2),
		PossiblyAffected: impactsAt(sum, 3),
		AffectedFlows:    trimList(sum.AffectedFlows, MaxFlowsInResponse),
		AffectedClusters: sum.AffectedClusters,
		Summary:          sum.Summary,
	}, nil
}

func impactsAt(sum *models.ImpactSummary, depth int) []string {
	if sum == nil || sum.ByDepth == nil {
		return nil
	}
	items := sum.ByDepth[depth]
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, im := range items {
		out = append(out, fmt.Sprintf("%s %s:%d", im.Name, im.File, im.Line))
	}
	return out
}

func liveImpact(h *Handlers, root *models.Node) *models.ImpactSummary {
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
		for _, c := range h.graph.GetDependents(current) {
			if _, seen := visited[c.ID]; seen {
				continue
			}
			visited[c.ID] = dist + 1
			queue = append(queue, c.ID)
			byDepth[dist+1] = append(byDepth[dist+1], models.Impact{
				NodeID: c.ID, Name: c.Name, File: c.FilePath, Line: c.StartLine,
				Confidence: 1.0 - float64(dist)*0.25, Relation: "calls",
			})
			total++
		}
	}
	risk := "low"
	switch {
	case total == 0:
		risk = "none"
	case total <= 2:
		risk = "low"
	case total <= 5:
		risk = "medium"
	default:
		risk = "high"
	}
	return &models.ImpactSummary{
		NodeID: root.ID, NodeName: root.Name, TotalAffected: total,
		RiskLevel: risk, ByDepth: byDepth,
		Summary: fmt.Sprintf("%s affects %d node(s). Risk: %s.", root.Name, total, risk),
	}
}

// ============================================================
// Tool: search_graph
// ============================================================

type SearchGraphInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type SearchGraphOutput struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
	// Legacy field kept so older tests/clients still get NodeInfo-shaped data.
	LegacyResults []NodeInfo `json:"-"`
}

type SearchResult struct {
	Ref         string `json:"ref"`
	Cluster     string `json:"cluster,omitempty"`
	CallerCount int    `json:"callers"`
	CalleeCount int    `json:"callees"`
	FlowCount   int    `json:"flow_count,omitempty"`
	Relevance   string `json:"relevance,omitempty"`
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
	if limit == 0 || limit > MaxSearchResults {
		limit = MaxSearchResults
	}

	nodes := h.graph.Search(input.Query)
	q := strings.ToLower(input.Query)

	type ranked struct {
		node      *models.Node
		relevance string
		order     int
	}
	var all []ranked
	for _, n := range nodes {
		if n == nil {
			continue
		}
		nl := strings.ToLower(n.Name)
		switch {
		case nl == q:
			all = append(all, ranked{n, "exact_name_match", 0})
		case strings.HasPrefix(nl, q):
			all = append(all, ranked{n, "prefix_match", 1})
		default:
			all = append(all, ranked{n, "contains", 2})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].order != all[j].order {
			return all[i].order < all[j].order
		}
		return all[i].node.Name < all[j].node.Name
	})

	out := SearchGraphOutput{Total: len(nodes)}
	for i := 0; i < len(all) && i < limit; i++ {
		r := all[i]
		out.Results = append(out.Results, SearchResult{
			Ref:         formatRef(r.node),
			Cluster:     r.node.Cluster,
			CallerCount: r.node.CallerCount,
			CalleeCount: r.node.CalleeCount,
			FlowCount:   len(r.node.Flows),
			Relevance:   r.relevance,
		})
		out.LegacyResults = append(out.LegacyResults, toNodeInfo(r.node))
	}
	return nil, out, nil
}

// ============================================================
// Tool: get_context — 360° view in one call
// ============================================================

type GetContextInput struct {
	Name string `json:"name"`
}

type GetContextOutput struct {
	Symbol           string   `json:"symbol,omitempty"`
	Cluster          string   `json:"cluster,omitempty"`
	IncomingCalls    []string `json:"incoming_calls,omitempty"`
	OutgoingCalls    []string `json:"outgoing_calls,omitempty"`
	Flows            []string `json:"flows,omitempty"`
	Impact           string   `json:"impact,omitempty"`
	ClusterNeighbors []string `json:"cluster_neighbors,omitempty"`
	Message          string   `json:"message,omitempty"`
}

func (h *Handlers) HandleGetContext(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetContextInput,
) (*mcp.CallToolResult, GetContextOutput, error) {
	if h.graph == nil {
		return nil, GetContextOutput{Message: "Graph not loaded. Run 'universe init' first."}, nil
	}
	node := findNodeByName(h, input.Name)
	if node == nil {
		return nil, GetContextOutput{
			Message: "No function or type found matching '" + input.Name + "'. Try search_graph.",
		}, nil
	}

	callers := h.graph.GetDependents(node.ID)
	callees := h.graph.GetDependencies(node.ID)
	callerStrs, _ := formatRefs(callers, MaxCallersInResponse)
	calleeStrs, _ := formatRefs(callees, MaxCalleesInResponse)

	out := GetContextOutput{
		Symbol:           formatRef(node),
		Cluster:          node.Cluster,
		IncomingCalls:    callerStrs,
		OutgoingCalls:    calleeStrs,
		Flows:            trimList(node.Flows, MaxFlowsInResponse),
		ClusterNeighbors: clusterNeighbors(h, node),
	}

	if sum := h.graph.GetImpact(node.ID); sum != nil {
		out.Impact = sum.Summary
	} else {
		out.Impact = liveImpact(h, node).Summary
	}

	if h.sessionMgr != nil {
		h.sessionMgr.OnToolCall("", "get_context", node.ID, input.Name, "", true, "")
	}
	return nil, out, nil
}

func clusterNeighbors(h *Handlers, n *models.Node) []string {
	if n.Cluster == "" {
		return nil
	}
	out := []string{}
	for id, other := range h.graph.Nodes {
		if id == n.ID || other == nil || other.Cluster != n.Cluster {
			continue
		}
		if other.Type != models.NodeFunction && other.Type != models.NodeMethod {
			continue
		}
		out = append(out, formatRef(other))
	}
	sort.Strings(out)
	if len(out) > MaxClusterNeighbors {
		out = out[:MaxClusterNeighbors]
	}
	return out
}

// ============================================================
// helpers
// ============================================================

func findNodeByName(h *Handlers, name string) *models.Node {
	if n := h.graph.GetNode(name); n != nil {
		return n
	}
	results := h.graph.Search(name)
	if len(results) == 0 {
		return nil
	}
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

func trimList(xs []string, max int) []string {
	if len(xs) <= max {
		return xs
	}
	out := make([]string, 0, max+1)
	out = append(out, xs[:max]...)
	out = append(out, fmt.Sprintf("...and %d more", len(xs)-max))
	return out
}
