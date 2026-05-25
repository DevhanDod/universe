package orchestrator

import "fmt"

// ============================================================
// SPEC TEMPLATES
// ============================================================

type CodeFixSpec struct {
	Changes []CodeChange `json:"changes"`
}

type CodeChange struct {
	File        string `json:"file"`
	LineRange   [2]int `json:"line_range"`
	CurrentCode string `json:"current_code"`
	TargetCode  string `json:"target_code"`
	Reason      string `json:"reason"`
}

type TestGenSpec struct {
	Tests []TestCase `json:"tests"`
}

type TestCase struct {
	File         string `json:"file"`
	FunctionName string `json:"function_name"`
	Description  string `json:"description"`
	Setup        string `json:"setup"`
	Input        string `json:"input"`
	Expected     string `json:"expected"`
	CoversNode   string `json:"covers_node"`
}

type PRGenSpec struct {
	Title         string   `json:"title"`
	ChangeSummary string   `json:"change_summary"`
	AffectedFiles []string `json:"affected_files"`
	AffectedNodes []string `json:"affected_nodes"`
	TestingNotes  string   `json:"testing_notes"`
	Labels        []string `json:"labels"`
}

type RefactorSpec struct {
	Objective string       `json:"objective"`
	Changes   []CodeChange `json:"changes"`
	MoveFiles []FileMove   `json:"move_files"`
}

type FileMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DepUpdateSpec struct {
	Dependency    string       `json:"dependency"`
	FromVersion   string       `json:"from_version"`
	ToVersion     string       `json:"to_version"`
	CodeChanges   []CodeChange `json:"code_changes"`
	ConfigChanges []CodeChange `json:"config_changes"`
}

type ConfigChangeSpec struct {
	Changes []ConfigEntry `json:"changes"`
}

type ConfigEntry struct {
	File     string `json:"file"`
	Key      string `json:"key"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
	Format   string `json:"format"`
}

type AnalysisSpec struct {
	Question      string `json:"question"`
	Scope         string `json:"scope"`
	AffectedNodes []struct {
		NodeID string `json:"node_id"`
		Impact string `json:"impact"`
		Reason string `json:"reason"`
	} `json:"affected_nodes"`
	RiskLevel      string `json:"risk_level"`
	Recommendation string `json:"recommendation"`
}

// ============================================================
// TEMPLATE REGISTRY
// ============================================================

func HasTemplate(taskType TaskType) bool {
	switch taskType {
	case TaskCodeFix, TaskTestGen, TaskPRGen, TaskRefactor,
		TaskDepUpdate, TaskConfigChange, TaskAnalysis:
		return true
	case TaskExplanation, TaskGeneral, TaskMigration:
		return false
	default:
		return false
	}
}

func GetTemplateName(taskType TaskType) string {
	return string(taskType)
}

// GetPlannerPrompt returns the Opus system prompt for a planning step.
func GetPlannerPrompt(taskType TaskType, graphContext string, memoryContext string, skillContext string) string {
	schema := plannerSchema(taskType)

	prompt := fmt.Sprintf(`You are a task planner. Analyze the developer's request and produce a structured specification.

OUTPUT FORMAT — respond with ONLY valid JSON matching this schema:
%s

RULES:
- All string fields must be non-empty
- current_code MUST match exactly what is in the file
- target_code MUST be complete, compilable code
- reason MUST be one sentence
- Max 5 sub-tasks per plan
- Set verify_tier: 1 for code changes, 2 for PR/docs, 3 for cross-repo changes
- Set depends_on when order matters (tests depend on code changes)
- Independent sub-tasks should have empty depends_on (enables parallel execution)
- Do NOT include any text outside the JSON`, schema)

	if graphContext != "" {
		prompt += "\n\nGRAPH CONTEXT:\n" + graphContext
	}
	if memoryContext != "" {
		prompt += "\n\nMEMORY CONTEXT (past solutions):\n" + memoryContext
	}
	if skillContext != "" {
		prompt += "\n\nSKILL REFERENCE (known recipe):\n" + skillContext
	}

	return prompt
}

func plannerSchema(taskType TaskType) string {
	switch taskType {
	case TaskCodeFix:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "modify_file",
      "depends_on": [],
      "spec": {
        "changes": [
          {
            "file": "path/to/file.go",
            "line_range": [40, 45],
            "current_code": "exact current code",
            "target_code": "exact replacement code",
            "reason": "why this change"
          }
        ]
      },
      "verify_tier": 1
    }
  ],
  "test_command": "go test ./...",
  "pr_title": "fix: description"
}`
	case TaskTestGen:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "generate_test",
      "depends_on": [],
      "spec": {
        "tests": [
          {
            "file": "internal/pkg/file_test.go",
            "function_name": "TestFuncName_Case",
            "description": "what this test verifies",
            "setup": "any setup code",
            "input": "test input",
            "expected": "expected output",
            "covers_node": "graph_node_id"
          }
        ]
      },
      "verify_tier": 1
    }
  ]
}`
	case TaskPRGen:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "generate_pr",
      "depends_on": [],
      "spec": {
        "title": "type: short description",
        "change_summary": "2-3 sentence summary",
        "affected_files": ["path/to/file.go"],
        "affected_nodes": ["node_id"],
        "testing_notes": "how to test",
        "labels": ["bug", "enhancement"]
      },
      "verify_tier": 2
    }
  ]
}`
	case TaskRefactor:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "modify_file",
      "depends_on": [],
      "spec": {
        "objective": "one sentence: what the refactor achieves",
        "changes": [
          {
            "file": "path/to/file.go",
            "line_range": [10, 20],
            "current_code": "old code",
            "target_code": "new code",
            "reason": "why"
          }
        ],
        "move_files": []
      },
      "verify_tier": 1
    }
  ],
  "test_command": "go test ./..."
}`
	case TaskAnalysis:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "analyze",
      "depends_on": [],
      "spec": {
        "question": "what to analyze",
        "scope": "function",
        "affected_nodes": [
          {"node_id": "id", "impact": "high", "reason": "why"}
        ],
        "risk_level": "medium",
        "recommendation": "one sentence"
      },
      "verify_tier": 2
    }
  ]
}`
	default:
		return `{
  "sub_tasks": [
    {
      "id": "unique_id",
      "action": "modify_file",
      "depends_on": [],
      "spec": {},
      "verify_tier": 1
    }
  ]
}`
	}
}
