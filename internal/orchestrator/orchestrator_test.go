package orchestrator

import (
	"fmt"
	"testing"
)

// ============================================================
// MOCK IMPLEMENTATIONS
// ============================================================

type mockSkillMatcher struct {
	match       *SkillMatch
	err         error
}

func (m *mockSkillMatcher) Match(_ []string, _ string) (*SkillMatch, error) {
	return m.match, m.err
}

type mockMemoryRecaller struct {
	check *MemoryCheck
	err   error
}

func (m *mockMemoryRecaller) QuickCheck(_ []string, _ string) (*MemoryCheck, error) {
	return m.check, m.err
}

type mockGraphAnalyzer struct {
	nodeCount int
	crossRepo bool
	language  string
}

func (m *mockGraphAnalyzer) CountAffectedNodes(_ []string) (int, error) {
	return m.nodeCount, nil
}

func (m *mockGraphAnalyzer) IsCrossRepo(_ []string) (bool, error) {
	return m.crossRepo, nil
}

func (m *mockGraphAnalyzer) GetLanguage(_ []string) (string, error) {
	return m.language, nil
}

type mockLLMClient struct {
	responses map[ModelTier]string
	callCount int
}

func newMockLLMClient(opusResp, haikuResp string) *mockLLMClient {
	return &mockLLMClient{
		responses: map[ModelTier]string{
			Opus:  opusResp,
			Haiku: haikuResp,
		},
	}
}

func (m *mockLLMClient) callMock(model ModelTier, _, _ string, _ int) (*LLMResponse, error) {
	m.callCount++
	resp, ok := m.responses[model]
	if !ok {
		return nil, fmt.Errorf("no mock response for model %s", model)
	}
	return &LLMResponse{
		Content:      resp,
		InputTokens:  100,
		OutputTokens: 50,
		Model:        model,
		LatencyMs:    10,
		CostUSD:      calculateCost(model, 100, 50),
	}, nil
}

func defaultConfig() Config {
	cfg := DefaultConfig()
	cfg.DatabaseURL = ""
	return cfg
}

// ============================================================
// ROUTER TESTS
// ============================================================

func TestRouter_SkillMatch(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: &SkillMatch{
			SkillID: "skill-1", Name: "fix-auth", SuccessRate: 0.90, GraphOverlap: 0.8,
		}},
		nil, nil, cfg,
	)

	task := Task{ID: "t1", Prompt: "fix bug", GraphNodeIDs: []string{"node1"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModeSkillExecute {
		t.Errorf("expected ModeSkillExecute, got %s", dec.Mode)
	}
	if !dec.SkipPlanning {
		t.Error("expected SkipPlanning=true")
	}
	if dec.VerifyTier != VerifyAutomated {
		t.Errorf("expected VerifyAutomated, got %d", dec.VerifyTier)
	}
}

func TestRouter_MemoryMatch(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: nil},
		&mockMemoryRecaller{check: &MemoryCheck{HasExactMatch: true, Confidence: 0.95}},
		nil, cfg,
	)

	task := Task{ID: "t2", Prompt: "apply fix", GraphNodeIDs: []string{"node1"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModeMemoryApply {
		t.Errorf("expected ModeMemoryApply, got %s", dec.Mode)
	}
	if !dec.MemoryHit {
		t.Error("expected MemoryHit=true")
	}
	if dec.VerifyTier != VerifySpotCheck {
		t.Errorf("expected VerifySpotCheck, got %d", dec.VerifyTier)
	}
}

func TestRouter_SimplePlanExecute(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: nil},
		&mockMemoryRecaller{check: nil},
		&mockGraphAnalyzer{nodeCount: 2, crossRepo: false},
		cfg,
	)

	task := Task{ID: "t3", Prompt: "fix the null pointer", TaskType: TaskCodeFix, GraphNodeIDs: []string{"n1", "n2"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModePlanExecute {
		t.Errorf("expected ModePlanExecute, got %s", dec.Mode)
	}
}

func TestRouter_ComplexFullOrch(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: nil},
		&mockMemoryRecaller{check: nil},
		&mockGraphAnalyzer{nodeCount: 5, crossRepo: true},
		cfg,
	)

	task := Task{ID: "t4", Prompt: "fix cross-repo auth", TaskType: TaskCodeFix,
		GraphNodeIDs: []string{"n1", "n2", "n3", "n4", "n5"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModeFullOrchestration {
		t.Errorf("expected ModeFullOrchestration, got %s", dec.Mode)
	}
	if dec.VerifyTier != VerifyFullReview {
		t.Errorf("expected VerifyFullReview, got %d", dec.VerifyTier)
	}
}

func TestRouter_NoTemplateOpus(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: nil},
		&mockMemoryRecaller{check: nil},
		&mockGraphAnalyzer{nodeCount: 10, crossRepo: false},
		cfg,
	)

	// many graph nodes → !simple → no template → ModeSingleOpus
	task := Task{
		ID:           "t5",
		Prompt:       "explain why this pattern exists",
		TaskType:     TaskExplanation,
		GraphNodeIDs: []string{"n1", "n2", "n3", "n4", "n5"},
	}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModeSingleOpus {
		t.Errorf("expected ModeSingleOpus, got %s", dec.Mode)
	}
}

