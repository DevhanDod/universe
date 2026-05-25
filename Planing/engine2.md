# Engine 2 — Graph-Aware Persistent Memory

## Build Specification for Claude Code

**Engine name:** Graph-Aware Persistent Memory (Graph-RAG)  
**Concept:** Retrieval-Augmented Generation with graph-indexed episodic memory  
**Estimated effort:** 2-3 days  
**Dependencies:** Engine 1 (Knowledge Graph) — already built. Engine 4 (Compression) — build first or in parallel  
**Database required:** PostgreSQL + pgvector extension (add tables to existing DB)  
**New services required:** None — adds to existing Go binary  

---

## 1. What This Engine Does (Plain English)

When a developer fixes a bug or makes a decision, we save a short note (an "observation") about what they did. We tag that note to the exact function or code entity they touched in the knowledge graph.

Later, when anyone on the team works on that same function — or anything connected to it — the system automatically finds that note and injects it into the AI's prompt. The AI already "knows" what happened before, without the developer having to explain it.

The search works three ways at the same time:
1. **By matching words** — "auth bug" finds notes containing "auth" and "bug"
2. **By matching meaning** — "fix the token type" finds "changed int to string" even though no words match
3. **By following code connections** — if you're working on LoginHandler, we also find notes tagged to ValidateToken because the graph shows LoginHandler calls ValidateToken

We show short summaries first (cheap, ~50 tokens). Only load full details if the AI needs them (expensive, ~500 tokens). This saves ~10x tokens.

Old notes that nobody ever recalls slowly fade away (confidence decay) so the database doesn't get cluttered.

**Expected token savings:** ~60% reduction on repeat patterns (memory hit skips entire investigation cycles).

---

## 2. Current Project Structure

Engine 2 adds a new `memory` package under `internal/` alongside your existing packages.

```
UNIVERSE/
├── cmd/
├── internal/
│   ├── analyzer/
│   │   └── analyzer.go
│   ├── compress/                     ← Engine 4 (build first or in parallel)
│   │   ├── prompt.go
│   │   ├── shorthand.go
│   │   └── formatter.go
│   ├── extractor/
│   │   ├── extractor.go
│   │   ├── go_extractor.go
│   │   └── python_extractor.go
│   ├── graph/
│   │   └── graph.go                  ← Your existing knowledge graph
│   ├── models/
│   │   └── models.go                 ← Existing shared types
│   ├── parser/
│   │   ├── go_parser.go
│   │   ├── parser.go
│   │   ├── python_parser.go
│   │   ├── python_parser_fallback.go
│   │   └── registry.go
│   └── scanner/
│       └── scanner.go
├── go.mod
├── go.sum
└── universe.exe
```

---

## 3. New Files to Create

```
internal/
└── memory/
    ├── types.go          # All types and constants for the memory package
    ├── store.go          # PostgreSQL CRUD — insert, get, update, delete observations
    ├── retriever.go      # 3-way hybrid search: keyword + vector + graph
    ├── compressor.go     # Calls Haiku to compress raw text into short summaries
    ├── hooks.go          # Session lifecycle tracking: start, capture, end
    ├── decay.go          # Confidence decay — fade old unused observations
    └── memory_test.go    # All tests
```

Also create a database migration file:

```
migrations/
└── 002_memory_tables.sql   # SQL to create the observations table + indexes
```

---

## 4. Database Migration: `migrations/002_memory_tables.sql`

Run this SQL against your existing PostgreSQL database. It adds the observations table and all required indexes.

**IMPORTANT:** The pgvector extension must be enabled first. If not already done:

```sql
-- Run once on your database (requires superuser or CREATE EXTENSION privilege)
CREATE EXTENSION IF NOT EXISTS vector;
```

Then run the migration:

```sql
-- ============================================================
-- Engine 2: Graph-Aware Persistent Memory
-- Migration: 002_memory_tables.sql
-- ============================================================

-- The observations table is the core memory store.
-- Each row is one atomic "note" about something the agent did.
--
-- Key design decisions:
--   - graph_node_id links each observation to a specific function/type/package
--     in the knowledge graph. This is what makes it "graph-aware."
--   - embedding stores the meaning-fingerprint (vector) for semantic search.
--   - fts stores the keyword-searchable text for fast word matching.
--   - Both search methods run simultaneously (hybrid search).

CREATE TABLE IF NOT EXISTS observations (
    -- Unique identifier for this observation
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Which developer created this observation
    -- References your existing users table. If you don't have a users table yet,
    -- use TEXT type instead and store a developer identifier string.
    developer_id    TEXT NOT NULL,

    -- Which repository this observation relates to
    repo_id         TEXT NOT NULL,

    -- The knowledge graph node ID this observation is tagged to.
    -- Format: "repo:package:function_name" (matches your graph node IDs)
    -- This is the KEY FIELD that enables graph-aware retrieval.
    -- One observation can be tagged to one node. If it relates to multiple nodes,
    -- create one observation per node (with the same content but different graph_node_id).
    graph_node_id   TEXT NOT NULL,

    -- What kind of observation this is
    -- 'fix'        — a bug fix or code change
    -- 'pattern'    — a coding pattern or approach that worked
    -- 'decision'   — an architectural or design decision
    -- 'failure'    — something that didn't work (useful to avoid repeating)
    -- 'convention' — a team coding convention or rule
    category        TEXT NOT NULL CHECK (category IN ('fix', 'pattern', 'decision', 'failure', 'convention')),

    -- Short AI-compressed summary (~50-100 tokens)
    -- This is what gets returned in the "index" layer (layer 1 of progressive disclosure)
    -- Generated by compressor.go using Haiku
    summary         TEXT NOT NULL,

    -- Full observation detail (~500-1000 tokens)
    -- This is what gets returned in the "detail" layer (layer 3 of progressive disclosure)
    -- Can be NULL if the summary is sufficient
    detail          TEXT,

    -- The meaning-fingerprint for semantic search
    -- 1536 dimensions = OpenAI ada-002 embedding model
    -- If you use a different embedding model, change the dimension:
    --   Anthropic voyage-3: 1024 dimensions
    --   all-MiniLM-L6-v2 (local): 384 dimensions
    embedding       vector(1536),

    -- Which agent session created this observation
    -- Used to group observations by session for timeline queries
    session_id      TEXT,

    -- What tools the agent used during this observation (optional metadata)
    -- Stored as JSON: [{"tool": "read_file", "target": "auth.go"}, ...]
    tool_calls      JSONB,

    -- Confidence score: starts at 1.0, decays over time if never recalled.
    -- When confidence drops below the threshold (default 0.1), the observation
    -- is eligible for cleanup by decay.go
    confidence      FLOAT NOT NULL DEFAULT 1.0,

    -- Is this observation visible to the whole team, or just the developer?
    -- Private by default. Developer can opt-in to share.
    shared          BOOLEAN NOT NULL DEFAULT false,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Last time this observation was recalled (used in a search result).
    -- Updated by retriever.go when this observation is returned in search results.
    -- NULL means never recalled. Used by decay.go — recalled observations
    -- get their confidence boosted back up.
    recalled_at     TIMESTAMPTZ
);

-- ============================================================
-- INDEXES — these make the three search methods fast
-- ============================================================

-- Index 1: Graph node lookup (the most important one)
-- "Find all observations for this function and its connected functions"
CREATE INDEX idx_obs_graph_node ON observations(graph_node_id);

-- Index 2: Developer lookup
-- "Find all my observations" or "Find observations by team member X"
CREATE INDEX idx_obs_developer ON observations(developer_id);

-- Index 3: Category filter
-- "Find all fixes" or "Find all conventions"
CREATE INDEX idx_obs_category ON observations(category);

-- Index 4: Session lookup
-- "Find all observations from this session"
CREATE INDEX idx_obs_session ON observations(session_id);

-- Index 5: Repo filter
-- "Find all observations for this repository"
CREATE INDEX idx_obs_repo ON observations(repo_id);

-- Index 6: Vector similarity search (semantic/meaning search)
-- ivfflat is the index type — it's an approximate nearest neighbor search.
-- lists = 100 is good for up to ~100,000 observations.
-- If you grow beyond that, increase lists to sqrt(total_rows).
CREATE INDEX idx_obs_embedding ON observations
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- Index 7: Full-text search (keyword search)
-- This auto-generates a searchable text column from summary + detail.
-- tsvector breaks the text into searchable tokens.
-- The GIN index makes keyword search fast.
ALTER TABLE observations ADD COLUMN fts tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', summary || ' ' || COALESCE(detail, ''))
    ) STORED;

CREATE INDEX idx_obs_fts ON observations USING gin(fts);

-- Index 8: Confidence + created_at for decay cleanup
-- "Find old observations with low confidence to delete"
CREATE INDEX idx_obs_decay ON observations(confidence, created_at);

-- Index 9: Shared filter for team queries
CREATE INDEX idx_obs_shared ON observations(shared) WHERE shared = true;
```

