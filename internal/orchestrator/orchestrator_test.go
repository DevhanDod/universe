package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================
// PLAN STORE TESTS (require DATABASE_URL)
// ============================================================

func testDB(t *testing.T) string {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping database test")
	}
	return dbURL
}

func TestPlanStore_StoreAndGet(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	plan := Plan{
		DeveloperID: "test-dev",
		Title:       "Test plan",
		TaskPrompt:  "Fix the bug in handler",
		Steps:       []string{"step 1", "step 2", "step 3"},
		RiskLevel:   "low",
	}

	stored, err := ps.StorePlan(plan)
	if err != nil {
		t.Fatalf("StorePlan: %v", err)
	}
	if stored.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if stored.Status != PlanPending {
		t.Fatalf("expected pending, got %s", stored.Status)
	}

	got, err := ps.GetPlanByID(stored.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected plan, got nil")
	}
	if got.Title != plan.Title {
		t.Errorf("title: want %q, got %q", plan.Title, got.Title)
	}
	if len(got.Steps) != 3 {
		t.Errorf("steps: want 3, got %d", len(got.Steps))
	}
}

func TestPlanStore_GetLatest(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	devID := "test-dev-latest"
	plan := Plan{
		DeveloperID: devID,
		Title:       "Latest plan",
		TaskPrompt:  "Do something",
		Steps:       []string{"step 1"},
	}
	stored, err := ps.StorePlan(plan)
	if err != nil {
		t.Fatalf("StorePlan: %v", err)
	}

	got, err := ps.GetLatestPlan(devID)
	if err != nil {
		t.Fatalf("GetLatestPlan: %v", err)
	}
	if got == nil {
		t.Fatal("expected plan, got nil")
	}
	if got.ID != stored.ID {
		t.Errorf("expected plan %s, got %s", stored.ID, got.ID)
	}
}

func TestPlanStore_GetLatestUpdatesStatus(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	devID := "test-dev-status"
	stored, err := ps.StorePlan(Plan{
		DeveloperID: devID,
		Title:       "Status test",
		TaskPrompt:  "test",
		Steps:       []string{"step 1"},
	})
	if err != nil {
		t.Fatalf("StorePlan: %v", err)
	}

	got, err := ps.GetLatestPlan(devID)
	if err != nil {
		t.Fatalf("GetLatestPlan: %v", err)
	}
	if got == nil || got.ID != stored.ID {
		t.Fatal("plan not found")
	}
	if got.Status != PlanExecuting {
		t.Errorf("expected executing, got %s", got.Status)
	}
}

func TestPlanStore_StoreResult(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	stored, err := ps.StorePlan(Plan{
		DeveloperID: "test-dev-result",
		Title:       "Result test",
		TaskPrompt:  "test",
		Steps:       []string{"step 1"},
	})
	if err != nil {
		t.Fatalf("StorePlan: %v", err)
	}

	err = ps.StorePlanResult(stored.ID, true, "all done", []string{"main.go"}, true, "")
	if err != nil {
		t.Fatalf("StorePlanResult: %v", err)
	}

	got, err := ps.GetPlanResult(stored.ID)
	if err != nil {
		t.Fatalf("GetPlanResult: %v", err)
	}
	if got.Status != PlanCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.ResultSuccess == nil || !*got.ResultSuccess {
		t.Error("expected result_success = true")
	}
	if got.ResultSummary != "all done" {
		t.Errorf("summary: want %q, got %q", "all done", got.ResultSummary)
	}
}

