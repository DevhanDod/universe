package orchestrator

import "time"

// ============================================================
// MODELS — developer configures their own model preferences
// ============================================================

// ModelConfig represents a developer's chosen model.
// Universe doesn't call these models — Cursor does.
// This config is used for cost estimation and dashboard display.
type ModelConfig struct {
	Name           string  `json:"name"`             // "claude-opus-4", "gpt-4o", etc.
	Provider       string  `json:"provider"`          // "anthropic", "openai", "google"
	InputCostPerM  float64 `json:"input_cost_per_m"`  // cost per 1M input tokens
	OutputCostPerM float64 `json:"output_cost_per_m"` // cost per 1M output tokens
}

// ============================================================
// PLANS — the bridge between planner and executor agents
// ============================================================

// Plan is a task specification created by the planner agent (premium model).
// Stored in PostgreSQL. Retrieved by the executor agent (cheap model).
type Plan struct {
	ID               string     `json:"id"`
	DeveloperID      string     `json:"developer_id"`
	Title            string     `json:"title"`
	TaskPrompt       string     `json:"task_prompt"`
	Steps            []string   `json:"steps"`
	FilesToChange    []string   `json:"files_to_change,omitempty"`
	SkillUsed        string     `json:"skill_used,omitempty"`
	SkillVerified    bool       `json:"skill_verified"`
	GraphContext     string     `json:"graph_context,omitempty"`
	AffectedNodes    []string   `json:"affected_nodes,omitempty"`
	CrossRepo        bool       `json:"cross_repo"`
	RiskLevel        string     `json:"risk_level,omitempty"`
	PlannerModel     string     `json:"planner_model,omitempty"`
	ExecutorModel    string     `json:"executor_model,omitempty"`
	Status           PlanStatus `json:"status"`
	ResultSuccess    *bool      `json:"result_success,omitempty"`
	ResultSummary    string     `json:"result_summary,omitempty"`
	ResultFiles      []string   `json:"result_files,omitempty"`
	ResultTests      *bool      `json:"result_tests,omitempty"`
	ResultError      string     `json:"result_error,omitempty"`
	Verified         *bool      `json:"verified,omitempty"`
	VerificationNote string     `json:"verification_note,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ExecutedAt       *time.Time `json:"executed_at,omitempty"`
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
}

type PlanStatus string

const (
	PlanPending   PlanStatus = "pending"
	PlanExecuting PlanStatus = "executing"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanVerified  PlanStatus = "verified"
	PlanRejected  PlanStatus = "rejected"
)

// PlanSummary is the compact version for listing plans.
type PlanSummary struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Status        PlanStatus `json:"status"`
	StepCount     int        `json:"step_count"`
	SkillUsed     bool       `json:"skill_used"`
	CrossRepo     bool       `json:"cross_repo"`
	PlannerModel  string     `json:"planner_model"`
	ExecutorModel string     `json:"executor_model"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ============================================================
// COST TRACKING
// ============================================================

// PlanCost tracks the estimated cost of a plan execution.
// These are ESTIMATES based on the developer's configured model pricing.
// Universe doesn't see actual Cursor token counts.
type PlanCost struct {
	ID                      string  `json:"id"`
	PlanID                  string  `json:"plan_id"`
	DeveloperID             string  `json:"developer_id"`
	PlannerModel            string  `json:"planner_model"`
	ExecutorModel           string  `json:"executor_model"`
	EstimatedPlannerTokens  int     `json:"estimated_planner_tokens"`
	EstimatedExecutorTokens int     `json:"estimated_executor_tokens"`
	EstimatedPlannerCost    float64 `json:"estimated_planner_cost"`
	EstimatedExecutorCost   float64 `json:"estimated_executor_cost"`
	EstimatedTotalCost      float64 `json:"estimated_total_cost"`
	EstimatedAllPremiumCost float64 `json:"estimated_all_premium_cost"`
	EstimatedSavings        float64 `json:"estimated_savings"`
	SkillUsed               bool    `json:"skill_used"`
	MemoryHit               bool    `json:"memory_hit"`
	RoutingRecommendation   string  `json:"routing_recommendation"`
}

// ============================================================
// ROUTING — recommendation, not decision
// ============================================================

// RoutingRecommendation is what the router suggests to the planner.
// The planner (premium model in Cursor) reads this and decides what to do.
// Universe doesn't enforce routing — it just provides information.
type RoutingRecommendation struct {
	SkillAvailable    bool    `json:"skill_available"`
	SkillID           string  `json:"skill_id,omitempty"`
	SkillName         string  `json:"skill_name,omitempty"`
	SkillConfidence   float64 `json:"skill_confidence,omitempty"`
	MemoryAvailable   bool    `json:"memory_available"`
	MemoryCount       int     `json:"memory_count,omitempty"`
	AffectedNodeCount int     `json:"affected_node_count"`
	CrossRepo         bool    `json:"cross_repo"`
	RiskLevel         string  `json:"risk_level"` // "low", "medium", "high"
	Recommendation    string  `json:"recommendation"`
}

// ============================================================
// CONFIGURATION
// ============================================================

type Config struct {
	DatabaseURL string

	// Developer's model preferences (from universe config)
	PremiumModel   ModelConfig
	ExecutionModel ModelConfig

	// Auto-open executor workspace after store_plan?
	AutoOpenExecutor bool // default: true

	// Workspace file paths
	PlannerWorkspacePath  string // default: ".universe/workspaces/planner.code-workspace"
	ExecutorWorkspacePath string // default: ".universe/workspaces/executor.code-workspace"
}

func DefaultConfig() Config {
	return Config{
		PremiumModel: ModelConfig{
			Name:           "claude-opus-4",
			Provider:       "anthropic",
			InputCostPerM:  15.0,
			OutputCostPerM: 75.0,
		},
		ExecutionModel: ModelConfig{
			Name:           "claude-haiku-3.5",
			Provider:       "anthropic",
			InputCostPerM:  0.25,
			OutputCostPerM: 1.25,
		},
		AutoOpenExecutor:      true,
		PlannerWorkspacePath:  ".universe/workspaces/planner.code-workspace",
		ExecutorWorkspacePath: ".universe/workspaces/executor.code-workspace",
	}
}