---

## 5. File: `internal/memory/types.go`

All types, constants, and configuration for the memory package.

```go
package memory

import "time"

// ============================================================
// OBSERVATION — the core data type
// ============================================================

// Observation is one atomic unit of memory.
// It represents a single thing the agent learned during a session:
// a fix it applied, a pattern it discovered, a decision it made.
type Observation struct {
    ID           string    `json:"id"`
    DeveloperID  string    `json:"developer_id"`
    RepoID       string    `json:"repo_id"`
    GraphNodeID  string    `json:"graph_node_id"`
    Category     string    `json:"category"`     // "fix", "pattern", "decision", "failure", "convention"
    Summary      string    `json:"summary"`       // ~50-100 tokens, always present
    Detail       string    `json:"detail"`        // ~500-1000 tokens, may be empty
    Embedding    []float32 `json:"embedding"`     // 1536-dimensional vector
    SessionID    string    `json:"session_id"`
    ToolCalls    []ToolCall `json:"tool_calls"`
    Confidence   float64   `json:"confidence"`
    Shared       bool      `json:"shared"`
    CreatedAt    time.Time `json:"created_at"`
    RecalledAt   *time.Time `json:"recalled_at"`   // nil if never recalled
}

// ToolCall records one tool invocation during the observation.
type ToolCall struct {
    Tool   string `json:"tool"`    // e.g., "read_file", "write_file", "run_tests"
    Target string `json:"target"`  // e.g., "auth/validate.go"
}

// ObservationSummary is the compact version returned in layer 1 (index).
// Contains only enough info for the agent to decide if it wants the full detail.
// ~50-100 tokens per summary.
type ObservationSummary struct {
    ID          string    `json:"id"`
    GraphNodeID string    `json:"graph_node_id"`
    Category    string    `json:"category"`
    Summary     string    `json:"summary"`
    Confidence  float64   `json:"confidence"`
    CreatedAt   time.Time `json:"created_at"`
    Score       float64   `json:"score"`  // relevance score from search (0.0 to 1.0)
}

// ============================================================
// SEARCH — input and output types for retrieval
// ============================================================

// SearchQuery defines what we're looking for in memory.
type SearchQuery struct {
    // Text query for keyword and semantic search.
    // Can be empty if searching by graph nodes only.
    Text string

    // Graph node IDs to search by.
    // If provided, also searches observations for these nodes AND their
    // graph neighbors (callers + callees, 1 hop).
    GraphNodeIDs []string

    // Filter by category. Empty means all categories.
    Categories []string

    // Filter by repository. Empty means all repos.
    RepoIDs []string

    // Filter by developer. Empty means all developers.
    // When shared=false observations exist, only the requesting developer's
    // private observations + all shared observations are returned.
    DeveloperID string

    // Maximum number of results to return.
    // Default: 10. Max: 50.
    Limit int

    // If true, also search observations tagged to graph neighbors
    // (functions that call or are called by the specified nodes).
    // Default: true. This is what makes it "graph-aware."
    IncludeGraphNeighbors bool
}

// SearchResult is the output of a search operation.
type SearchResult struct {
    // Compact summaries — layer 1 of progressive disclosure
    Summaries []ObservationSummary `json:"summaries"`

    // Total number of matching observations (may be more than Limit)
    TotalCount int `json:"total_count"`

    // Which graph nodes were searched (including neighbors if expanded)
    SearchedNodes []string `json:"searched_nodes"`

    // How the results were found — for debugging and cost tracking
    SearchMethod string `json:"search_method"` // "graph_only", "keyword_only", "semantic_only", "hybrid"
}

// ============================================================
// SESSION TRACKING — for lifecycle hooks
// ============================================================

// Session tracks an active agent session.
// Created when the first MCP tool call arrives, ended by inactivity timeout.
type Session struct {
    ID          string    `json:"id"`
    DeveloperID string    `json:"developer_id"`
    StartedAt   time.Time `json:"started_at"`
    LastActiveAt time.Time `json:"last_active_at"`

    // Graph nodes touched during this session (accumulated as tools are called)
    TouchedNodes []string `json:"touched_nodes"`

    // Raw events captured during the session (compressed at session end)
    RawEvents []SessionEvent `json:"raw_events"`
}

// SessionEvent is a raw event captured during a session.
// These get compressed into Observations at session end.
type SessionEvent struct {
    Timestamp   time.Time `json:"timestamp"`
    EventType   string    `json:"event_type"`   // "tool_use", "task_start", "task_complete", "error"
    ToolName    string    `json:"tool_name"`     // which MCP tool was called
    GraphNodeID string    `json:"graph_node_id"` // which graph node was involved (if any)
    Input       string    `json:"input"`         // what was passed to the tool (truncated)
    Output      string    `json:"output"`        // what the tool returned (truncated)
    Success     bool      `json:"success"`
}

// ============================================================
// CONFIGURATION
// ============================================================

// Config holds all configuration for the memory engine.
type Config struct {
    // Database connection string for PostgreSQL
    DatabaseURL string

    // Embedding model configuration
    EmbeddingModel    string // "openai-ada-002" or "voyage-3" or "local-minilm"
    EmbeddingAPIKey   string // API key for the embedding service
    EmbeddingEndpoint string // API endpoint (default: OpenAI)
    EmbeddingDimension int   // 1536 for ada-002, 1024 for voyage-3, 384 for minilm

    // LLM configuration for the compressor (uses cheap model)
    CompressorModel   string // "claude-haiku" or "claude-3-5-haiku-20241022"
    CompressorAPIKey  string // Anthropic API key
    CompressorEndpoint string // default: "https://api.anthropic.com/v1/messages"

    // Session tracking
    SessionTimeoutMinutes int // Inactivity timeout before session ends. Default: 5

    // Decay configuration
    DecayRate         float64 // How much confidence drops per day. Default: 0.02 (2% per day)
    DecayMinConfidence float64 // Below this, observation is deleted. Default: 0.1
    DecayRunInterval  string  // How often to run decay. Default: "24h"

    // Search configuration
    DefaultSearchLimit    int     // Default number of results. Default: 10
    MaxSearchLimit        int     // Maximum allowed. Default: 50
    GraphNeighborHops     int     // How many graph hops to follow. Default: 1
    SemanticScoreWeight   float64 // Weight for semantic results in hybrid. Default: 0.4
    KeywordScoreWeight    float64 // Weight for keyword results in hybrid. Default: 0.3
    GraphScoreWeight      float64 // Weight for graph-matched results in hybrid. Default: 0.3
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
    return Config{
        EmbeddingModel:     "openai-ada-002",
        EmbeddingDimension: 1536,
        CompressorModel:    "claude-3-5-haiku-20241022",
        CompressorEndpoint: "https://api.anthropic.com/v1/messages",
        SessionTimeoutMinutes: 5,
        DecayRate:           0.02,
        DecayMinConfidence:  0.1,
        DecayRunInterval:    "24h",
        DefaultSearchLimit:  10,
        MaxSearchLimit:      50,
        GraphNeighborHops:   1,
        SemanticScoreWeight: 0.4,
        KeywordScoreWeight:  0.3,
        GraphScoreWeight:    0.3,
    }
}
```