func TestPlanStore_Verify(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	stored, err := ps.StorePlan(Plan{
		DeveloperID: "test-dev-verify",
		Title:       "Verify test",
		TaskPrompt:  "test",
		Steps:       []string{"step 1"},
	})
	if err != nil {
		t.Fatalf("StorePlan: %v", err)
	}

	if err := ps.VerifyPlan(stored.ID, true, "looks good"); err != nil {
		t.Fatalf("VerifyPlan: %v", err)
	}

	got, err := ps.GetPlanByID(stored.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if got.Status != PlanVerified {
		t.Errorf("expected verified, got %s", got.Status)
	}
}

func TestPlanStore_ListPlans(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	devID := "test-dev-list"
	for i := 0; i < 3; i++ {
		_, err := ps.StorePlan(Plan{
			DeveloperID: devID,
			Title:       "Plan",
			TaskPrompt:  "test",
			Steps:       []string{"step 1"},
		})
		if err != nil {
			t.Fatalf("StorePlan: %v", err)
		}
	}

	summaries, total, err := ps.ListPlans(devID, 10, 0)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(summaries) < 3 {
		t.Errorf("expected at least 3 summaries, got %d", len(summaries))
	}
	if total < 3 {
		t.Errorf("expected total >= 3, got %d", total)
	}
}

func TestPlanStore_StatusTransitions(t *testing.T) {
	dbURL := testDB(t)
	ps, err := NewPlanStore(dbURL)
	if err != nil {
		t.Fatalf("NewPlanStore: %v", err)
	}
	defer ps.Close() //nolint:errcheck

	devID := "test-dev-transitions"
	stored, _ := ps.StorePlan(Plan{
		DeveloperID: devID, Title: "T", TaskPrompt: "t", Steps: []string{"s"},
	})

	// pending → executing (via GetLatestPlan)
	got, _ := ps.GetLatestPlan(devID)
	if got.Status != PlanExecuting {
		t.Errorf("step 1: want executing, got %s", got.Status)
	}

	// executing → completed (via StorePlanResult)
	ps.StorePlanResult(stored.ID, true, "done", nil, true, "") //nolint:errcheck
	got, _ = ps.GetPlanByID(stored.ID)
	if got.Status != PlanCompleted {
		t.Errorf("step 2: want completed, got %s", got.Status)
	}

	// completed → verified (via VerifyPlan)
	ps.VerifyPlan(stored.ID, true, "ok") //nolint:errcheck
	got, _ = ps.GetPlanByID(stored.ID)
	if got.Status != PlanVerified {
		t.Errorf("step 3: want verified, got %s", got.Status)
	}
}

// ============================================================
// ROUTER TESTS
// ============================================================

type mockSkillChecker struct {
	found      bool
	confidence float64
}

func (m *mockSkillChecker) HasMatch(graphNodeIDs []string, taskText string) (bool, string, string, float64, error) {
	return m.found, "skill-1", "TypeFixer", m.confidence, nil
}

type mockMemoryChecker struct {
	found bool
	count int
}

func (m *mockMemoryChecker) HasRelevant(graphNodeIDs []string, developerID string) (bool, int, error) {
	return m.found, m.count, nil
}

type mockGraphChecker struct {
	nodeCount int
	crossRepo bool
}

func (m *mockGraphChecker) CountAffectedNodes(nodeIDs []string) (int, error) {
	return m.nodeCount, nil
}
func (m *mockGraphChecker) IsCrossRepo(nodeIDs []string) (bool, error) {
	return m.crossRepo, nil
}

func TestRouter_SkillAvailable(t *testing.T) {
	r := NewRouter(&mockSkillChecker{found: true, confidence: 0.92}, nil, nil)
	rec, err := r.Recommend([]string{"node1"}, "fix the type error", "dev1")
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if !rec.SkillAvailable {
		t.Error("expected skill_available = true")
	}
	if rec.SkillName != "TypeFixer" {
		t.Errorf("skill name: want TypeFixer, got %s", rec.SkillName)
	}
}

func TestRouter_MemoryAvailable(t *testing.T) {
	r := NewRouter(nil, &mockMemoryChecker{found: true, count: 3}, nil)
	rec, err := r.Recommend([]string{"node1"}, "fix the handler", "dev1")
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if !rec.MemoryAvailable {
		t.Error("expected memory_available = true")
	}
	if rec.MemoryCount != 3 {
		t.Errorf("memory count: want 3, got %d", rec.MemoryCount)
	}
}

func TestRouter_RiskLevel(t *testing.T) {
	tests := []struct {
		nodes     int
		crossRepo bool
		want      string
	}{
		{1, false, "low"},
		{2, false, "low"},
		{3, false, "medium"},
		{5, false, "medium"},
		{2, true, "medium"},
		{6, true, "high"},
	}

	for _, tc := range tests {
		r := NewRouter(nil, nil, &mockGraphChecker{nodeCount: tc.nodes, crossRepo: tc.crossRepo})
		rec, err := r.Recommend([]string{"node1"}, "task", "dev1")
		if err != nil {
			t.Fatalf("Recommend: %v", err)
		}
		if rec.RiskLevel != tc.want {
			t.Errorf("nodes=%d crossRepo=%v: want %s, got %s",
				tc.nodes, tc.crossRepo, tc.want, rec.RiskLevel)
		}
	}
}

func TestRouter_NoSkillNoMemory(t *testing.T) {
	r := NewRouter(nil, nil, nil)
	rec, err := r.Recommend(nil, "do something general", "dev1")
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if rec.SkillAvailable {
		t.Error("expected no skill")
	}
	if rec.MemoryAvailable {
		t.Error("expected no memory")
	}
	if rec.RiskLevel != "low" {
		t.Errorf("risk: want low, got %s", rec.RiskLevel)
	}
}

// ============================================================
// WORKSPACE TESTS
// ============================================================

func TestGenerateWorkspaces(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateWorkspaces(dir, "claude-opus-4", "claude-haiku-3.5"); err != nil {
		t.Fatalf("GenerateWorkspaces: %v", err)
	}

	plannerPath := filepath.Join(dir, ".universe", "workspaces", "planner.code-workspace")
	if _, err := os.Stat(plannerPath); err != nil {
		t.Errorf("planner workspace missing: %v", err)
	}

	executorPath := filepath.Join(dir, ".universe", "workspaces", "executor.code-workspace")
	if _, err := os.Stat(executorPath); err != nil {
		t.Errorf("executor workspace missing: %v", err)
	}
}

func TestWorkspaces_CorrectPaths(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateWorkspaces(dir, "gpt-4o", "gpt-4o-mini"); err != nil {
		t.Fatalf("GenerateWorkspaces: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".universe", "workspaces", "planner.code-workspace"))
	if err != nil {
		t.Fatalf("read planner workspace: %v", err)
	}
	if !strContains(string(data), "gpt-4o") {
		t.Error("planner workspace should contain gpt-4o")
	}

	data, err = os.ReadFile(filepath.Join(dir, ".universe", "workspaces", "executor.code-workspace"))
	if err != nil {
		t.Fatalf("read executor workspace: %v", err)
	}
	if !strContains(string(data), "gpt-4o-mini") {
		t.Error("executor workspace should contain gpt-4o-mini")
	}
}

// ============================================================
// SETUP TESTS
// ============================================================

func TestRunSetup(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	err := RunSetup(dir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name,
		cfg.PremiumModel, cfg.ExecutionModel, "")
	if err != nil {
		t.Fatalf("RunSetup: %v", err)
	}

	files := []string{
		".universe/workspaces/planner.code-workspace",
		".universe/workspaces/executor.code-workspace",
		".cursor/rules/universe-planner.mdc",
		".cursor/rules/universe-executor.mdc",
		".cursor/rules/universe-compression.mdc",
		".cursor/mcp.json",
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing file %s: %v", f, err)
		}
	}
}

func TestRunSetup_PreserveMCPConfig(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".cursor")
	os.MkdirAll(mcpDir, 0755)              //nolint:errcheck
	existing := []byte(`{"custom": true}`) //nolint:errcheck
	os.WriteFile(filepath.Join(mcpDir, "mcp.json"), existing, 0644) //nolint:errcheck

	cfg := DefaultConfig()
	RunSetup(dir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name, cfg.PremiumModel, cfg.ExecutionModel, "") //nolint:errcheck

	data, _ := os.ReadFile(filepath.Join(dir, ".cursor", "mcp.json"))
	if string(data) != string(existing) {
		t.Errorf("mcp.json should not be overwritten; got %s", string(data))
	}
}

func TestCursorRules_ModelNames(t *testing.T) {
	dir := t.TempDir()
	RunSetup(dir, "gpt-4o", "gpt-4o-mini", ModelConfig{}, ModelConfig{}, "") //nolint:errcheck

	plannerRule, _ := os.ReadFile(filepath.Join(dir, ".cursor", "rules", "universe-planner.mdc"))
	if !strContains(string(plannerRule), "gpt-4o") {
		t.Error("planner rule should reference gpt-4o")
	}

	executorRule, _ := os.ReadFile(filepath.Join(dir, ".cursor", "rules", "universe-executor.mdc"))
	if !strContains(string(executorRule), "gpt-4o-mini") {
		t.Error("executor rule should reference gpt-4o-mini")
	}
}

// ============================================================
// TRACKER TESTS (require DATABASE_URL)
// ============================================================

func TestTracker_LogCost(t *testing.T) {
	dbURL := testDB(t)
	tr, err := NewTracker(dbURL)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	defer tr.Close()

	cost := PlanCost{
		DeveloperID:             "test-tracker-dev",
		PlannerModel:            "claude-opus-4",
		ExecutorModel:           "claude-haiku-3.5",
		EstimatedPlannerTokens:  1000,
		EstimatedExecutorTokens: 2000,
		EstimatedPlannerCost:    0.015,
		EstimatedExecutorCost:   0.001,
		EstimatedTotalCost:      0.016,
		EstimatedAllPremiumCost: 0.045,
		EstimatedSavings:        0.029,
		SkillUsed:               true,
		MemoryHit:               false,
		RoutingRecommendation:   "skill_available",
	}
	if err := tr.LogPlanCost(cost); err != nil {
		t.Fatalf("LogPlanCost: %v", err)
	}
}

func TestTracker_MonthlySummary(t *testing.T) {
	dbURL := testDB(t)
	tr, err := NewTracker(dbURL)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	defer tr.Close()

	tr.LogPlanCost(PlanCost{ //nolint:errcheck
		DeveloperID:             "test-summary-dev",
		EstimatedTotalCost:      0.01,
		EstimatedAllPremiumCost: 0.05,
		EstimatedSavings:        0.04,
	})

	summaries, err := tr.GetMonthlySummary()
	if err != nil {
		t.Fatalf("GetMonthlySummary: %v", err)
	}
	if len(summaries) == 0 {
		t.Error("expected at least one monthly summary entry")
	}
}

// ============================================================
// CLASSIFY TASK TYPE TESTS
// ============================================================

func TestClassifyTaskType(t *testing.T) {
	tests := []struct {
		prompt string
		want   string
	}{
		{"fix the bug in handler", "code_fix"},
		{"write tests for the auth package", "test_gen"},
		{"refactor the database layer", "refactor"},
		{"migrate the users table", "migration"},
		{"explain how the router works", "explanation"},
		{"do something general", "general"},
	}
	for _, tc := range tests {
		got := ClassifyTaskType(tc.prompt)
		if got != tc.want {
			t.Errorf("ClassifyTaskType(%q) = %s, want %s", tc.prompt, got, tc.want)
		}
	}
}

// strContains is a simple substring check.
func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
