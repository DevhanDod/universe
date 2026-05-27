package dashboard

import "time"

// ── Overview ──────────────────────────────────────────────────────────────────

type EngineStatus struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Status string `json:"status"` // "active", "degraded", "disabled"
	Detail string `json:"detail"`
}

type MonthlySummary struct {
	ActualCost    float64 `json:"actual_cost"`
	WouldHaveCost float64 `json:"would_have_cost"`
	SavingsUSD    float64 `json:"savings_usd"`
	SavingsPct    float64 `json:"savings_pct"`
	TotalTasks    int     `json:"total_tasks"`
	HaikuPct      float64 `json:"haiku_pct"`
	SkillUses     int     `json:"skill_uses"`
	MemoryHits    int     `json:"memory_hits"`
	Takeovers     int     `json:"takeovers"`
}

type MonthlyDataPoint struct {
	Month     string  `json:"month"`
	Actual    float64 `json:"actual"`
	WouldHave float64 `json:"would_have"`
}

type OverviewResponse struct {
	Engines []EngineStatus     `json:"engines"`
	Monthly MonthlySummary     `json:"monthly"`
	Trend   []MonthlyDataPoint `json:"trend"`
}

// ── Memory ────────────────────────────────────────────────────────────────────

type ObservationRow struct {
	ID          string     `json:"id"`
	DeveloperID string     `json:"developer_id"`
	RepoID      string     `json:"repo_id"`
	GraphNodeID string     `json:"graph_node_id"`
	Category    string     `json:"category"`
	Summary     string     `json:"summary"`
	Confidence  float64    `json:"confidence"`
	Shared      bool       `json:"shared"`
	CreatedAt   time.Time  `json:"created_at"`
	RecalledAt  *time.Time `json:"recalled_at"`
}

type ObservationDetail struct {
	ObservationRow
	Detail    string     `json:"detail"`
	SessionID string     `json:"session_id"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

type ToolCall struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
}

type ObservationListResponse struct {
	Observations   []ObservationRow  `json:"observations"`
	Total          int               `json:"total"`
	Page           int               `json:"page"`
	Limit          int               `json:"limit"`
	FiltersApplied map[string]string `json:"filters_applied"`
}

type ObservationFilters struct {
	DeveloperID string
	Category    string
	GraphNodeID string
	RepoID      string
	From        time.Time
	To          time.Time
	Page        int
	Limit       int
}

// ── Skills ────────────────────────────────────────────────────────────────────

type SkillRow struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Version        int       `json:"version"`
	Evolution      string    `json:"evolution"`
	Language       string    `json:"language"`
	TriggerDesc    string    `json:"trigger_desc"`
	GraphNodeIDs   []string  `json:"graph_node_ids"`
	Confidence     float64   `json:"confidence"`
	SuccessRate    float64   `json:"success_rate"`
	TimesApplied   int       `json:"times_applied"`
	TimesSucceeded int       `json:"times_succeeded"`
	IsFrozen       bool      `json:"is_frozen"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      string    `json:"created_by"`
}

type SkillDetail struct {
	SkillRow
	ParentID            *string     `json:"parent_id,omitempty"`
	Instruction         string      `json:"instruction"`
	NegativeTags        interface{} `json:"negative_tags"`
	SuccessByComplexity interface{} `json:"success_by_complexity"`
	TestCase            interface{} `json:"test_case,omitempty"`
}

type SkillStats struct {
	TotalActive    int            `json:"total_active"`
	TotalFrozen    int            `json:"total_frozen"`
	ByEvolution    map[string]int `json:"by_evolution"`
	AvgSuccessRate float64        `json:"avg_success_rate"`
}

type SkillListResponse struct {
	Skills []SkillRow `json:"skills"`
	Stats  SkillStats `json:"stats"`
}

type SkillLineageEntry struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Version   int     `json:"version"`
	Evolution string  `json:"evolution"`
	ParentID  *string `json:"parent_id"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

type SkillLineageResponse struct {
	Lineage []SkillLineageEntry `json:"lineage"`
	Derived []SkillLineageEntry `json:"derived"`
}

type SkillFilters struct {
	Language    string
	Sort        string
	GraphNodeID string
}

// ── Compression ───────────────────────────────────────────────────────────────

type CompressionSample struct {
	TaskID          string    `json:"task_id"`
	Timestamp       time.Time `json:"timestamp"`
	Level           string    `json:"level"`
	BeforeTokens    int       `json:"before_tokens"`
	AfterTokens     int       `json:"after_tokens"`
	SavingsPct      float64   `json:"savings_pct"`
	BeforePreview   string    `json:"before_preview"`
	AfterPreview    string    `json:"after_preview"`
	GraphShorthand  string    `json:"graph_shorthand"`
}

type CompressionStats struct {
	AvgOutputReduction  float64 `json:"avg_output_reduction"`
	AvgInputReduction   float64 `json:"avg_input_reduction"`
	ActiveLevel         string  `json:"active_level"`
	TotalTokensSavedToday int   `json:"total_tokens_saved_today"`
}

type CompressionSamplesResponse struct {
	Samples []CompressionSample `json:"samples"`
	Stats   CompressionStats    `json:"stats"`
}

// ── Routing ───────────────────────────────────────────────────────────────────

type RoutingTaskRow struct {
	TaskID          string    `json:"task_id"`
	DeveloperID     string    `json:"developer_id"`
	PromptPreview   string    `json:"prompt_preview"`
	RoutingMode     string    `json:"routing_mode"`
	TotalTokens     int       `json:"total_tokens"`
	TotalCost       float64   `json:"total_cost"`
	WouldHaveCost   float64   `json:"would_have_cost"`
	LatencyMS       int       `json:"latency_ms"`
	EscalationSteps int       `json:"escalation_steps"`
	WasTakeover     bool      `json:"was_takeover"`
	MemoryHit       bool      `json:"memory_hit"`
	CreatedAt       time.Time `json:"created_at"`
}

type RoutingTraceStep struct {
	Step       int    `json:"step"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	Tokens     int    `json:"tokens"`
	Model      string `json:"model"`
	DurationMS int    `json:"duration_ms"`
}