---

## 6. File: `internal/memory/store.go`

PostgreSQL CRUD operations. This is the data access layer — it only talks to the database.

```go
package memory

// Store handles all database operations for observations.
// It uses a *sql.DB connection pool from the Go standard library
// or a library like pgx (recommended for PostgreSQL).
//
// External dependency needed: github.com/jackc/pgx/v5
// Add to go.mod: go get github.com/jackc/pgx/v5

// NewStore creates a new Store connected to PostgreSQL.
// Parameters:
//   - databaseURL: PostgreSQL connection string
//     e.g., "postgres://user:pass@localhost:5432/universe?sslmode=disable"
//
// Returns: *Store, error
//
// Implementation:
//   1. Connect to PostgreSQL using pgx.Connect or pgxpool.New (pool recommended)
//   2. Ping the database to verify connection
//   3. Return the Store with the connection pool
func NewStore(databaseURL string) (*Store, error)

// Close closes the database connection pool.
func (s *Store) Close() error

// ============================================================
// INSERT operations
// ============================================================

// InsertObservation stores a new observation in the database.
//
// Parameters:
//   - obs: the Observation to store. ID will be generated by the database
//     if obs.ID is empty. Embedding must already be computed.
//
// Returns: the stored Observation (with generated ID and timestamps), error
//
// SQL:
//   INSERT INTO observations
//     (developer_id, repo_id, graph_node_id, category, summary, detail,
//      embedding, session_id, tool_calls, confidence, shared)
//   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
//   RETURNING id, created_at
//
// Notes:
//   - embedding is stored as vector type: pass as pgvector.Vector or []float32
//   - tool_calls is stored as JSONB: marshal to JSON before inserting
func (s *Store) InsertObservation(obs Observation) (*Observation, error)

// InsertBatch stores multiple observations in one database round-trip.
// Use this at session end when compressing a full session into multiple observations.
//
// Parameters:
//   - observations: list of Observations to store
//
// Returns: list of stored Observations (with IDs), error
//
// Implementation: use pgx.Batch or a single INSERT with multiple VALUE rows
func (s *Store) InsertBatch(observations []Observation) ([]Observation, error)

// ============================================================
// READ operations — used by retriever.go
// ============================================================

// GetByID retrieves a single observation by its UUID.
// Used in layer 3 of progressive disclosure (get full detail for specific IDs).
//
// Parameters:
//   - id: the observation UUID
//
// Returns: *Observation, error (nil Observation if not found)
func (s *Store) GetByID(id string) (*Observation, error)

// GetByIDs retrieves multiple observations by their UUIDs.
// Used when the agent requests full details for several observations at once.
//
// Parameters:
//   - ids: list of observation UUIDs
//
// Returns: []Observation (in same order as input IDs), error
//
// SQL:
//   SELECT * FROM observations WHERE id = ANY($1)
func (s *Store) GetByIDs(ids []string) ([]Observation, error)

// GetByGraphNode retrieves all observations tagged to a specific graph node.
// This is the primary graph-aware retrieval method.
//
// Parameters:
//   - graphNodeID: the graph node ID (e.g., "auth-service:auth:ValidateToken")
//   - developerID: the requesting developer (for private/shared filtering)
//   - limit: max results
//
// Returns: []ObservationSummary (compact, for layer 1), error
//
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at
//   FROM observations
//   WHERE graph_node_id = $1
//     AND (shared = true OR developer_id = $2)
//     AND confidence > 0.1
//   ORDER BY confidence DESC, created_at DESC
//   LIMIT $3
func (s *Store) GetByGraphNode(graphNodeID string, developerID string, limit int) ([]ObservationSummary, error)

// GetByGraphNodes retrieves observations for multiple graph nodes at once.
// Used when the retriever expands to include graph neighbors.
//
// Parameters:
//   - graphNodeIDs: list of graph node IDs (target node + its callers + callees)
//   - developerID: for private/shared filtering
//   - limit: max total results across all nodes
//
// Returns: []ObservationSummary, error
//
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at
//   FROM observations
//   WHERE graph_node_id = ANY($1)
//     AND (shared = true OR developer_id = $2)
//     AND confidence > 0.1
//   ORDER BY confidence DESC, created_at DESC
//   LIMIT $3
func (s *Store) GetByGraphNodes(graphNodeIDs []string, developerID string, limit int) ([]ObservationSummary, error)

// SearchKeyword performs full-text keyword search using PostgreSQL FTS.
//
// Parameters:
//   - query: the search text (e.g., "auth bug validate token")
//   - developerID: for private/shared filtering
//   - limit: max results
//
// Returns: []ObservationSummary with Score field populated (FTS rank), error
//
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at,
//          ts_rank(fts, plainto_tsquery('english', $1)) AS score
//   FROM observations
//   WHERE fts @@ plainto_tsquery('english', $1)
//     AND (shared = true OR developer_id = $2)
//     AND confidence > 0.1
//   ORDER BY score DESC
//   LIMIT $3
func (s *Store) SearchKeyword(query string, developerID string, limit int) ([]ObservationSummary, error)

// SearchSemantic performs vector similarity search using pgvector.
//
// Parameters:
//   - queryEmbedding: the meaning-fingerprint of the search query ([]float32)
//   - developerID: for private/shared filtering
//   - limit: max results
//
// Returns: []ObservationSummary with Score field populated (cosine similarity), error
//
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at,
//          1 - (embedding <=> $1) AS score
//   FROM observations
//   WHERE (shared = true OR developer_id = $2)
//     AND confidence > 0.1
//   ORDER BY embedding <=> $1
//   LIMIT $3
//
// Notes:
//   - <=> is the pgvector cosine distance operator
//   - 1 - distance = similarity (higher is better)
//   - $1 is passed as a pgvector.Vector type
func (s *Store) SearchSemantic(queryEmbedding []float32, developerID string, limit int) ([]ObservationSummary, error)

// ============================================================
// UPDATE operations
// ============================================================

// TouchRecalled updates the recalled_at timestamp and boosts confidence.
// Called by retriever.go when an observation is returned in search results.
// This keeps useful observations alive (prevents decay from deleting them).
//
// Parameters:
//   - id: the observation UUID
//
// SQL:
//   UPDATE observations
//   SET recalled_at = NOW(),
//       confidence = LEAST(1.0, confidence + 0.1)
//   WHERE id = $1
func (s *Store) TouchRecalled(id string) error

// UpdateConfidenceBatch updates confidence scores for multiple observations.
// Used by decay.go during the nightly decay run.
//
// Parameters:
//   - updates: map of observation ID → new confidence score
//
// Implementation: use a batch UPDATE or a CTE
func (s *Store) UpdateConfidenceBatch(updates map[string]float64) error

// ============================================================
// DELETE operations
// ============================================================

// DeleteByConfidence deletes observations below the minimum confidence threshold.
// Called by decay.go after updating confidence scores.
//
// Parameters:
//   - minConfidence: observations below this score get deleted
//
// Returns: number of observations deleted, error
//
// SQL:
//   DELETE FROM observations WHERE confidence < $1
//   RETURNING id
func (s *Store) DeleteByConfidence(minConfidence float64) (int, error)

// DeleteByID deletes a specific observation (for manual cleanup).
func (s *Store) DeleteByID(id string) error

// ============================================================
// STATS operations — for dashboards and monitoring
// ============================================================

// GetStats returns summary statistics about the memory store.
type MemoryStats struct {
    TotalObservations   int     `json:"total_observations"`
    ByCategory          map[string]int `json:"by_category"`
    ByRepo              map[string]int `json:"by_repo"`
    AverageConfidence   float64 `json:"average_confidence"`
    OldestObservation   time.Time `json:"oldest_observation"`
    NewestObservation   time.Time `json:"newest_observation"`
    TotalRecalls        int     `json:"total_recalls"` // observations that have been recalled at least once
    SharedObservations  int     `json:"shared_observations"`
}

func (s *Store) GetStats() (*MemoryStats, error)
```

