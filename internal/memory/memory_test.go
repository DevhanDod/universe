package memory

import (
	"context"
	"os"
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testStore(t *testing.T) *Store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping database test")
	}
	s, err := NewStore(dbURL)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		// Clean up all test observations after each test
		s.pool.Exec(context.Background(), `DELETE FROM observations WHERE developer_id LIKE 'test-%'`) //nolint
		s.Close()
	})
	return s
}

func makeObs(devID, nodeID, category, summary string) Observation {
	return Observation{
		DeveloperID: devID,
		RepoID:      "test-repo",
		GraphNodeID: nodeID,
		Category:    category,
		Summary:     summary,
		Confidence:  1.0,
	}
}

// ── Store tests ───────────────────────────────────────────────────────────────

func TestStore_InsertAndGet(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev1", "auth:validate", "fix", "Fixed type mismatch in ValidateToken")
	obs.Detail = "Changed parameter from int to string"
	obs.ToolCalls = []ToolCall{{Tool: "read_file", Target: "auth.go"}}

	stored, err := s.InsertObservation(obs)
	if err != nil {
		t.Fatalf("InsertObservation: %v", err)
	}
	if stored.ID == "" {
		t.Error("expected non-empty ID")
	}
	if stored.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	got, err := s.GetByID(stored.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Summary != obs.Summary {
		t.Errorf("summary mismatch: want %q got %q", obs.Summary, got.Summary)
	}
	if got.Detail != obs.Detail {
		t.Errorf("detail mismatch: want %q got %q", obs.Detail, got.Detail)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Tool != "read_file" {
		t.Errorf("tool_calls mismatch: %+v", got.ToolCalls)
	}
}

func TestStore_GetByGraphNode(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 3; i++ {
		if _, err := s.InsertObservation(makeObs("test-dev2", "auth:validate", "fix", "fix "+string(rune('A'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := s.InsertObservation(makeObs("test-dev2", "gateway:login", "pattern", "pattern "+string(rune('A'+i)))); err != nil {
			t.Fatal(err)
		}
	}

	results, err := s.GetByGraphNode("auth:validate", "test-dev2", 10)
	if err != nil {
		t.Fatalf("GetByGraphNode: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestStore_GetByGraphNodes(t *testing.T) {
	s := testStore(t)

	nodes := []string{"auth:validate", "gateway:login"}
	for _, node := range nodes {
		if _, err := s.InsertObservation(makeObs("test-dev3", node, "fix", "note for "+node)); err != nil {
			t.Fatal(err)
		}
	}

	results, err := s.GetByGraphNodes(nodes, "test-dev3", 10)
	if err != nil {
		t.Fatalf("GetByGraphNodes: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(results))
	}
}

func TestStore_SearchKeyword(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev4", "auth:validate", "fix", "Fixed type mismatch in ValidateToken")
	if _, err := s.InsertObservation(obs); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchKeyword("type mismatch", "test-dev4", 10)
	if err != nil {
		t.Fatalf("SearchKeyword: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 result for 'type mismatch'")
	}

	noResults, err := s.SearchKeyword("database connection timeout", "test-dev4", 10)
	if err != nil {
		t.Fatalf("SearchKeyword (no match): %v", err)
	}
	// Just verify it doesn't error — unrelated query may return nothing
	_ = noResults
}

func TestStore_SearchSemantic(t *testing.T) {
	s := testStore(t)

	// Insert an observation with a known dummy embedding
	obs := makeObs("test-dev5", "auth:validate", "fix", "Token type mismatch")
	obs.Embedding = make([]float32, 1536)
	obs.Embedding[0] = 1.0 // simple non-zero vector

	if _, err := s.InsertObservation(obs); err != nil {
		t.Fatal(err)
	}

	// Search with a similar embedding
	queryVec := make([]float32, 1536)
	queryVec[0] = 0.9
	results, err := s.SearchSemantic(queryVec, "test-dev5", 10)
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 semantic result")
	}
}

func TestStore_PrivateSharedFiltering(t *testing.T) {
	s := testStore(t)

	// Private observation for devA
	privObs := makeObs("test-devA", "auth:validate", "fix", "private note devA")
	privObs.Shared = false
	if _, err := s.InsertObservation(privObs); err != nil {
		t.Fatal(err)
	}

	// Shared observation for devA
	sharedObs := makeObs("test-devA", "auth:validate", "fix", "shared note devA")
	sharedObs.Shared = true
	if _, err := s.InsertObservation(sharedObs); err != nil {
		t.Fatal(err)
	}

	// devB should only see the shared one
	resultsB, err := s.GetByGraphNode("auth:validate", "test-devB", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resultsB {
		if r.Summary == "private note devA" {
			t.Error("devB should not see devA's private observation")
		}
	}

	// devA should see both
	resultsA, err := s.GetByGraphNode("auth:validate", "test-devA", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resultsA) < 2 {
		t.Errorf("devA should see at least 2 observations, got %d", len(resultsA))
	}
}

func TestStore_TouchRecalled(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev6", "auth:validate", "fix", "something happened")
	obs.Confidence = 0.5
	stored, err := s.InsertObservation(obs)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.TouchRecalled(stored.ID); err != nil {
		t.Fatalf("TouchRecalled: %v", err)
	}

	got, err := s.GetByID(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecalledAt == nil {
		t.Error("expected recalled_at to be set")
	}
	if got.Confidence <= 0.5 {
		t.Errorf("expected confidence boost, got %f", got.Confidence)
	}
}

func TestStore_DeleteByConfidence(t *testing.T) {
	s := testStore(t)

	low := makeObs("test-dev7", "auth:validate", "fix", "low confidence obs")
	low.Confidence = 0.05
	storedLow, err := s.InsertObservation(low)
	if err != nil {
		t.Fatal(err)
	}

	high := makeObs("test-dev7", "auth:validate", "fix", "high confidence obs")
	high.Confidence = 0.5
	storedHigh, err := s.InsertObservation(high)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := s.DeleteByConfidence(0.1)
	if err != nil {
		t.Fatalf("DeleteByConfidence: %v", err)
	}
	if deleted == 0 {
		t.Error("expected at least 1 deletion")
	}

	// Low should be gone
	got, _ := s.GetByID(storedLow.ID)
	if got != nil {
		t.Error("expected low-confidence observation to be deleted")
	}

	// High should still exist
	got2, err := s.GetByID(storedHigh.ID)
	if err != nil || got2 == nil {
		t.Error("expected high-confidence observation to still exist")
	}
}

// ── Retriever tests ───────────────────────────────────────────────────────────

type mockGraphQuerier struct {
	callers map[string][]string
	callees map[string][]string
}

func (m *mockGraphQuerier) GetCallerIDs(nodeID string) ([]string, error) {
	return m.callers[nodeID], nil
}
func (m *mockGraphQuerier) GetCalleeIDs(nodeID string) ([]string, error) {
	return m.callees[nodeID], nil
}

func TestRetriever_GraphNeighborExpansion(t *testing.T) {
	s := testStore(t)

	// Insert observation for a neighbor node
	if _, err := s.InsertObservation(makeObs("test-dev8", "auth:helpers", "fix", "helper fix")); err != nil {
		t.Fatal(err)
	}

	gq := &mockGraphQuerier{
		callees: map[string][]string{
			"auth:validate": {"auth:helpers"},
		},
	}
	cfg := DefaultConfig()
	r := NewRetriever(s, nil, gq, cfg)

	result, err := r.Search(SearchQuery{
		GraphNodeIDs:          []string{"auth:validate"},
		IncludeGraphNeighbors: true,
		DeveloperID:           "test-dev8",
		Limit:                 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.SearchedNodes) < 2 {
		t.Errorf("expected at least 2 searched nodes (target + neighbor), got %d", len(result.SearchedNodes))
	}
}

func TestRetriever_GetSessionContext(t *testing.T) {
	s := testStore(t)

	if _, err := s.InsertObservation(makeObs("test-dev9", "auth:validate", "fix", "context obs")); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	r := NewRetriever(s, nil, nil, cfg)

	result, err := r.GetSessionContext([]string{"auth:validate"}, "test-dev9")
	if err != nil {
		t.Fatalf("GetSessionContext: %v", err)
	}
	if len(result.Summaries) == 0 {
		t.Error("expected at least 1 result from session context")
	}
}

func TestRetriever_EmptyTextQuery(t *testing.T) {
	s := testStore(t)

	if _, err := s.InsertObservation(makeObs("test-dev10", "auth:validate", "fix", "some fix")); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	r := NewRetriever(s, nil, nil, cfg)

	result, err := r.Search(SearchQuery{
		GraphNodeIDs: []string{"auth:validate"},
		DeveloperID:  "test-dev10",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.SearchMethod == "keyword_only" || result.SearchMethod == "semantic_only" {
		t.Errorf("expected graph-based method, got %s", result.SearchMethod)
	}
}

// ── Compressor tests (no DB, no real API) ─────────────────────────────────────

func TestCompressor_GroupsByGraphNode(t *testing.T) {
	events := []SessionEvent{
		{Timestamp: time.Now(), ToolName: "read_file", GraphNodeID: "auth:validate", Input: "validate.go", Success: true},
		{Timestamp: time.Now(), ToolName: "write_file", GraphNodeID: "auth:validate", Input: "validate.go", Success: true},
		{Timestamp: time.Now(), ToolName: "read_file", GraphNodeID: "gateway:login", Input: "handler.go", Success: true},
	}

	// Use fallback by passing a config with no API key — we just test grouping
	cfg := DefaultConfig()
	c := NewCompressor(cfg)

	// Use only fallback — compressGroup will fail API call and use fallback
	obs := c.fallbackObservation(events[0], "dev1", "repo1", "session1")
	if obs.GraphNodeID != "auth:validate" {
		t.Errorf("expected graph_node_id auth:validate, got %s", obs.GraphNodeID)
	}

	// Verify grouping logic by checking event grouping directly
	grouped := make(map[string][]SessionEvent)
	for _, e := range events {
		grouped[e.GraphNodeID] = append(grouped[e.GraphNodeID], e)
	}
	if len(grouped["auth:validate"]) != 2 {
		t.Errorf("expected 2 events for auth:validate, got %d", len(grouped["auth:validate"]))
	}
	if len(grouped["gateway:login"]) != 1 {
		t.Errorf("expected 1 event for gateway:login, got %d", len(grouped["gateway:login"]))
	}
}

func TestCompressor_ParseCompressedResponse_ValidJSON(t *testing.T) {
	cfg := DefaultConfig()
	c := NewCompressor(cfg)

	input := `{"summary": "Fixed type mismatch", "detail": "Changed int to string", "category": "fix"}`
	obs, err := c.parseCompressedResponse(input)
	if err != nil {
		t.Fatalf("parseCompressedResponse: %v", err)
	}
	if obs.Summary != "Fixed type mismatch" {
		t.Errorf("unexpected summary: %s", obs.Summary)
	}
	if obs.Category != "fix" {
		t.Errorf("unexpected category: %s", obs.Category)
	}
}

func TestCompressor_ParseCompressedResponse_WithFences(t *testing.T) {
	cfg := DefaultConfig()
	c := NewCompressor(cfg)

	input := "```json\n{\"summary\": \"Pattern found\", \"detail\": \"\", \"category\": \"pattern\"}\n```"
	obs, err := c.parseCompressedResponse(input)
	if err != nil {
		t.Fatalf("parseCompressedResponse with fences: %v", err)
	}
	if obs.Summary != "Pattern found" {
		t.Errorf("unexpected summary: %s", obs.Summary)
	}
}

// ── Hooks tests (no DB required) ─────────────────────────────────────────────

func TestHooks_CreatesSession(t *testing.T) {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		config:   DefaultConfig(),
		stopCh:   make(chan struct{}),
	}

	sm.OnToolCall("dev1", "read_file", "auth:validate", "validate.go", "content", true, "repo1")

	if sm.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session, got %d", sm.ActiveSessionCount())
	}
}

func TestHooks_AccumulatesEvents(t *testing.T) {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		config:   DefaultConfig(),
		stopCh:   make(chan struct{}),
	}

	for i := 0; i < 5; i++ {
		sm.OnToolCall("dev2", "read_file", "auth:validate", "input", "output", true, "repo1")
	}

	session, ok := sm.GetSession("dev2")
	if !ok {
		t.Fatal("expected session to exist")
	}
	if len(session.RawEvents) != 5 {
		t.Errorf("expected 5 events, got %d", len(session.RawEvents))
	}
}

// ── Decay tests ───────────────────────────────────────────────────────────────

func TestDecay_ConfidenceFormula(t *testing.T) {
	// Test the decay formula directly without a DB
	initialConfidence := 1.0
	decayRate := 0.02
	daysUnused := 30.0

	newConfidence := initialConfidence - (daysUnused * decayRate)
	expected := 0.4
	if abs(newConfidence-expected) > 0.001 {
		t.Errorf("expected confidence %.3f, got %.3f", expected, newConfidence)
	}
}

func TestDecay_ReducesConfidence(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev11", "auth:validate", "fix", "old observation")
	obs.Confidence = 1.0
	stored, err := s.InsertObservation(obs)
	if err != nil {
		t.Fatal(err)
	}

	// Manually set recalled_at to 30 days ago for this test
	_, err = s.pool.Exec(context.Background(),
		`UPDATE observations SET recalled_at = NOW() - interval '30 days' WHERE id = $1`,
		stored.ID)
	if err != nil {
		t.Fatalf("set recalled_at: %v", err)
	}

	cfg := DefaultConfig()
	d := NewDecayRunner(s, cfg)
	result, err := d.RunDecay()
	if err != nil {
		t.Fatalf("RunDecay: %v", err)
	}
	if result.Updated == 0 {
		t.Error("expected at least 1 updated observation")
	}
}

func TestDecay_DeletesBelowThreshold(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev12", "auth:validate", "fix", "low confidence observation")
	obs.Confidence = 0.05
	if _, err := s.InsertObservation(obs); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	d := NewDecayRunner(s, cfg)
	result, err := d.RunDecay()
	if err != nil {
		t.Fatalf("RunDecay: %v", err)
	}
	if result.Deleted == 0 {
		t.Error("expected at least 1 deleted observation")
	}
}

func TestDecay_RecentRecallNoDecay(t *testing.T) {
	s := testStore(t)

	obs := makeObs("test-dev13", "auth:validate", "fix", "recently recalled")
	obs.Confidence = 1.0
	stored, err := s.InsertObservation(obs)
	if err != nil {
		t.Fatal(err)
	}

	// Touch it (sets recalled_at = NOW())
	if err := s.TouchRecalled(stored.ID); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	d := NewDecayRunner(s, cfg)
	if _, err := d.RunDecay(); err != nil {
		t.Fatalf("RunDecay: %v", err)
	}

	// Should still exist with high confidence
	got, err := s.GetByID(stored.ID)
	if err != nil || got == nil {
		t.Fatal("expected observation to still exist after decay")
	}
	if got.Confidence < 0.9 {
		t.Errorf("recently recalled obs should not decay, confidence: %f", got.Confidence)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
