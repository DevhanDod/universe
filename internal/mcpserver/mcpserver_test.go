package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// makeGraph builds a small in-memory graph for testing.
func makeGraph() *graph.Graph {
	g := &graph.Graph{
		Nodes: make(map[string]*models.Node),
		Edges: []*models.Edge{},
		Files: make(map[string]*models.FileInfo),
	}
	g.Nodes["pkg.Foo"] = &models.Node{
		ID:        "pkg.Foo",
		Name:      "Foo",
		Type:      models.NodeFunction,
		Package:   "pkg",
		FilePath:  "pkg/foo.go",
		StartLine: 10,
	}
	g.Nodes["pkg.Bar"] = &models.Node{
		ID:        "pkg.Bar",
		Name:      "Bar",
		Type:      models.NodeFunction,
		Package:   "pkg",
		FilePath:  "pkg/bar.go",
		StartLine: 5,
	}
	g.Edges = append(g.Edges, &models.Edge{
		From: "pkg.Bar",
		To:   "pkg.Foo",
		Type: models.EdgeCalls,
	})
	return g
}

func makeHandlers(g *graph.Graph) *Handlers {
	return &Handlers{graph: g}
}

// Test 1: get_dependencies finds a known node.
func TestHandleGetDependencies_Found(t *testing.T) {
	h := makeHandlers(makeGraph())
	_, out, err := h.HandleGetDependencies(context.Background(), &mcp.CallToolRequest{}, GetDependenciesInput{Name: "Foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Node == nil {
		t.Fatal("expected node, got nil")
	}
	if out.Node.Name != "Foo" {
		t.Errorf("expected Foo, got %s", out.Node.Name)
	}
}

// Test 2: get_dependencies returns message for unknown node.
func TestHandleGetDependencies_NotFound(t *testing.T) {
	h := makeHandlers(makeGraph())
	_, out, err := h.HandleGetDependencies(context.Background(), &mcp.CallToolRequest{}, GetDependenciesInput{Name: "NoSuchFunc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Node != nil {
		t.Error("expected nil node for missing function")
	}
	if out.Message == "" {
		t.Error("expected a message explaining the missing node")
	}
}

// Test 3: get_dependencies with nil graph returns message.
func TestHandleGetDependencies_NoGraph(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleGetDependencies(context.Background(), &mcp.CallToolRequest{}, GetDependenciesInput{Name: "Foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Message == "" {
		t.Error("expected degradation message when graph is nil")
	}
}

// Test 4: get_impact_analysis calculates low risk for small impact.
func TestHandleGetImpactAnalysis_LowRisk(t *testing.T) {
	h := makeHandlers(makeGraph())
	// Foo has no callers → 0 affected nodes → low risk
	_, out, err := h.HandleGetImpactAnalysis(context.Background(), &mcp.CallToolRequest{}, ImpactAnalysisInput{Name: "Foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.RiskLevel != "low" {
		t.Errorf("expected low risk, got %s", out.RiskLevel)
	}
}

// Test 5: search_graph finds nodes by name.
func TestHandleSearchGraph(t *testing.T) {
	h := makeHandlers(makeGraph())
	_, out, err := h.HandleSearchGraph(context.Background(), &mcp.CallToolRequest{}, SearchGraphInput{Query: "Foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Results) == 0 {
		t.Error("expected search results, got none")
	}
	if out.Results[0].Name != "Foo" {
		t.Errorf("expected Foo first, got %s", out.Results[0].Name)
	}
}

// Test 6: search_graph returns empty for no match.
func TestHandleSearchGraph_NoMatch(t *testing.T) {
	h := makeHandlers(makeGraph())
	_, out, err := h.HandleSearchGraph(context.Background(), &mcp.CallToolRequest{}, SearchGraphInput{Query: "ZZZNeverMatches"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(out.Results))
	}
}

// Test 7: recall_memory returns helpful message when engine unavailable.
func TestHandleRecallMemory_NoEngine(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleRecallMemory(context.Background(), &mcp.CallToolRequest{}, RecallMemoryInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Message == "" {
		t.Error("expected degradation message when retriever is nil")
	}
}

// Test 8: store_observation returns helpful message when engine unavailable.
func TestHandleStoreObservation_NoEngine(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleStoreObservation(context.Background(), &mcp.CallToolRequest{}, StoreObservationInput{
		GraphNodeID: "pkg.Foo",
		Category:    "fix",
		Content:     "test observation",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Message == "" {
		t.Error("expected degradation message when memory store is nil")
	}
}

// Test 9: find_skill returns helpful message when engine unavailable.
func TestHandleFindSkill_NoEngine(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleFindSkill(context.Background(), &mcp.CallToolRequest{}, FindSkillInput{TaskText: "fix the bug"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Found {
		t.Error("expected found=false when skill engine is nil")
	}
	if out.Message == "" {
		t.Error("expected degradation message when skill matcher is nil")
	}
}

// Test 10: list_skills returns helpful message when engine unavailable.
func TestHandleListSkills_NoEngine(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleListSkills(context.Background(), &mcp.CallToolRequest{}, ListSkillsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Message == "" {
		t.Error("expected degradation message when skill store is nil")
	}
}

// Test 11: get_cost_summary returns helpful message when orchestrator unavailable.
func TestHandleGetCostSummary_NoEngine(t *testing.T) {
	h := &Handlers{}
	_, out, err := h.HandleGetCostSummary(context.Background(), &mcp.CallToolRequest{}, GetCostSummaryInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Message == "" {
		t.Error("expected degradation message when orchestrator is nil")
	}
}

// Test 12: riskFor returns correct levels.
func TestRiskFor(t *testing.T) {
	cases := []struct{ n int; want string }{{0, "low"}, {2, "low"}, {3, "medium"}, {5, "medium"}, {6, "high"}, {100, "high"}}
	for _, c := range cases {
		if got := riskFor(c.n); got != c.want {
			t.Errorf("riskFor(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