---

## 7. File: `internal/memory/compressor.go`

Compresses raw session events into short observation summaries using a cheap LLM (Haiku).

```go
package memory

// Compressor uses a cheap LLM to convert raw session events
// into concise observation summaries.
//
// Why compress?
// A full session might be 5,000 tokens of tool calls and responses.
// We compress that into a 50-100 token summary for storage.
// This saves storage space and makes retrieval faster (less to read).
//
// External dependency: net/http (standard library) for Anthropic API calls

// NewCompressor creates a new Compressor.
// Parameters:
//   - config: Config with CompressorModel, CompressorAPIKey, CompressorEndpoint
func NewCompressor(config Config) *Compressor

// CompressEvents takes raw session events and produces Observations.
// This is called at session end to convert the session into stored memory.
//
// Parameters:
//   - events: raw session events captured during the session
//   - developerID: who ran the session
//   - repoID: which repo was being worked on
//   - sessionID: the session identifier
//
// Returns: []Observation (without embeddings — caller must compute those)
//
// Implementation:
//   1. Group events by graph_node_id (all events touching the same function
//      get compressed into one observation per function)
//   2. For each group:
//      a. Build a compression prompt (see COMPRESSION_PROMPT below)
//      b. Call Haiku API with the prompt
//      c. Parse the response into an Observation struct
//      d. Set category based on the events (fix, pattern, decision, failure, convention)
//   3. Return the list of Observations
//
// The compression prompt for each group:
//
//   COMPRESSION_PROMPT (sent to Haiku):
//   """
//   Compress these agent session events into a concise technical observation.
//
//   RULES:
//   - Output ONE JSON object, nothing else
//   - "summary": 1-2 sentences, max 100 tokens. Keep function names, error types,
//     and the fix approach. Drop everything else.
//   - "detail": 3-5 sentences, max 500 tokens. Include the full context:
//     what was wrong, what was tried, what worked, what code changed.
//   - "category": one of "fix", "pattern", "decision", "failure", "convention"
//
//   EVENTS:
//   [paste the grouped events here as JSON]
//
//   Respond with ONLY the JSON:
//   {"summary": "...", "detail": "...", "category": "..."}
//   """
//
// Error handling:
//   - If Haiku returns invalid JSON, retry once with a stricter prompt
//   - If the retry fails, create a basic observation with the first event's
//     info as summary and skip the detail
//   - Never throw away events — always produce at least a minimal observation
func (c *Compressor) CompressEvents(events []SessionEvent, developerID, repoID, sessionID string) ([]Observation, error)

// CompressSingleEvent compresses a single event into an observation.
// Used for high-importance events that should be stored immediately
// (not waiting for session end). E.g., a critical error or a major fix.
//
// Parameters:
//   - event: the single event to compress
//   - developerID, repoID, sessionID: context
//
// Returns: *Observation (without embedding), error
func (c *Compressor) CompressSingleEvent(event SessionEvent, developerID, repoID, sessionID string) (*Observation, error)

// callHaiku makes an HTTP POST to the Anthropic API.
// This is a private helper function.
//
// Parameters:
//   - systemPrompt: the system prompt
//   - userMessage: the user message (the events to compress)
//   - maxTokens: max output tokens (default: 300 for compression)
//
// Returns: the response text, error
//
// Implementation:
//   1. Build the request body:
//      {
//        "model": config.CompressorModel,
//        "max_tokens": maxTokens,
//        "system": systemPrompt,
//        "messages": [{"role": "user", "content": userMessage}]
//      }
//   2. POST to config.CompressorEndpoint
//   3. Set headers:
//      "Content-Type": "application/json"
//      "x-api-key": config.CompressorAPIKey
//      "anthropic-version": "2023-06-01"
//   4. Parse response: extract content[0].text
//   5. Return the text
func (c *Compressor) callHaiku(systemPrompt, userMessage string, maxTokens int) (string, error)
```

