package compress

import (
	"encoding/json"
	"fmt"
	"strings"
)

type OutputSchema struct {
	Name        string
	Description string
	Schema      string
	Example     string
}

var FixOutputSchema = OutputSchema{
	Name:        "fix",
	Description: "Code fix specification",
	Schema: `{
  "fixes": [
    {
      "file": "relative/path/to/file.go",
      "line": 42,
      "old_code": "the exact code to replace",
      "new_code": "the replacement code",
      "reason": "one sentence explaining why"
    }
  ],
  "affected_nodes": ["repo:package:function"],
  "confidence": 0.95
}`,
	Example: `{
  "fixes": [
    {
      "file": "internal/auth/validate.go",
      "line": 42,
      "old_code": "func ValidateToken(token int) error {",
      "new_code": "func ValidateToken(token string) error {",
      "reason": "Token parameter should be string, not int — matches API contract"
    }
  ],
  "affected_nodes": ["auth-service:auth:ValidateToken"],
  "confidence": 0.92
}`,
}

var TestOutputSchema = OutputSchema{
	Name:        "test",
	Description: "Generated test specification",
	Schema: `{
  "tests": [
    {
      "file": "relative/path/to/file_test.go",
      "function_name": "TestFunctionName",
      "content": "full test function code",
      "covers_node": "repo:package:function"
    }
  ],
  "test_command": "go test ./path/to/package -run TestName"
}`,
	Example: `{
  "tests": [
    {
      "file": "internal/auth/validate_test.go",
      "function_name": "TestValidateToken_StringInput",
      "content": "func TestValidateToken_StringInput(t *testing.T) {\n\terr := ValidateToken(\"abc123\")\n\tif err != nil {\n\t\tt.Fatalf(\"expected nil error, got %v\", err)\n\t}\n}",
      "covers_node": "auth-service:auth:ValidateToken"
    }
  ],
  "test_command": "go test ./internal/auth -run TestValidateToken_StringInput"
}`,
}

var PROutputSchema = OutputSchema{
	Name:        "pr",
	Description: "Pull request metadata",
	Schema: `{
  "title": "short PR title",
  "body": "markdown PR description",
  "labels": ["bug", "cross-repo"],
  "reviewers": ["username"],
  "affected_repos": ["repo-name"]
}`,
	Example: `{
  "title": "fix: correct token type mismatch in auth.ValidateToken",
  "body": "## What changed\nChanged token parameter from int to string in ValidateToken to match the API contract defined in gateway-service.\n\n## Impact\n- auth-service: validate.go line 42\n- 3 callers updated in gateway-service\n\n## Testing\n- Added TestValidateToken_StringInput\n- All existing tests pass",
  "labels": ["bug", "cross-repo"],
  "reviewers": [],
  "affected_repos": ["auth-service", "gateway-service"]
}`,
}

var AnalysisOutputSchema = OutputSchema{
	Name:        "analysis",
	Description: "Impact analysis result",
	Schema: `{
  "root_cause": "one sentence",
  "affected_nodes": [
    {
      "node_id": "repo:package:function",
      "impact": "high|medium|low",
      "reason": "why this node is affected"
    }
  ],
  "suggested_fix": "brief fix description",
  "risk_level": "high|medium|low",
  "cross_repo": true
}`,
	Example: `{
  "root_cause": "Type mismatch — auth.ValidateToken expects int but gateway sends string",
  "affected_nodes": [
    {
      "node_id": "auth-service:auth:ValidateToken",
      "impact": "high",
      "reason": "Direct type mismatch on parameter"
    },
    {
      "node_id": "gateway-service:handlers:LoginHandler",
      "impact": "medium",
      "reason": "Caller — sends string token to ValidateToken"
    }
  ],
  "suggested_fix": "Change ValidateToken parameter from int to string",
  "risk_level": "medium",
  "cross_repo": true
}`,
}

func GetOutputSchema(taskType TaskType) *OutputSchema {
	switch taskType {
	case TaskFix:
		return &FixOutputSchema
	case TaskTest:
		return &TestOutputSchema
	case TaskPR:
		return &PROutputSchema
	case TaskAnalysis:
		return &AnalysisOutputSchema
	default:
		return nil
	}
}

func FormatSchemaPrompt(schema *OutputSchema) string {
	var b strings.Builder
	b.WriteString("OUTPUT SCHEMA — ")
	b.WriteString(schema.Description)
	b.WriteString(":\n")
	b.WriteString(schema.Schema)
	b.WriteString("\n\nEXAMPLE:\n")
	b.WriteString(schema.Example)
	b.WriteString("\n\nRespond with ONLY the JSON. No markdown fences. No explanation. No preamble.")
	return b.String()
}

type FixOutput struct {
	Fixes []struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		OldCode string `json:"old_code"`
		NewCode string `json:"new_code"`
		Reason  string `json:"reason"`
	} `json:"fixes"`
	AffectedNodes []string `json:"affected_nodes"`
	Confidence    float64  `json:"confidence"`
}

func ParseFixOutput(jsonStr string) (*FixOutput, error) {
	clean := strings.TrimSpace(stripCodeFences(jsonStr))
	var out FixOutput
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid fix output JSON: %w", err)
	}
	if out.Fixes == nil {
		return nil, fmt.Errorf("missing required field: fixes")
	}
	return &out, nil
}

type TestOutput struct {
	Tests []struct {
		File         string `json:"file"`
		FunctionName string `json:"function_name"`
		Content      string `json:"content"`
		CoversNode   string `json:"covers_node"`
	} `json:"tests"`
	TestCommand string `json:"test_command"`
}

func ParseTestOutput(jsonStr string) (*TestOutput, error) {
	clean := strings.TrimSpace(stripCodeFences(jsonStr))
	var out TestOutput
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid test output JSON: %w", err)
	}
	if out.Tests == nil {
		return nil, fmt.Errorf("missing required field: tests")
	}
	return &out, nil
}

type PROutput struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Labels        []string `json:"labels"`
	Reviewers     []string `json:"reviewers"`
	AffectedRepos []string `json:"affected_repos"`
}

func ParsePROutput(jsonStr string) (*PROutput, error) {
	clean := strings.TrimSpace(stripCodeFences(jsonStr))
	var out PROutput
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid PR output JSON: %w", err)
	}
	if out.Title == "" {
		return nil, fmt.Errorf("missing required field: title")
	}
	return &out, nil
}

type AnalysisOutput struct {
	RootCause     string `json:"root_cause"`
	AffectedNodes []struct {
		NodeID string `json:"node_id"`
		Impact string `json:"impact"`
		Reason string `json:"reason"`
	} `json:"affected_nodes"`
	SuggestedFix string `json:"suggested_fix"`
	RiskLevel    string `json:"risk_level"`
	CrossRepo    bool   `json:"cross_repo"`
}

func ParseAnalysisOutput(jsonStr string) (*AnalysisOutput, error) {
	clean := strings.TrimSpace(stripCodeFences(jsonStr))
	var out AnalysisOutput
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid analysis output JSON: %w", err)
	}
	if out.RootCause == "" {
		return nil, fmt.Errorf("missing required field: root_cause")
	}
	return &out, nil
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = s[len("```json"):]
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	} else if strings.HasPrefix(s, "```") {
		s = s[3:]
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(strings.TrimSpace(s), "```") {
		s = strings.TrimSpace(s)
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}
