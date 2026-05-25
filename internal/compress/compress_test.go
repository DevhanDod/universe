package compress

import (
	"strings"
	"testing"
)

func TestBuildShorthand_BasicNode(t *testing.T) {
	nodes := []GraphNodeInfo{
		{
			Name:        "ValidateToken",
			Kind:        "function",
			Package:     "auth",
			File:        "validate.go",
			Line:        42,
			CallerNames: []string{"gateway.LoginHandler", "token.RefreshToken"},
			CalleeNames: []string{"crypto.VerifyJWT"},
		},
	}
	result := BuildShorthand(nodes)
	if !strings.Contains(result, "• auth.ValidateToken [function] (validate.go:42)") {
		t.Errorf("missing node header, got: %s", result)
	}
	if !strings.Contains(result, "← gateway.LoginHandler, token.RefreshToken") {
		t.Errorf("missing callers, got: %s", result)
	}
	if !strings.Contains(result, "→ crypto.VerifyJWT") {
		t.Errorf("missing callees, got: %s", result)
	}
}

func TestBuildShorthand_TruncatesCallers(t *testing.T) {
	node := GraphNodeInfo{
		Name:        "Serialize",
		Kind:        "function",
		Package:     "utils",
		File:        "json.go",
		Line:        10,
		CallerNames: []string{"a.One", "b.Two", "c.Three", "d.Four", "e.Five", "f.Six", "g.Seven"},
	}
	result := BuildShorthand([]GraphNodeInfo{node})
	if !strings.Contains(result, "← a.One, b.Two, c.Three, d.Four, e.Five and 2 more") {
		t.Errorf("unexpected truncation output, got: %s", result)
	}
}

func TestBuildShorthandCompact_MinimalFormat(t *testing.T) {
	nodes := []GraphNodeInfo{
		{
			Name:        "ValidateToken",
			Package:     "auth",
			File:        "validate.go",
			Line:        42,
			CallerNames: []string{"a", "b"},
			CalleeNames: []string{"c"},
		},
	}
	result := BuildShorthandCompact(nodes)
	expected := "auth.ValidateToken validate.go:42 ←2 →1\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestBuildPrompt_CompactLevel(t *testing.T) {
	prompt := BuildPrompt("Fix the bug", PromptConfig{
		Level: LevelCompact,
		GraphContext: []GraphNodeInfo{
			{Name: "Foo", Kind: "function", Package: "bar", File: "baz.go", Line: 1},
		},
	})
	if !strings.Contains(prompt, "Drop articles") {
		t.Error("missing compactPrompt content")
	}
	if !strings.Contains(prompt, "GRAPH CONTEXT") {
		t.Error("missing GRAPH CONTEXT section")
	}
	if !strings.Contains(prompt, "bar.Foo") {
		t.Error("missing node reference")
	}
	if !strings.Contains(prompt, "TASK:\nFix the bug") {
		t.Error("missing TASK section")
	}
	if strings.Contains(prompt, "OUTPUT SCHEMA") {
		t.Error("unexpected OUTPUT SCHEMA in compact prompt")
	}
}

func TestBuildPrompt_FullLevelWithTaskType(t *testing.T) {
	prompt := BuildPrompt("Generate tests", PromptConfig{
		Level:    LevelFull,
		TaskType: TaskTest,
	})
	if !strings.Contains(prompt, "Respond ONLY with valid JSON") {
		t.Error("missing fullPrompt content")
	}
	if !strings.Contains(prompt, TestOutputSchema.Schema) {
		t.Error("missing test output schema")
	}
	if !strings.Contains(prompt, TestOutputSchema.Example) {
		t.Error("missing test output example")
	}
	if !strings.Contains(prompt, "TASK:\nGenerate tests") {
		t.Error("missing TASK section")
	}
}

func TestParseFixOutput_ValidJSON(t *testing.T) {
	input := `{"fixes":[{"file":"a.go","line":1,"old_code":"old","new_code":"new","reason":"why"}],"affected_nodes":["repo:pkg:fn"],"confidence":0.9}`
	result, err := ParseFixOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Fixes) != 1 {
		t.Errorf("expected 1 fix, got %d", len(result.Fixes))
	}
	if result.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", result.Confidence)
	}
}

func TestParseFixOutput_WithCodeFences(t *testing.T) {
	input := "```json\n{\"fixes\":[],\"affected_nodes\":[],\"confidence\":0.5}\n```"
	result, err := ParseFixOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != 0.5 {
		t.Errorf("expected confidence 0.5, got %f", result.Confidence)
	}
}

func TestParseFixOutput_InvalidJSON(t *testing.T) {
	_, err := ParseFixOutput("this is not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFormatNameList_UnderLimit(t *testing.T) {
	result := formatNameList([]string{"a.Foo", "b.Bar"}, 5)
	if result != "a.Foo, b.Bar" {
		t.Errorf("expected 'a.Foo, b.Bar', got %q", result)
	}
}

func TestFormatNameList_OverLimit(t *testing.T) {
	result := formatNameList([]string{"a", "b", "c", "d", "e", "f"}, 3)
	if result != "a, b, c and 3 more" {
		t.Errorf("expected 'a, b, c and 3 more', got %q", result)
	}
}

func TestEstimateTokenSavings(t *testing.T) {
	if EstimateTokenSavings(LevelFull) != 0.85 {
		t.Error("expected 0.85 for LevelFull")
	}
	if EstimateTokenSavings(LevelCompact) != 0.75 {
		t.Error("expected 0.75 for LevelCompact")
	}
	if EstimateTokenSavings(LevelNormal) != 0.30 {
		t.Error("expected 0.30 for LevelNormal")
	}
}

func TestBuildPrompt_NoGraphContext(t *testing.T) {
	prompt := BuildPrompt("Do something", PromptConfig{
		Level: LevelCompact,
	})
	if !strings.Contains(prompt, "Drop articles") {
		t.Error("missing compactPrompt content")
	}
	if strings.Contains(prompt, "GRAPH CONTEXT") {
		t.Error("unexpected GRAPH CONTEXT in prompt with no nodes")
	}
	if !strings.Contains(prompt, "TASK:\nDo something") {
		t.Error("missing TASK section")
	}
}

func TestGetOutputSchema(t *testing.T) {
	cases := []struct {
		taskType TaskType
		wantNil  bool
		wantName string
	}{
		{TaskFix, false, "fix"},
		{TaskTest, false, "test"},
		{TaskPR, false, "pr"},
		{TaskAnalysis, false, "analysis"},
		{TaskGeneral, true, ""},
	}
	for _, c := range cases {
		schema := GetOutputSchema(c.taskType)
		if c.wantNil && schema != nil {
			t.Errorf("expected nil for %s, got %v", c.taskType, schema)
		}
		if !c.wantNil && schema == nil {
			t.Errorf("expected schema for %s, got nil", c.taskType)
		}
		if !c.wantNil && schema != nil && schema.Name != c.wantName {
			t.Errorf("expected schema name %q, got %q", c.wantName, schema.Name)
		}
	}
}