---

## 8. File: `internal/memory/retriever.go`

The 3-way hybrid search engine. This is the most important file in Engine 2.

```go
package memory

// Retriever performs 3-way hybrid search: keyword + semantic + graph.
// It combines results from all three methods and ranks them by a weighted score.
//
// The search flow:
//   1. If GraphNodeIDs provided → query graph for neighbor nodes (callers + callees)
//   2. Run three searches in parallel:
//      a. Graph node match: GetByGraphNodes (exact node ID match)
//      b. Keyword search: SearchKeyword (PostgreSQL FTS)
//      c. Semantic search: SearchSemantic (pgvector cosine similarity)
//   3. Merge results: deduplicate by observation ID, combine scores
//   4. Rank by weighted score: graph × 0.3 + keyword × 0.3 + semantic × 0.4
//   5. Return top N as ObservationSummary list
//   6. Update recalled_at for all returned observations

// NewRetriever creates a new Retriever.
// Parameters:
//   - store: the Store for database access
//   - embedder: function to convert text → embedding vector
//   - graphQuerier: interface to query your existing knowledge graph
//   - config: Config with search weights and limits
func NewRetriever(store *Store, embedder EmbedFunc, graphQuerier GraphQuerier, config Config) *Retriever

// EmbedFunc converts text into a vector embedding.
// This is a function type so you can swap embedding providers easily.
//
// Parameters:
//   - text: the text to embed
//
// Returns: []float32 (the embedding vector), error
//
// Implementation options:
//   a. OpenAI ada-002: POST to https://api.openai.com/v1/embeddings
//   b. Anthropic voyage: POST to Anthropic embedding endpoint
//   c. Local model: call a local embedding server
//
// For the spec, implement option (a) as default.
// The function signature makes it easy to swap later.
type EmbedFunc func(text string) ([]float32, error)

// GraphQuerier is an interface to your existing knowledge graph.
// It lets the retriever look up graph neighbors without depending
// directly on your graph package's concrete types.
//
// You will implement this interface as an adapter in your integration code.
// It wraps your existing graph.Graph methods.
type GraphQuerier interface {
    // GetCallerIDs returns the graph node IDs of all functions that call
    // the given node. One hop only.
    GetCallerIDs(nodeID string) ([]string, error)

    // GetCalleeIDs returns the graph node IDs of all functions that
    // the given node calls. One hop only.
    GetCalleeIDs(nodeID string) ([]string, error)
}

// Search performs the full 3-way hybrid search.
//
// This is the main entry point for memory retrieval.
//
// Parameters:
//   - query: SearchQuery with text, graph nodes, filters, and limits
//
// Returns: SearchResult with ranked summaries
//
// Implementation (step by step):
//
//   STEP 1: Expand graph nodes
//   If query.GraphNodeIDs is not empty and query.IncludeGraphNeighbors is true:
//     For each node in query.GraphNodeIDs:
//       callers := graphQuerier.GetCallerIDs(node)
//       callees := graphQuerier.GetCalleeIDs(node)
//       Add callers and callees to an expandedNodes list
//     Deduplicate expandedNodes
//
//   STEP 2: Run three searches (in parallel using goroutines)
//
//     Search A — Graph node match:
//       If expandedNodes is not empty:
//         graphResults := store.GetByGraphNodes(expandedNodes, query.DeveloperID, query.Limit * 2)
//         Assign score = 1.0 for exact node match, 0.7 for neighbor match
//
//     Search B — Keyword search:
//       If query.Text is not empty:
//         keywordResults := store.SearchKeyword(query.Text, query.DeveloperID, query.Limit * 2)
//         Score comes from PostgreSQL ts_rank (already in result)
//
//     Search C — Semantic search:
//       If query.Text is not empty:
//         queryEmbedding := embedder(query.Text)
//         semanticResults := store.SearchSemantic(queryEmbedding, query.DeveloperID, query.Limit * 2)
//         Score comes from cosine similarity (already in result)
//
//   STEP 3: Merge and deduplicate
//     Create a map[string]*scoredObservation keyed by observation ID
//     For each result from all three searches:
//       If observation ID already in map:
//         Add to its score (don't replace — accumulate)
//       Else:
//         Create new entry with the source-specific score
//
//   STEP 4: Calculate final weighted score
//     For each observation in the map:
//       finalScore = (graphScore * config.GraphScoreWeight) +
//                    (keywordScore * config.KeywordScoreWeight) +
//                    (semanticScore * config.SemanticScoreWeight)
//       Normalize so max possible = 1.0
//
//   STEP 5: Sort by finalScore descending, take top query.Limit results
//
//   STEP 6: Update recalled_at for all returned observations
//     For each returned observation:
//       go store.TouchRecalled(obs.ID)  // async — don't slow down the search
//
//   STEP 7: Return SearchResult
func (r *Retriever) Search(query SearchQuery) (*SearchResult, error)

// GetFullObservations retrieves full observation details for specific IDs.
// This is layer 3 of progressive disclosure — called when the agent
// decides it needs the full detail for specific observations.
//
// Parameters:
//   - ids: observation UUIDs to retrieve
//
// Returns: []Observation (full detail), error
func (r *Retriever) GetFullObservations(ids []string) ([]Observation, error) {
    // Just delegates to store.GetByIDs(ids)
}

// GetSessionContext retrieves all relevant observations for a set of graph nodes.
// This is the "auto-inject at session start" function.
// Called when a new session begins with known graph context (from the developer's
// current diff or open file).
//
// Parameters:
//   - graphNodeIDs: the graph nodes from the developer's current context
//   - developerID: the requesting developer
//
// Returns: SearchResult with the most relevant observations
//
// Implementation:
//   Just calls Search with a SearchQuery that has:
//     GraphNodeIDs: graphNodeIDs
//     IncludeGraphNeighbors: true
//     DeveloperID: developerID
//     Limit: 10
//     Text: "" (no text query — pure graph-based retrieval)
func (r *Retriever) GetSessionContext(graphNodeIDs []string, developerID string) (*SearchResult, error)
```