func TestRouter_SimpleHaiku(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: nil},
		&mockMemoryRecaller{check: nil},
		&mockGraphAnalyzer{nodeCount: 1, crossRepo: false},
		cfg,
	)

	task := Task{ID: "t6", Prompt: "do something general", TaskType: TaskGeneral, GraphNodeIDs: []string{"n1"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode != ModeSingleHaiku {
		t.Errorf("expected ModeSingleHaiku, got %s", dec.Mode)
	}
}

func TestRouter_SkillBelowThreshold(t *testing.T) {
	cfg := defaultConfig()
	router := NewRouter(
		&mockSkillMatcher{match: &SkillMatch{
			SkillID: "skill-low", SuccessRate: 0.60, GraphOverlap: 0.8,
		}},
		&mockMemoryRecaller{check: nil},
		&mockGraphAnalyzer{nodeCount: 2, crossRepo: false},
		cfg,
	)

	task := Task{ID: "t7", Prompt: "fix bug", TaskType: TaskCodeFix, GraphNodeIDs: []string{"n1"}}
	dec, err := router.Route(task)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Mode == ModeSkillExecute {
		t.Error("should NOT route to ModeSkillExecute when below threshold")
	}
}

func TestClassifyTaskType(t *testing.T) {
	cases := []struct {
		prompt   string
		expected TaskType
	}{
		{"fix the type mismatch in auth.go", TaskCodeFix},
		{"write tests for ValidateToken function", TaskTestGen},
		{"create a PR description for this change", TaskPRGen},
		{"explain why this pattern breaks on nil", TaskExplanation},
		{"update the database config to use TLS", TaskConfigChange},
		{"refactor the login handler", TaskRefactor},
		{"upgrade the pgx dependency to v6", TaskDepUpdate},
		{"analyze the impact of removing this function", TaskAnalysis},
		{"migrate the users table schema", TaskMigration},
	}

	for _, tc := range cases {
		got := ClassifyTaskType(tc.prompt)
		if got != tc.expected {
			t.Errorf("ClassifyTaskType(%q) = %s, want %s", tc.prompt, got, tc.expected)
		}
	}
}

// ============================================================
// PLANNER TESTS
// ============================================================

func TestValidatePlan_CircularDeps(t *testing.T) {
	plan := &Plan{
		TaskID: "task-1",
		SubTasks: []SubTask{
			{ID: "a", Action: "modify_file", DependsOn: []string{"b"}, VerifyTier: 1},
			{ID: "b", Action: "modify_file", DependsOn: []string{"a"}, VerifyTier: 1},
		},
	}
	if err := ValidatePlan(plan); err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestValidatePlan_Valid(t *testing.T) {
	plan := &Plan{
		TaskID: "task-2",
		SubTasks: []SubTask{
			{ID: "a", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "b", Action: "generate_test", DependsOn: []string{"a"}, VerifyTier: 1},
		},
	}
	if err := ValidatePlan(plan); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePlan_TooManySubTasks(t *testing.T) {
	plan := &Plan{TaskID: "t"}
	for i := 0; i < 6; i++ {
		plan.SubTasks = append(plan.SubTasks, SubTask{
			ID: fmt.Sprintf("s%d", i), Action: "modify_file", VerifyTier: 1,
		})
	}
	if err := ValidatePlan(plan); err == nil {
		t.Error("expected error for too many sub-tasks")
	}
}

// ============================================================
// VERIFIER TESTS
// ============================================================

func TestVerifier_AutomatedCatchesSyntax(t *testing.T) {
	v := &Verifier{}
	result := &SubTaskResult{
		Success: true,
		Output:  "package main\n\nfunc Broken( {\n}",
	}
	subTask := SubTask{Action: "modify_file", VerifyTier: VerifyAutomated}
	vr, err := v.verifyAutomated(subTask, result)
	if err != nil {
		t.Fatal(err)
	}
	if vr.Passed {
		t.Error("expected syntax error to fail verification")
	}
	if vr.TokensUsed != 0 {
		t.Errorf("expected 0 tokens for automated check, got %d", vr.TokensUsed)
	}
}

func TestVerifier_AutomatedPassesValidGo(t *testing.T) {
	v := &Verifier{}
	result := &SubTaskResult{
		Success: true,
		Output:  "package main\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n",
	}
	subTask := SubTask{Action: "modify_file", VerifyTier: VerifyAutomated}
	vr, err := v.verifyAutomated(subTask, result)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Passed {
		t.Errorf("expected valid Go to pass, got reason: %s", vr.Reason)
	}
}

func TestVerifier_AutomatedFailsOnEmpty(t *testing.T) {
	v := &Verifier{}
	result := &SubTaskResult{Success: false, ErrorMessage: "haiku failed"}
	subTask := SubTask{Action: "modify_file", VerifyTier: VerifyAutomated}
	vr, _ := v.verifyAutomated(subTask, result)
	if vr.Passed {
		t.Error("expected failure for failed executor result")
	}
}

// ============================================================
// ESCALATION TESTS
// ============================================================

type callTracker struct {
	calls []ModelTier
}

func TestEscalation_AllStepsRunWithNilClient(t *testing.T) {
	// with nil LLM client: executor returns "no LLM client" error for all steps,
	// rephrase and takeover also return errors → HandleFailure returns error after 3 steps
	executor := NewExecutor(nil, nil, defaultConfig())
	esc := NewEscalation(executor, nil, nil, defaultConfig())

	failed := &SubTaskResult{SubTaskID: "s1", Success: false, ErrorMessage: "initial error"}
	subTask := SubTask{ID: "s1", Action: "modify_file", VerifyTier: 1}

	_, record, err := esc.HandleFailure(subTask, failed, "fix bug", nil)
	// expected: error after all steps exhausted
	if err == nil {
		t.Error("expected error when all escalation steps fail")
	}
	// record should have at least step 1 logged
	if record == nil {
		t.Fatal("expected escalation record")
	}
	if len(record.Steps) == 0 {
		t.Error("expected at least one escalation step recorded")
	}
}

func TestEscalation_RecordSubTaskID(t *testing.T) {
	executor := NewExecutor(nil, nil, defaultConfig())
	esc := NewEscalation(executor, nil, nil, defaultConfig())

	failed := &SubTaskResult{SubTaskID: "my-subtask", Success: false, ErrorMessage: "err"}
	subTask := SubTask{ID: "my-subtask", Action: "modify_file", VerifyTier: 1}

	_, record, _ := esc.HandleFailure(subTask, failed, "context", nil)
	if record.SubTaskID != "my-subtask" {
		t.Errorf("expected SubTaskID=my-subtask, got %s", record.SubTaskID)
	}
}

// ============================================================
// PARALLEL EXECUTION TESTS
// ============================================================

func TestParallel_BuildDependencyGraph(t *testing.T) {
	subTasks := []SubTask{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: nil},
		{ID: "c", DependsOn: []string{"a"}},
	}
	ready, dependsOn, dependedBy := buildDependencyGraph(subTasks)

	if len(ready) != 2 {
		t.Errorf("expected 2 ready tasks, got %d: %v", len(ready), ready)
	}
	if len(dependsOn["c"]) != 1 || dependsOn["c"][0] != "a" {
		t.Errorf("c should depend on a, got %v", dependsOn["c"])
	}
	if len(dependedBy["a"]) != 1 || dependedBy["a"][0] != "c" {
		t.Errorf("a should be depended on by c, got %v", dependedBy["a"])
	}
}

func TestParallel_IndependentTasks(t *testing.T) {
	plan := &Plan{
		TaskID: "task-parallel",
		SubTasks: []SubTask{
			{ID: "a", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "b", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "c", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
		},
	}

	// executor with nil client — all sub-tasks fail (we just check they all run)
	executor := NewExecutor(nil, nil, defaultConfig())
	esc := NewEscalation(executor, nil, nil, defaultConfig())
	pe := NewParallelExecutor(executor, esc, defaultConfig())

	results, _, _ := pe.ExecuteAll(plan, "test prompt")
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestParallel_DependentTasks(t *testing.T) {
	plan := &Plan{
		TaskID: "task-deps",
		SubTasks: []SubTask{
			{ID: "a", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "b", Action: "modify_file", DependsOn: []string{"a"}, VerifyTier: 1},
		},
	}

	ready, _, _ := buildDependencyGraph(plan.SubTasks)
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("only 'a' should be initially ready, got %v", ready)
	}
}

func TestParallel_MixedDeps(t *testing.T) {
	plan := &Plan{
		TaskID: "task-mixed",
		SubTasks: []SubTask{
			{ID: "a", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "b", Action: "modify_file", DependsOn: nil, VerifyTier: 1},
			{ID: "c", Action: "modify_file", DependsOn: []string{"a"}, VerifyTier: 1},
		},
	}

	ready, dependsOn, dependedBy := buildDependencyGraph(plan.SubTasks)

	// a and b should be ready, c should not
	readySet := make(map[string]bool)
	for _, id := range ready {
		readySet[id] = true
	}
	if !readySet["a"] || !readySet["b"] {
		t.Errorf("a and b should be ready, got %v", ready)
	}
	if readySet["c"] {
		t.Error("c should not be ready (depends on a)")
	}

	// c depends on a
	if len(dependsOn["c"]) != 1 {
		t.Error("c should depend on a")
	}
	// a is depended on by c
	if len(dependedBy["a"]) != 1 || dependedBy["a"][0] != "c" {
		t.Errorf("a should be depended on by c, got %v", dependedBy["a"])
	}
}

// ============================================================
// COST CALCULATION TESTS
// ============================================================

func TestCalculateCost_Opus(t *testing.T) {
	// 1000 input tokens at $15/M = $0.000015
	// 500 output tokens at $75/M = $0.0000375
	cost := calculateCost(Opus, 1000, 500)
	expected := (1000*15.0 + 500*75.0) / 1_000_000
	if cost != expected {
		t.Errorf("expected %.8f, got %.8f", expected, cost)
	}
}

func TestCalculateCost_Haiku(t *testing.T) {
	cost := calculateCost(Haiku, 1000, 500)
	expected := (1000*0.25 + 500*1.25) / 1_000_000
	if cost != expected {
		t.Errorf("expected %.8f, got %.8f", expected, cost)
	}
}

func TestCalculateCost_SavingsRatio(t *testing.T) {
	tokens := 1_000_000
	opusCost := calculateCost(Opus, tokens, tokens)
	haikuCost := calculateCost(Haiku, tokens, tokens)
	ratio := opusCost / haikuCost
	// Roughly 30-60x cheaper
	if ratio < 20 || ratio > 80 {
		t.Errorf("expected ~30-60x cheaper, got %.1fx", ratio)
	}
}

// ============================================================
// TEMPLATE TESTS
// ============================================================

func TestHasTemplate(t *testing.T) {
	cases := []struct {
		taskType TaskType
		expected bool
	}{
		{TaskCodeFix, true},
		{TaskTestGen, true},
		{TaskPRGen, true},
		{TaskRefactor, true},
		{TaskDepUpdate, true},
		{TaskConfigChange, true},
		{TaskAnalysis, true},
		{TaskExplanation, false},
		{TaskGeneral, false},
		{TaskMigration, false},
	}
	for _, tc := range cases {
		got := HasTemplate(tc.taskType)
		if got != tc.expected {
			t.Errorf("HasTemplate(%s) = %v, want %v", tc.taskType, got, tc.expected)
		}
	}
}

func TestGetPlannerPrompt_ContainsSchema(t *testing.T) {
	prompt := GetPlannerPrompt(TaskCodeFix, "node: validate.go [func]", "past fix: nil check", "")
	if len(prompt) == 0 {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "sub_tasks") {
		t.Error("expected prompt to contain sub_tasks schema")
	}
	if !contains(prompt, "GRAPH CONTEXT") {
		t.Error("expected prompt to contain GRAPH CONTEXT section")
	}
	if !contains(prompt, "MEMORY CONTEXT") {
		t.Error("expected prompt to contain MEMORY CONTEXT section")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ============================================================
// TRACKER TESTS (no DB required)
// ============================================================

func TestTracker_NoDB_DoesNotPanic(t *testing.T) {
	tracker, err := NewTracker("")
	if err != nil {
		t.Fatal(err)
	}
	// should not panic even with nil pool
	tracker.LogCall(CostRecord{
		TaskID:      "t1",
		DeveloperID: "dev1",
		Model:       Haiku,
		InputTokens: 100,
		OutputTokens: 50,
		CostUSD:     0.0001,
		Phase:       "execute",
		RoutingMode: ModeSkillExecute,
	})
	tracker.Stop()
}

func TestTracker_GetMonthlySummary_NoDB(t *testing.T) {
	tracker, _ := NewTracker("")
	summaries, err := tracker.GetMonthlySummary()
	if err != nil {
		t.Fatal(err)
	}
	if summaries != nil {
		t.Error("expected nil summaries with no DB")
	}
}