type RoutingDetail struct {
	TaskID        string             `json:"task_id"`
	DeveloperID   string             `json:"developer_id"`
	Prompt        string             `json:"prompt"`
	RoutingMode   string             `json:"routing_mode"`
	RoutingReason string             `json:"routing_reason"`
	Trace         []RoutingTraceStep `json:"trace"`
	TotalTokens   int                `json:"total_tokens"`
	TotalCost     float64            `json:"total_cost"`
	WouldHaveCost float64            `json:"would_have_cost"`
	TotalLatencyMS int               `json:"total_latency_ms"`
	CreatedAt     time.Time          `json:"created_at"`
}

type RoutingStats struct {
	TasksToday    int            `json:"tasks_today"`
	HaikuPct      float64        `json:"haiku_pct"`
	CostToday     float64        `json:"cost_today"`
	TakeoversToday int           `json:"takeovers_today"`
	ByRoutingMode map[string]int `json:"by_routing_mode"`
}

type RoutingListResponse struct {
	Tasks []RoutingTaskRow `json:"tasks"`
	Total int              `json:"total"`
	Page  int              `json:"page"`
	Stats RoutingStats     `json:"stats"`
}

type RoutingFilters struct {
	DeveloperID string
	RoutingMode string
	From        time.Time
	To          time.Time
	Page        int
	Limit       int
}

// ── Plans ─────────────────────────────────────────────────────────────────────

type PlanRow struct {
	ID            string     `json:"id"`
	DeveloperID   string     `json:"developer_id"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	StepCount     int        `json:"step_count"`
	SkillUsed     bool       `json:"skill_used"`
	SkillVerified bool       `json:"skill_verified"`
	CrossRepo     bool       `json:"cross_repo"`
	RiskLevel     string     `json:"risk_level"`
	PlannerModel  string     `json:"planner_model"`
	ExecutorModel string     `json:"executor_model"`
	ResultSuccess *bool      `json:"result_success,omitempty"`
	ResultTests   *bool      `json:"result_tests,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExecutedAt    *time.Time `json:"executed_at,omitempty"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
}

type PlanDetail struct {
	PlanRow
	TaskPrompt       string   `json:"task_prompt"`
	Steps            []string `json:"steps"`
	FilesToChange    []string `json:"files_to_change"`
	GraphContext     string   `json:"graph_context"`
	AffectedNodes    []string `json:"affected_nodes"`
	ResultSummary    string   `json:"result_summary"`
	ResultFiles      []string `json:"result_files"`
	ResultError      string   `json:"result_error"`
	VerificationNote string   `json:"verification_note"`
	EstimatedCost    float64  `json:"estimated_cost"`
	AllPremiumCost   float64  `json:"all_premium_cost"`
	EstimatedSavings float64  `json:"estimated_savings"`
}

type PlanStatsResponse struct {
	TotalPlans      int     `json:"total_plans"`
	Completed       int     `json:"completed"`
	Failed          int     `json:"failed"`
	Verified        int     `json:"verified"`
	Rejected        int     `json:"rejected"`
	Pending         int     `json:"pending"`
	CompletionRate  float64 `json:"completion_rate"`
	VerificationRate float64 `json:"verification_rate"`
	AvgStepsPerPlan float64 `json:"avg_steps_per_plan"`
	SkillUsedCount  int     `json:"skill_used_count"`
	CrossRepoCount  int     `json:"cross_repo_count"`
}

type PlanListResponse struct {
	Plans  []PlanRow `json:"plans"`
	Total  int       `json:"total"`
	Page   int       `json:"page"`
	Limit  int       `json:"limit"`
	Stats  PlanStatsResponse `json:"stats"`
}

type PlanFilters struct {
	DeveloperID string
	Status      string
	Page        int
	Limit       int
}

// ── Graph ─────────────────────────────────────────────────────────────────────

type GraphNodeRow struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Package      string  `json:"package"`
	Repo         string  `json:"repo"`
	File         string  `json:"file"`
	MemoryCount  int     `json:"memory_count"`
	SkillCount   int     `json:"skill_count"`
	HasStaleSkill bool   `json:"has_stale_skill"`
}

type GraphEdgeRow struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type GraphNodesResponse struct {
	Nodes []GraphNodeRow `json:"nodes"`
}

type GraphEdgesResponse struct {
	Edges []GraphEdgeRow `json:"edges"`
}

type GraphNodeDetailResponse struct {
	Node        GraphNodeRow   `json:"node"`
	Callers     []string       `json:"callers"`
	Callees     []string       `json:"callees"`
	Memories    []ObservationRow `json:"memories"`
	Skills      []SkillRow     `json:"skills"`
	RecentRoutes []RoutingTaskRow `json:"recent_routes"`
}
