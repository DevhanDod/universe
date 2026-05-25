package memory

import "time"

// Observation is one atomic unit of memory.
type Observation struct {
	ID          string     `json:"id"`
	DeveloperID string     `json:"developer_id"`
	RepoID      string     `json:"repo_id"`
	GraphNodeID string     `json:"graph_node_id"`
	Category    string     `json:"category"`
	Summary     string     `json:"summary"`
	Detail      string     `json:"detail"`
	Embedding   []float32  `json:"embedding"`
	SessionID   string     `json:"session_id"`
	ToolCalls   []ToolCall `json:"tool_calls"`
	Confidence  float64    `json:"confidence"`
	Shared      bool       `json:"shared"`
	CreatedAt   time.Time  `json:"created_at"`
	RecalledAt  *time.Time `json:"recalled_at"`
}

// ToolCall records one tool invocation during the observation.
type ToolCall struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
}

// ObservationSummary is the compact version returned in layer 1 (index).
type ObservationSummary struct {
	ID          string    `json:"id"`
	GraphNodeID string    `json:"graph_node_id"`
	Category    string    `json:"category"`
	Summary     string    `json:"summary"`
	Confidence  float64   `json:"confidence"`
	CreatedAt   time.Time `json:"created_at"`
	Score       float64   `json:"score"`
}

// SearchQuery defines what we're looking for in memory.
type SearchQuery struct {
	Text                  string
	GraphNodeIDs          []string
	Categories            []string
	RepoIDs               []string
	DeveloperID           string
	Limit                 int
	IncludeGraphNeighbors bool
}

// SearchResult is the output of a search operation.
type SearchResult struct {
	Summaries     []ObservationSummary `json:"summaries"`
	TotalCount    int                  `json:"total_count"`
	SearchedNodes []string             `json:"searched_nodes"`
	SearchMethod  string               `json:"search_method"`
}

// Session tracks an active agent session.
type Session struct {
	ID           string         `json:"id"`
	DeveloperID  string         `json:"developer_id"`
	RepoID       string         `json:"repo_id"`
	StartedAt    time.Time      `json:"started_at"`
	LastActiveAt time.Time      `json:"last_active_at"`
	TouchedNodes []string       `json:"touched_nodes"`
	RawEvents    []SessionEvent `json:"raw_events"`
}

// SessionEvent is a raw event captured during a session.
type SessionEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"event_type"`
	ToolName    string    `json:"tool_name"`
	GraphNodeID string    `json:"graph_node_id"`
	Input       string    `json:"input"`
	Output      string    `json:"output"`
	Success     bool      `json:"success"`
}

// Config holds all configuration for the memory engine.
type Config struct {
	DatabaseURL        string
	EmbeddingModel     string
	EmbeddingAPIKey    string
	EmbeddingEndpoint  string
	EmbeddingDimension int

	CompressorModel    string
	CompressorAPIKey   string
	CompressorEndpoint string

	SessionTimeoutMinutes int

	DecayRate          float64
	DecayMinConfidence float64
	DecayRunInterval   string

	DefaultSearchLimit  int
	MaxSearchLimit      int
	GraphNeighborHops   int
	SemanticScoreWeight float64
	KeywordScoreWeight  float64
	GraphScoreWeight    float64
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		EmbeddingModel:        "text-embedding-ada-002",
		EmbeddingDimension:    1536,
		CompressorModel:       "claude-3-5-haiku-20241022",
		CompressorEndpoint:    "https://api.anthropic.com/v1/messages",
		SessionTimeoutMinutes: 5,
		DecayRate:             0.02,
		DecayMinConfidence:    0.1,
		DecayRunInterval:      "24h",
		DefaultSearchLimit:    10,
		MaxSearchLimit:        50,
		GraphNeighborHops:     1,
		SemanticScoreWeight:   0.4,
		KeywordScoreWeight:    0.3,
		GraphScoreWeight:      0.3,
	}
}

// MemoryStats is returned by Store.GetStats.
type MemoryStats struct {
	TotalObservations int            `json:"total_observations"`
	ByCategory        map[string]int `json:"by_category"`
	ByRepo            map[string]int `json:"by_repo"`
	AverageConfidence float64        `json:"average_confidence"`
	OldestObservation time.Time      `json:"oldest_observation"`
	NewestObservation time.Time      `json:"newest_observation"`
	TotalRecalls      int            `json:"total_recalls"`
	SharedObservations int           `json:"shared_observations"`
}

// DecayResult is returned by DecayRunner.RunDecay.
type DecayResult struct {
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Remaining int `json:"remaining"`
}
