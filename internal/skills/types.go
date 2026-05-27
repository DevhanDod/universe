package skills

import "time"

// Skill is one version of a reusable recipe.
type Skill struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Version                int               `json:"version"`
	ParentID               *string           `json:"parent_id,omitempty"`
	Evolution              EvolutionType     `json:"evolution"`
	GraphNodeIDs           []string          `json:"graph_node_ids"`
	Language               string            `json:"language"`
	TriggerDesc            string            `json:"trigger_desc"`
	Instruction            string            `json:"instruction"`
	TestCase               *SkillTestCase    `json:"test_case,omitempty"`
	NegativeTags           []NegativeTag     `json:"negative_tags"`
	Embedding              []float32         `json:"embedding,omitempty"`
	CreatedBy              string            `json:"created_by"`
	Shared                 bool              `json:"shared"`
	IsActive               bool              `json:"is_active"`
	IsFrozen               bool              `json:"is_frozen"`
	EvolutionAttemptsToday int               `json:"evolution_attempts_today"`
	TimesApplied           int               `json:"times_applied"`
	TimesSucceeded         int               `json:"times_succeeded"`
	TimesFailed            int               `json:"times_failed"`
	SuccessByComplexity    ComplexityMetrics `json:"success_by_complexity"`
	AvgTokensSaved         float64           `json:"avg_tokens_saved"`
	Confidence             float64           `json:"confidence"`
	LastAppliedAt          *time.Time        `json:"last_applied_at,omitempty"`
	CreatedAt              time.Time         `json:"created_at"`
}

// EvolutionType describes how a skill version was created.
type EvolutionType string

const (
	EvolutionCaptured EvolutionType = "captured"
	EvolutionFix      EvolutionType = "fix"
	EvolutionDerived  EvolutionType = "derived"
	EvolutionManual   EvolutionType = "manual"
)

// SkillTestCase is a captured test for the skill.
type SkillTestCase struct {
	Input          string   `json:"input"`
	ExpectedOutput string   `json:"expected_output"`
	GraphNodeIDs   []string `json:"graph_node_ids"`
}

// NegativeTag records a context where the skill should NOT be used.
type NegativeTag struct {
	Context string `json:"context"`
	Reason  string `json:"reason"`
	AddedAt string `json:"added_at"`
}

// ComplexityMetrics tracks success rates by task complexity.
type ComplexityMetrics struct {
	Simple  ComplexityBucket `json:"simple"`
	Complex ComplexityBucket `json:"complex"`
}

type ComplexityBucket struct {
	Applied   int `json:"applied"`
	Succeeded int `json:"succeeded"`
}

// SkillSummary is the compact version for search results.
type SkillSummary struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Version             int               `json:"version"`
	Evolution           EvolutionType     `json:"evolution"`
	TriggerDesc         string            `json:"trigger_desc"`
	Language            string            `json:"language"`
	Confidence          float64           `json:"confidence"`
	SuccessRate         float64           `json:"success_rate"`
	GraphOverlap        float64           `json:"graph_overlap"`
	SearchScore         float64           `json:"search_score"`
	IsFrozen            bool              `json:"is_frozen"`
	TimesApplied        int               `json:"times_applied"`
	NegativeTags        []NegativeTag     `json:"negative_tags,omitempty"`
	SuccessByComplexity ComplexityMetrics `json:"success_by_complexity"`

	// Verification fields — always set by Matcher.Match.
	// The planning agent (premium model) must verify the skill before the
	// execution agent uses it. Skills are reference knowledge, not auto-applied.
	RequiresVerification bool   `json:"requires_verification"`
	VerificationPrompt   string `json:"verification_prompt,omitempty"`
}

// SkillExecution logs one application of a skill.
type SkillExecution struct {
	ID           string    `json:"id"`
	SkillID      string    `json:"skill_id"`
	DeveloperID  string    `json:"developer_id"`
	SessionID    string    `json:"session_id"`
	Success      bool      `json:"success"`
	TokensUsed   int       `json:"tokens_used"`
	ErrorDetail  string    `json:"error_detail,omitempty"`
	Complexity   string    `json:"complexity"`
	GraphNodeIDs []string  `json:"graph_node_ids"`
	TaskPrompt   string    `json:"task_prompt"`
	TaskOutput   string    `json:"task_output"`
	ExecutedAt   time.Time `json:"executed_at"`
}

// MatchQuery defines what we're looking for.
type MatchQuery struct {
	TaskText     string
	GraphNodeIDs []string
	Language     string
	Complexity   string
	DeveloperID  string
	Limit        int
}

