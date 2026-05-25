package orchestrator

import "time"

// ============================================================
// MODELS
// ============================================================

type ModelTier string

const (
	Opus  ModelTier = "opus"
	Haiku ModelTier = "haiku"
)

type ModelConfig struct {
	Tier           ModelTier
	ModelID        string
	APIKey         string
	Endpoint       string
	MaxTokens      int
	InputCostPerM  float64
	OutputCostPerM float64
	MaxConcurrent  int
}

// ============================================================
// ROUTING
// ============================================================

type RoutingMode string

const (
	ModeSkillExecute    RoutingMode = "skill_execute"
	ModeMemoryApply     RoutingMode = "memory_apply"
	ModePlanExecute     RoutingMode = "plan_execute"
	ModeFullOrchestration RoutingMode = "full_orchestration"
	ModeSingleOpus      RoutingMode = "single_opus"
	ModeSingleHaiku     RoutingMode = "single_haiku"
)

type RoutingDecision struct {
	Mode           RoutingMode `json:"mode"`
	// Role-based fields — the agent maps these to its configured models.
	// Values: "premium", "low_cost", or "automated" (for verify tier 1).
	PlannerRole    string      `json:"planner_role"`
	ExecutorRole   string      `json:"executor_role"`
	VerifierRole   string      `json:"verifier_role"`
	// NextStep tells the agent what to do first.
	// Values: "plan", "execute", "verify", "direct", "skill_execute", "memory_apply"
	NextStep       string      `json:"next_step"`
	VerifyTier     VerifyTier  `json:"verify_tier"`
	SkipPlanning   bool        `json:"skip_planning"`
	MatchedSkillID string      `json:"matched_skill_id,omitempty"`
	MemoryHit      bool        `json:"memory_hit"`
	TemplateID     string      `json:"template_id,omitempty"`
	Reason         string      `json:"reason"`
}

type VerifyTier int

const (
	VerifyAutomated  VerifyTier = 1
	VerifySpotCheck  VerifyTier = 2
	VerifyFullReview VerifyTier = 3
)

// ============================================================
// TASK
// ============================================================

type Task struct {
	ID           string    `json:"id"`
	DeveloperID  string    `json:"developer_id"`
	RepoID       string    `json:"repo_id"`
	Prompt       string    `json:"prompt"`
	GraphNodeIDs []string  `json:"graph_node_ids"`
	TaskType     TaskType  `json:"task_type"`
	CreatedAt    time.Time `json:"created_at"`
}

type TaskType string

const (
	TaskCodeFix      TaskType = "code_fix"
	TaskTestGen      TaskType = "test_gen"
	TaskPRGen        TaskType = "pr_gen"
	TaskRefactor     TaskType = "refactor"
	TaskDepUpdate    TaskType = "dependency_update"
	TaskConfigChange TaskType = "config_change"
	TaskMigration    TaskType = "migration"
	TaskAnalysis     TaskType = "analysis"
	TaskExplanation  TaskType = "explanation"
	TaskGeneral      TaskType = "general"
)

// ============================================================
// PLAN
// ============================================================

type Plan struct {
	TaskID   string    `json:"task_id"`
	SubTasks []SubTask `json:"sub_tasks"`
	PRTitle  string    `json:"pr_title,omitempty"`
	PRBody   string    `json:"pr_body,omitempty"`
	TestCmd  string    `json:"test_command,omitempty"`
}

type SubTask struct {
	ID         string      `json:"id"`
	Action     string      `json:"action"`
	DependsOn  []string    `json:"depends_on"`
	Spec       interface{} `json:"spec"`
	VerifyTier VerifyTier  `json:"verify_tier"`
}

type SubTaskResult struct {
	SubTaskID    string    `json:"sub_task_id"`
	Success      bool      `json:"success"`
	Output       string    `json:"output"`
	ErrorMessage string    `json:"error,omitempty"`
	TokensUsed   int       `json:"tokens_used"`
	LatencyMs    int       `json:"latency_ms"`
	Model        ModelTier `json:"model"`
}

// ============================================================
// ESCALATION
// ============================================================

type EscalationStep struct {
	Step    int    `json:"step"`
	Action  string `json:"action"`
	Error   string `json:"error"`
	Success bool   `json:"success"`
}

// ============================================================
// COST TRACKING
// ============================================================

type CostRecord struct {
	ID              string      `json:"id"`
	TaskID          string      `json:"task_id"`
	DeveloperID     string      `json:"developer_id"`
	Model           ModelTier   `json:"model"`
	InputTokens     int         `json:"input_tokens"`
	OutputTokens    int         `json:"output_tokens"`
	CostUSD         float64     `json:"cost_usd"`
	Phase           string      `json:"phase"`
	RoutingMode     RoutingMode `json:"routing_mode"`
	SkillID         string      `json:"skill_id,omitempty"`
	MemoryHit       bool        `json:"memory_hit"`
	EscalationSteps int         `json:"escalation_steps"`
	WasTakeover     bool        `json:"was_takeover"`
	LatencyMs       int         `json:"latency_ms"`
	RepoID          string      `json:"repo_id"`
}

// ============================================================
// TASK RESULT
// ============================================================

type TaskResult struct {
	TaskID      string             `json:"task_id"`
	Success     bool               `json:"success"`
	Output      string             `json:"output"`
	RoutingMode RoutingMode        `json:"routing_mode"`
	TotalCost   float64            `json:"total_cost_usd"`
	TotalTokens int                `json:"total_tokens"`
	LatencyMs   int                `json:"latency_ms"`
	SubResults  []SubTaskResult    `json:"sub_results,omitempty"`
	Escalations []EscalationRecord `json:"escalations,omitempty"`
	SkillUsed   string             `json:"skill_used,omitempty"`
	MemoryHit   bool               `json:"memory_hit"`
}

// ============================================================
// CONFIGURATION
// ============================================================

type Config struct {
	OpusConfig  ModelConfig
	HaikuConfig ModelConfig

	SkillMatchMinSuccessRate  float64
	MemoryMatchMinConfidence  float64
	SimpleTaskMaxNodes        int
	MinTaskSizeForSplit       int

	MaxHaikuRetries           int
	MaxTotalAttempts          int

	AsyncVerifyForInteractive bool

	MaxParallelHaikuCalls int

	OpusCallsPerMinute  int
	HaikuCallsPerMinute int
	RetryBackoffBase    int

	DatabaseURL string
}

func DefaultConfig() Config {
	return Config{
		OpusConfig: ModelConfig{
			Tier:           Opus,
			ModelID:        "claude-opus-4-20250514",
			Endpoint:       "https://api.anthropic.com/v1/messages",
			MaxTokens:      4096,
			InputCostPerM:  15.0,
			OutputCostPerM: 75.0,
			MaxConcurrent:  3,
		},
		HaikuConfig: ModelConfig{
			Tier:           Haiku,
			ModelID:        "claude-haiku-4-5-20251001",
			Endpoint:       "https://api.anthropic.com/v1/messages",
			MaxTokens:      4096,
			InputCostPerM:  0.25,
			OutputCostPerM: 1.25,
			MaxConcurrent:  10,
		},
		SkillMatchMinSuccessRate:  0.85,
		MemoryMatchMinConfidence:  0.80,
		SimpleTaskMaxNodes:        3,
		MinTaskSizeForSplit:       300,
		MaxHaikuRetries:           2,
		MaxTotalAttempts:          3,
		AsyncVerifyForInteractive: true,
		MaxParallelHaikuCalls:     5,
		OpusCallsPerMinute:        30,
		HaikuCallsPerMinute:       100,
		RetryBackoffBase:          1000,
	}
}