---

## 9. File: `internal/memory/hooks.go`

Session lifecycle tracking. Captures events during agent sessions and triggers compression + storage at session end.

```go
package memory

// SessionManager tracks active agent sessions and captures events.
// It detects session start/end via MCP tool call activity.
//
// How it works:
//   - First MCP tool call from a developer → create new Session
//   - Each subsequent tool call → add event to session, update LastActiveAt
//   - No tool calls for SessionTimeoutMinutes → end session, compress, store
//
// The timeout check runs on a ticker (every 60 seconds) in a background goroutine.

// NewSessionManager creates a new SessionManager.
// Parameters:
//   - store: for storing observations
//   - compressor: for compressing events into observations
//   - embedder: for computing embeddings on compressed observations
//   - config: for timeout settings
//
// Implementation:
//   1. Create the manager
//   2. Start a background goroutine with a time.Ticker (60 second interval)
//   3. On each tick, check all active sessions for timeout
//   4. End any timed-out sessions
func NewSessionManager(store *Store, compressor *Compressor, embedder EmbedFunc, config Config) *SessionManager

// OnToolCall is called every time an MCP tool is invoked.
// This is the primary hook point — your MCP server calls this on every request.
//
// Parameters:
//   - developerID: who made the call
//   - toolName: which MCP tool was called (e.g., "get_dependencies", "run_tests")
//   - graphNodeID: which graph node is involved (empty if none)
//   - input: tool input (truncated to 500 chars)
//   - output: tool output (truncated to 500 chars)
//   - success: whether the tool call succeeded
//   - repoID: which repo the developer is working in
//
// Implementation:
//   1. Look up active session for this developer (map[developerID]*Session)
//   2. If no active session:
//      a. Create new Session with a generated UUID
//      b. Store in the active sessions map
//   3. Add a SessionEvent to the session's RawEvents list
//   4. Add graphNodeID to session's TouchedNodes (if not empty and not already present)
//   5. Update session's LastActiveAt to time.Now()
//
// Thread safety: use sync.RWMutex on the sessions map
func (sm *SessionManager) OnToolCall(developerID, toolName, graphNodeID, input, output string, success bool, repoID string)

// EndSession explicitly ends a session (e.g., developer disconnects).
// Also called by the timeout checker.
//
// Parameters:
//   - developerID: whose session to end
//
// Implementation:
//   1. Lock the sessions map
//   2. Get the session for this developer
//   3. Remove it from the active sessions map
//   4. Unlock
//   5. Call processSessionEnd(session) in a goroutine
func (sm *SessionManager) EndSession(developerID string)

// processSessionEnd compresses and stores the session's events.
// This is the key function — it converts raw events into stored observations.
//
// Parameters:
//   - session: the ended session
//
// Implementation:
//   1. If session has fewer than 2 events, skip (too short to be useful)
//   2. Call compressor.CompressEvents(session.RawEvents, ...)
//      This returns []Observation (without embeddings)
//   3. For each observation:
//      a. Compute embedding: embedding := embedder(obs.Summary + " " + obs.Detail)
//      b. Set obs.Embedding = embedding
//   4. Call store.InsertBatch(observations) to save them all
//   5. Log: "Session [ID] ended: [N] observations stored for developer [ID]"
//
// Error handling:
//   - If compression fails, log the error but don't crash
//   - If embedding fails for one observation, store it without embedding
//     (it'll still be found by keyword and graph search, just not semantic)
//   - If database insert fails, log the error — events are lost but session continues
func (sm *SessionManager) processSessionEnd(session *Session)

// checkTimeouts is the background goroutine that ends timed-out sessions.
// Runs every 60 seconds.
//
// Implementation:
//   1. Lock the sessions map (read lock)
//   2. For each active session:
//      If time.Since(session.LastActiveAt) > config.SessionTimeoutMinutes * time.Minute:
//        Add to timedOut list
//   3. Unlock
//   4. For each timed-out session:
//      Call EndSession(session.DeveloperID)
func (sm *SessionManager) checkTimeouts()

// Stop gracefully shuts down the session manager.
// Ends all active sessions (so their events get compressed and stored)
// and stops the background timeout checker.
func (sm *SessionManager) Stop()
```

---

## 10. File: `internal/memory/decay.go`

Confidence decay — fades old unused observations so the database doesn't grow forever.

```go
package memory

// DecayRunner periodically reduces confidence on old observations
// and deletes those that fall below the minimum threshold.
//
// The decay formula:
//   new_confidence = current_confidence - (days_since_last_recall * decay_rate)
//
// An observation recalled yesterday loses almost nothing.
// An observation not recalled for 30 days at 2% per day loses 60% confidence.
// An observation not recalled for 50 days drops below 0.1 and gets deleted.
//
// Observations that are frequently recalled get their confidence boosted
// back to 1.0 by store.TouchRecalled() — so they never decay.

// NewDecayRunner creates a new DecayRunner.
// Parameters:
//   - store: for reading/updating/deleting observations
//   - config: decay rate and thresholds
func NewDecayRunner(store *Store, config Config) *DecayRunner

// RunDecay performs one decay cycle.
// Call this on a schedule (e.g., daily via a cron job or background goroutine).
//
// Implementation:
//   1. Query all observations with confidence > config.DecayMinConfidence
//      and recalled_at < NOW() - interval '1 day' (or recalled_at IS NULL)
//
//      SQL:
//        SELECT id, confidence, recalled_at, created_at
//        FROM observations
//        WHERE confidence > $1
//          AND (recalled_at IS NULL OR recalled_at < NOW() - interval '1 day')
//
//   2. For each observation:
//      daysUnused := days since recalled_at (or created_at if never recalled)
//      newConfidence := obs.Confidence - (daysUnused * config.DecayRate)
//      if newConfidence < config.DecayMinConfidence:
//        newConfidence = 0  // mark for deletion
//      Add to updates map: id → newConfidence
//
//   3. Call store.UpdateConfidenceBatch(updates)
//
//   4. Call store.DeleteByConfidence(config.DecayMinConfidence)
//
//   5. Log: "Decay cycle complete: [N] updated, [M] deleted, [T] total remaining"
//
// Returns: DecayResult, error
type DecayResult struct {
    Updated  int `json:"updated"`
    Deleted  int `json:"deleted"`
    Remaining int `json:"remaining"`
}

func (d *DecayRunner) RunDecay() (*DecayResult, error)

// StartSchedule starts the decay runner on a background schedule.
// Parameters:
//   - interval: how often to run (e.g., 24 * time.Hour)
//
// Implementation:
//   1. Start a goroutine with time.Ticker(interval)
//   2. On each tick, call RunDecay()
//   3. Log results
func (d *DecayRunner) StartSchedule(interval time.Duration)

// Stop stops the background schedule.
func (d *DecayRunner) Stop()
```