// MatchResult is the output of a skill search.
type MatchResult struct {
	BestMatch            *SkillSummary  `json:"best_match"`
	Candidates           []SkillSummary `json:"candidates"`
	ExplorationTriggered bool           `json:"exploration_triggered"`
	SearchMethod         string         `json:"search_method"`

	// VerificationRequired is always true when a skill is found.
	// The planning agent must review the skill before the execution agent uses it.
	VerificationRequired bool   `json:"verification_required"`
	VerificationMessage  string `json:"verification_message,omitempty"`
}

// EvolutionRequest is sent to the evolver after a task completes.
type EvolutionRequest struct {
	AppliedSkill  *Skill
	Execution     SkillExecution
	SessionEvents []SessionEventSummary
	GraphContext  string
	CodeContext   map[string]string
}

// SessionEventSummary is a simplified session event (avoids memory package dependency).
type SessionEventSummary struct {
	ToolName    string `json:"tool_name"`
	GraphNodeID string `json:"graph_node_id"`
	Input       string `json:"input"`
	Output      string `json:"output"`
	Success     bool   `json:"success"`
}

// EvolutionResult describes what happened during evolution.
type EvolutionResult struct {
	Action      string `json:"action"`
	NewSkillID  string `json:"new_skill_id"`
	Reason      string `json:"reason"`
	TokensSpent int    `json:"tokens_spent"`
}

// SafetyScanResult describes the outcome of scanning a skill instruction.
type SafetyScanResult struct {
	Safe     bool     `json:"safe"`
	Warnings []string `json:"warnings"`
	Blocked  []string `json:"blocked"`
}

// GraphChangeEvent represents a code change detected by the graph.
type GraphChangeEvent struct {
	ChangedNodeIDs []string `json:"changed_node_ids"`
	ChangeType     string   `json:"change_type"`
	RepoID         string   `json:"repo_id"`
	CommitHash     string   `json:"commit_hash"`
	Timestamp      string   `json:"timestamp"`
}

// SkillStats is returned by Store.GetStats.
type SkillStats struct {
	TotalActive      int            `json:"total_active"`
	TotalFrozen      int            `json:"total_frozen"`
	ByEvolution      map[string]int `json:"by_evolution"`
	ByLanguage       map[string]int `json:"by_language"`
	AvgConfidence    float64        `json:"avg_confidence"`
	AvgSuccessRate   float64        `json:"avg_success_rate"`
	TotalExecutions  int            `json:"total_executions"`
	TotalTokensSaved float64        `json:"total_tokens_saved"`
}

// Config holds all configuration for the skills engine.
type Config struct {
	DatabaseURL     string
	EmbeddingAPIKey string
	EmbeddingModel  string

	OpusAPIKey   string
	OpusModel    string
	OpusEndpoint string

	MinConfidenceForMatch      float64
	MinSuccessRateForMatch     float64
	MinGraphOverlapForMatch    float64
	SimilarityThresholdForSkip float64
	MaxSkillsPerGraphNode      int

	GraphScoreWeight    float64
	KeywordScoreWeight  float64
	SemanticScoreWeight float64

	ConsecutiveFailuresForFix int
	FailureRateForFix         float64

	MaxEvolutionAttemptsPerDay int
	MaxTotalSkills             int
	ExplorationRate            float64

	PruneAfterDays   int
	PruneRunInterval string

	ConfidenceStartValue   float64
	ConfidencePerSuccess   float64
	ConfidenceMaxValue     float64
	ConfidenceMinSuccesses int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		EmbeddingModel:             "text-embedding-ada-002",
		OpusModel:                  "claude-opus-4-20250514",
		OpusEndpoint:               "https://api.anthropic.com/v1/messages",
		MinConfidenceForMatch:      0.3,  // lower — premium model verifies before use
		MinSuccessRateForMatch:     0.40, // lower — let premium model see more options
		MinGraphOverlapForMatch:    0.3,
		SimilarityThresholdForSkip: 0.85,
		MaxSkillsPerGraphNode:      5,
		GraphScoreWeight:           0.40,
		KeywordScoreWeight:         0.25,
		SemanticScoreWeight:        0.35,
		ConsecutiveFailuresForFix:  3,
		FailureRateForFix:          0.40,
		MaxEvolutionAttemptsPerDay: 3,
		MaxTotalSkills:             500,
		ExplorationRate:            0.10,
		PruneAfterDays:             30,
		PruneRunInterval:           "24h",
		ConfidenceStartValue:       0.5,
		ConfidencePerSuccess:       0.1,
		ConfidenceMaxValue:         1.0,
		ConfidenceMinSuccesses:     5,
	}
}