---

## 11. File: `internal/memory/embed.go`

Embedding function — converts text into meaning-fingerprints (vectors).

```go
package memory

// NewOpenAIEmbedder creates an EmbedFunc that calls OpenAI's embedding API.
//
// Parameters:
//   - apiKey: OpenAI API key
//   - model: embedding model name (default: "text-embedding-ada-002")
//
// Returns: EmbedFunc
//
// The returned function:
//   1. POST to https://api.openai.com/v1/embeddings
//   2. Body: {"input": text, "model": model}
//   3. Headers: Authorization: Bearer apiKey
//   4. Parse response: data[0].embedding → []float32
//
// Error handling:
//   - Retry once on HTTP 429 (rate limit) with 1 second backoff
//   - Return error on HTTP 4xx/5xx after retry
//   - Return error if response doesn't contain an embedding
func NewOpenAIEmbedder(apiKey string, model string) EmbedFunc

// NewBatchEmbedder creates a function that embeds multiple texts in one API call.
// Use this when storing multiple observations at session end.
//
// Parameters:
//   - apiKey: OpenAI API key
//   - model: embedding model name
//
// Returns: function that takes []string and returns [][]float32
//
// The API supports up to 2048 texts per batch call.
// Each text is max 8191 tokens for ada-002.
func NewBatchEmbedder(apiKey string, model string) func(texts []string) ([][]float32, error)
```

---

## 12. MCP Tools to Expose

When you build the MCP server, add these three tools. They are the interface between the AI agent and the memory engine.

### Tool 1: `recall_memory`

```json
{
  "name": "recall_memory",
  "description": "Search past observations from the memory store. Returns compact summaries first (layer 1). Use get_observation_details to load full details for specific IDs.",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Text to search for (keyword + semantic search). Optional if graph_node_ids provided."
      },
      "graph_node_ids": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Graph node IDs to search by. Also searches their callers and callees."
      },
      "categories": {
        "type": "array",
        "items": {"type": "string", "enum": ["fix", "pattern", "decision", "failure", "convention"]},
        "description": "Filter by observation category. Empty means all."
      },
      "limit": {
        "type": "integer",
        "description": "Max results. Default: 10, Max: 50"
      }
    }
  }
}
```

### Tool 2: `get_observation_details`

```json
{
  "name": "get_observation_details",
  "description": "Get full details for specific observations by their IDs. Layer 3 of progressive disclosure. Use recall_memory first to find relevant IDs.",
  "input_schema": {
    "type": "object",
    "properties": {
      "ids": {
        "type": "array",
        "items": {"type": "string"},
        "description": "List of observation UUIDs to retrieve full details for."
      }
    },
    "required": ["ids"]
  }
}
```

### Tool 3: `store_observation`

```json
{
  "name": "store_observation",
  "description": "Manually store an observation. Use this to save important patterns, decisions, or conventions that should be remembered across sessions.",
  "input_schema": {
    "type": "object",
    "properties": {
      "graph_node_id": {
        "type": "string",
        "description": "The graph node this observation relates to."
      },
      "category": {
        "type": "string",
        "enum": ["fix", "pattern", "decision", "failure", "convention"],
        "description": "What kind of observation this is."
      },
      "content": {
        "type": "string",
        "description": "The observation text. Will be AI-compressed into a summary."
      },
      "shared": {
        "type": "boolean",
        "description": "Make this visible to the whole team. Default: false"
      }
    },
    "required": ["graph_node_id", "category", "content"]
  }
}
```

### Auto-injection at session start

When a new MCP session begins (first tool call detected by hooks.go), automatically call:

```go
// In your MCP server's session initialization:
result := retriever.GetSessionContext(currentDiffGraphNodes, developerID)
// Convert result.Summaries to text and inject into the agent's system prompt
// using Engine 4's compress.BuildPrompt with the observations as context
```

This happens without the developer asking — the agent automatically "remembers" relevant context.

---

## 13. Integration with Existing Code

### 13.1 Integration with graph package

Create an adapter that implements the `GraphQuerier` interface using your existing graph:

```go
// This file goes in internal/memory/ or in your integration/glue code
// Adapt the function signatures to match your actual graph.go types

type GraphAdapter struct {
    graph *graph.Graph  // your existing graph type from internal/graph/graph.go
}

func (ga *GraphAdapter) GetCallerIDs(nodeID string) ([]string, error) {
    // Use your existing graph methods to find incoming edges to nodeID
    // Return the source node IDs of those edges
}

func (ga *GraphAdapter) GetCalleeIDs(nodeID string) ([]string, error) {
    // Use your existing graph methods to find outgoing edges from nodeID
    // Return the target node IDs of those edges
}
```

### 13.2 Integration with Engine 4 (compress package)

When injecting memory into prompts, use Engine 4's compression:

```go
import "universe/internal/compress"

// When building the agent prompt with memory context:
memoryText := formatObservationsAsText(searchResult.Summaries)
prompt := compress.BuildPrompt(taskPrompt, compress.PromptConfig{
    Level:        compress.LevelCompact,
    GraphContext: graphNodeInfos,
    // Memory observations get appended to the prompt between
    // graph context and the task description
})
```

### 13.3 Go module dependencies to add

```bash
# PostgreSQL driver with pgvector support
go get github.com/jackc/pgx/v5
go get github.com/pgvector/pgvector-go

# UUID generation (for session IDs)
go get github.com/google/uuid
```

---

## 14. Testing Strategy

Create `internal/memory/memory_test.go`:

```go
package memory

import "testing"

// ============================================================
// STORE TESTS — require a test PostgreSQL database
// ============================================================

// Test 1: InsertObservation and GetByID round-trip
func TestStore_InsertAndGet(t *testing.T) {
    // Create observation, insert it, get it by ID
    // Verify all fields match
}

// Test 2: GetByGraphNode returns matching observations
func TestStore_GetByGraphNode(t *testing.T) {
    // Insert 3 observations for node "auth:validate"
    // Insert 2 observations for node "gateway:login"
    // GetByGraphNode("auth:validate") should return 3
}

// Test 3: GetByGraphNodes returns observations for multiple nodes
func TestStore_GetByGraphNodes(t *testing.T) {
    // Insert observations for multiple nodes
    // GetByGraphNodes with both node IDs should return all
}

// Test 4: SearchKeyword finds by words
func TestStore_SearchKeyword(t *testing.T) {
    // Insert observation with summary "Fixed type mismatch in ValidateToken"
    // SearchKeyword("type mismatch") should find it
    // SearchKeyword("database connection") should NOT find it
}

// Test 5: SearchSemantic finds by meaning (requires embedding)
func TestStore_SearchSemantic(t *testing.T) {
    // Insert observation with known embedding
    // SearchSemantic with a similar embedding should find it
    // SearchSemantic with a very different embedding should not
}

// Test 6: Private/shared filtering
func TestStore_PrivateSharedFiltering(t *testing.T) {
    // Insert private observation for developer A
    // Insert shared observation for developer A
    // Search as developer B should find only the shared one
    // Search as developer A should find both
}

// Test 7: TouchRecalled updates recalled_at and boosts confidence
func TestStore_TouchRecalled(t *testing.T) {
    // Insert observation with confidence 0.5
    // Call TouchRecalled
    // GetByID — recalled_at should be set, confidence should be 0.6 (0.5 + 0.1)
}

// Test 8: DeleteByConfidence removes low-confidence observations
func TestStore_DeleteByConfidence(t *testing.T) {
    // Insert observation with confidence 0.05
    // Insert observation with confidence 0.5
    // DeleteByConfidence(0.1) should delete the first, keep the second
}

// ============================================================
// RETRIEVER TESTS
// ============================================================

// Test 9: Hybrid search merges results from all three sources
func TestRetriever_HybridSearch(t *testing.T) {
    // Create a mock store with known data
    // Create a mock graph querier
    // Create a mock embedder
    // Search with both text and graph node IDs
    // Verify results come from all three sources and are properly ranked
}

// Test 10: Graph neighbor expansion
func TestRetriever_GraphNeighborExpansion(t *testing.T) {
    // Mock graph querier returns callers and callees
    // Search by graph node ID should also search neighbor nodes
    // Verify neighbor results have lower score than direct matches
}

// Test 11: GetSessionContext returns graph-based results
func TestRetriever_GetSessionContext(t *testing.T) {
    // Insert observations for known graph nodes
    // GetSessionContext with those nodes should return matching observations
}

// Test 12: Empty query still works (graph-only search)
func TestRetriever_EmptyTextQuery(t *testing.T) {
    // Search with graph_node_ids but no text
    // Should return graph-matched results only (no keyword or semantic)
}

// ============================================================
// COMPRESSOR TESTS
// ============================================================

// Test 13: CompressEvents produces valid observations
func TestCompressor_CompressEvents(t *testing.T) {
    // Create mock events
    // Compress them (requires Haiku API or mock)
    // Verify output has summary, detail, and category
}

// Test 14: CompressEvents groups by graph node
func TestCompressor_GroupsByGraphNode(t *testing.T) {
    // Create events touching 3 different graph nodes
    // Compress should produce 3 observations (one per node)
}

// ============================================================
// HOOKS TESTS
// ============================================================

// Test 15: OnToolCall creates session on first call
func TestHooks_CreatesSession(t *testing.T) {
    // Call OnToolCall for a new developer
    // Verify session exists in the manager
}

// Test 16: OnToolCall accumulates events
func TestHooks_AccumulatesEvents(t *testing.T) {
    // Call OnToolCall 5 times for the same developer
    // Verify session has 5 events
}

// Test 17: Session timeout triggers compression
func TestHooks_SessionTimeout(t *testing.T) {
    // Create session, add events
    // Wait for timeout (or manually trigger)
    // Verify observations were stored in the database
}

// ============================================================
// DECAY TESTS
// ============================================================

// Test 18: RunDecay reduces confidence
func TestDecay_ReducesConfidence(t *testing.T) {
    // Insert observation with confidence 1.0 and recalled_at 30 days ago
    // RunDecay with 0.02 rate
    // New confidence should be 1.0 - (30 * 0.02) = 0.4
}

// Test 19: RunDecay deletes below threshold
func TestDecay_DeletesBelowThreshold(t *testing.T) {
    // Insert observation with confidence 0.05
    // RunDecay should delete it
}

// Test 20: Recently recalled observations don't decay
func TestDecay_RecentRecallNoDec(t *testing.T) {
    // Insert observation with recalled_at = now
    // RunDecay should not reduce its confidence
}
```

### How to run tests

```bash
# Set up test database (one-time)
createdb universe_test
psql universe_test -c "CREATE EXTENSION vector;"
psql universe_test -f migrations/002_memory_tables.sql

# Run tests
DATABASE_URL="postgres://localhost:5432/universe_test?sslmode=disable" \
  go test ./internal/memory/ -v

# Run with short flag to skip integration tests that need the database
go test ./internal/memory/ -v -short
```

---

## 15. Acceptance Criteria

Engine 2 is complete when:

- [ ] PostgreSQL migration runs successfully (observations table + all 9 indexes)
- [ ] `store.InsertObservation()` stores and retrieves observations correctly
- [ ] `store.SearchKeyword()` finds observations by matching words
- [ ] `store.SearchSemantic()` finds observations by meaning (vector similarity)
- [ ] `store.GetByGraphNodes()` finds observations tagged to specific graph nodes
- [ ] `retriever.Search()` combines all three search methods with weighted scoring
- [ ] `retriever.Search()` expands graph nodes to include callers/callees
- [ ] `retriever.GetSessionContext()` returns relevant observations for session start
- [ ] `compressor.CompressEvents()` converts raw session events into observations using Haiku
- [ ] `hooks.OnToolCall()` tracks sessions and captures events
- [ ] `hooks.EndSession()` triggers compression and storage
- [ ] `decay.RunDecay()` reduces confidence on old observations and deletes stale ones
- [ ] Private/shared filtering works correctly
- [ ] `store.TouchRecalled()` boosts confidence on recalled observations
- [ ] All 20 tests pass
- [ ] `go build ./...` succeeds with the new package
- [ ] No changes to existing files (graph.go, models.go, etc.)

---

## 16. What NOT to Build in This Engine

- Do NOT modify existing files (graph.go, models.go, analyzer.go, etc.)
- Do NOT build the MCP server — that's a separate integration step
- Do NOT build the web viewer UI — dashboard comes later
- Do NOT build Engine 5 (orchestrator) features — no tiered model routing
- Do NOT implement real-time streaming — observations are stored, not streamed
- Do NOT implement the GraphAdapter yet — provide the interface only, implement during integration
- Do NOT add Redis — all storage is PostgreSQL. Redis is for future event queuing only

---

## 17. Future Improvements (Not in This Build)

1. **Redis caching** — cache frequently retrieved observations in Redis for faster retrieval
2. **Batch embedding** — embed multiple observations in one API call at session end
3. **Local embedding model** — replace OpenAI API with a local model to eliminate external dependency
4. **Observation deduplication** — detect near-duplicate observations and merge them
5. **Memory analytics dashboard** — show memory growth, recall rates, decay stats on the web dashboard
6. **Observation voting** — let developers upvote/downvote observations to influence confidence
7. **Cross-team memory** — share observations across teams (not just within a team)
8. **Memory export/import** — export observations as JSON for backup or migration
