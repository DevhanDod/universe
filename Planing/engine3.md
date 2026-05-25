# Engine 3 — Self-Evolving Skill Engine

## Build Specification for Claude Code

**Engine name:** Self-Evolving Skill Engine (Meta-learning + Knowledge Distillation)  
**Concept:** Automated prompt optimization with graph-aware skill evolution  
**Estimated effort:** 3-4 days  
**Dependencies:** Engine 1 (Graph) — built. Engine 2 (Memory) — built. Engine 4 (Compression) — built.  
**Database required:** PostgreSQL (add tables to existing DB)  
**New services required:** None — adds to existing Go binary  

---

## 1. What This Engine Does (Plain English)

When the AI agent solves a problem, we save the approach as a recipe (a "skill"). Next time anyone on the team hits a similar problem, the agent follows the recipe instead of thinking from scratch — saving tokens and time.

Skills aren't static. They evolve automatically:
- **CAPTURED** — Agent solves a new problem successfully. We extract the approach as a new skill.
- **FIX** — A skill breaks (fails 3 times in a row). We ask Opus to repair the instruction. New version replaces the old one.
- **DERIVED** — A different developer solves the same type of problem with a different approach. We create a specialized variant that coexists with the original.

Every skill is tagged to specific graph nodes (functions, types, APIs) from Engine 1. When code changes in the graph, all skills covering that code are automatically flagged as potentially stale.

Skills have a version history (a family tree called a "DAG"). You can always see how a skill evolved: v1 was captured from Alice's session, v2 was a FIX after the API changed, v3 was DERIVED for Python repos.

**Expected token savings:** ~46% reduction on tasks where a matching skill exists (OpenSpace benchmark). Improves over time as the skill library grows.

---

## 2. Two Graphs in the System (Important Clarification)

There are TWO separate graphs. Don't confuse them:

**Graph 1 — Code Knowledge Graph (Engine 1, already built):**
- Nodes = functions, types, APIs, packages
- Edges = "calls," "imports," "depends on"
- Stored in your existing graph package
- Changes when code changes

**Graph 2 — Skill Version DAG (Engine 3, this spec):**
- Nodes = skill versions (v1, v2, v3...)
- Edges = "evolved from" with type (FIX, DERIVED, CAPTURED)
- Stored as parent_id column in the skills table — NOT a separate database
- Grows when skills evolve

**They connect through `graph_node_ids`.** Every skill is tagged to one or more nodes in Graph 1. When you query "what skills cover auth.ValidateToken?", you're looking up Graph 1 node IDs in the skills table.

---

## 3. Current Project Structure

Engine 3 adds a `skills` package under `internal/`.

```
UNIVERSE/
├── cmd/
├── internal/
│   ├── analyzer/        ← existing
│   ├── compress/        ← Engine 4
│   ├── extractor/       ← existing
│   ├── graph/           ← Engine 1 (existing)
│   ├── memory/          ← Engine 2
│   ├── models/          ← existing
│   ├── parser/          ← existing
│   ├── scanner/         ← existing
│   └── skills/          ← NEW (Engine 3)
├── migrations/
│   ├── 002_memory_tables.sql
│   └── 003_skills_tables.sql    ← NEW
├── go.mod
└── go.sum
```

---

## 4. New Files to Create

```
internal/
└── skills/
    ├── types.go          # All types, constants, configuration
    ├── store.go          # PostgreSQL CRUD for skills + executions
    ├── matcher.go        # 3-way hybrid search to find best skill for a task
    ├── executor.go       # Apply a skill: inject instruction into agent prompt
    ├── evolver.go        # FIX / DERIVED / CAPTURED evolution logic
    ├── monitor.go        # Quality metrics tracking + evolution triggers
    ├── graph_sync.go     # When graph changes, flag affected skills
    ├── safety.go         # Safety scanner, anti-loop, negative tagging
    └── skills_test.go    # All tests

migrations/
└── 003_skills_tables.sql     # Skills + executions tables + indexes
```

---

## 5. Database Migration: `migrations/003_skills_tables.sql`

```sql
-- ============================================================
-- Engine 3: Self-Evolving Skill Engine
-- Migration: 003_skills_tables.sql
-- ============================================================

-- The skills table stores every skill version.
-- Each row is one version of a skill. The version DAG (family tree)
-- is tracked via parent_id — each skill points to the version it evolved from.
CREATE TABLE IF NOT EXISTS skills (
    -- Unique identifier for this skill version
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Human-readable skill name (e.g., "cross-repo-type-fix")
    -- Same name shared across versions. Name + version is unique.
    name            TEXT NOT NULL,

    -- Version number within this skill family (1, 2, 3...)
    version         INT NOT NULL DEFAULT 1,

    -- Parent skill version this was evolved from.
    -- NULL for the first version (CAPTURED from scratch).
    -- Points to the previous version's id for FIX.
    -- Points to the source skill's id for DERIVED.
    -- This is the "Graph 2" — the version DAG.
    parent_id       UUID REFERENCES skills(id),

    -- How this version was created
    -- 'captured'  — extracted from a successful session (no prior skill matched)
    -- 'fix'       — repaired from a broken previous version
    -- 'derived'   — specialized variant created from a different approach
    -- 'manual'    — manually written by a developer (seed skills)
    evolution       TEXT NOT NULL CHECK (evolution IN ('captured', 'fix', 'derived', 'manual')),

    -- Which Graph 1 nodes this skill covers.
    -- Format: array of graph node IDs from Engine 1.
    -- e.g., {"auth-service:auth:ValidateToken", "gateway-service:handlers:LoginHandler"}
    -- A skill can cover multiple graph nodes.
    -- This is the CONNECTION between Graph 1 and Graph 2.
    graph_node_ids  TEXT[] NOT NULL,

    -- The language this skill applies to (from the graph nodes' repo language)
    -- Prevents cross-language wrong application.
    -- e.g., "go", "python", "typescript"
    language        TEXT,

    -- When to use this skill — a short description of the trigger condition.
    -- Used by the matcher for text-based search.
    -- e.g., "When there's a type mismatch between two repos in an API contract"
    trigger_desc    TEXT NOT NULL,

    -- The actual instruction — the recipe the agent follows.
    -- This is the core content. When matched, this text is injected
    -- into the agent's system prompt.
    -- Written by: Opus (for FIX and CAPTURED), developer (for manual)
    instruction     TEXT NOT NULL,

    -- A test case captured alongside the skill.
    -- JSON: {"input": "the task that triggered capture", "expected_output": "what the agent produced"}
    -- Future FIX versions must pass this test case before activation.
    test_case       JSONB,

    -- "Don't use when" conditions — negative tags from past failures.
    -- JSON array: [{"context": "python repo", "reason": "skill is Go-specific"}]
    -- Matcher checks these before applying the skill.
    negative_tags   JSONB DEFAULT '[]'::jsonb,

    -- Embedding of trigger_desc + instruction for semantic search
    embedding       vector(1536),

    -- Who created/triggered this skill version
    created_by      TEXT NOT NULL,

    -- Is this visible to the whole team?
    shared          BOOLEAN NOT NULL DEFAULT true,

    -- Is this the active (latest) version within its name family?
    -- Only one version per name should be active at a time.
    -- FIX replaces the parent: parent becomes inactive, new version becomes active.
    -- DERIVED creates a new name: both original and derived are active.
    is_active       BOOLEAN NOT NULL DEFAULT true,

    -- Is this skill frozen (stopped evolving due to repeated failures)?
    -- Frozen skills still work (old version), they just stop evolving.
    -- Requires human review to unfreeze.
    is_frozen       BOOLEAN NOT NULL DEFAULT false,

    -- How many times this skill was involved in an evolution attempt
    -- in the last 24 hours. Used for anti-loop protection.
    -- Reset daily by the monitor.
    evolution_attempts_today INT NOT NULL DEFAULT 0,

    -- Quality metrics (updated after each use)
    times_applied       INT NOT NULL DEFAULT 0,
    times_succeeded     INT NOT NULL DEFAULT 0,
    times_failed        INT NOT NULL DEFAULT 0,

    -- Complexity-segmented success tracking
    -- JSON: {"simple": {"applied": 10, "succeeded": 9}, "complex": {"applied": 5, "succeeded": 2}}
    -- "simple" = 1-2 affected nodes, same repo
    -- "complex" = 3+ affected nodes OR cross-repo
    success_by_complexity JSONB DEFAULT '{"simple":{"applied":0,"succeeded":0},"complex":{"applied":0,"succeeded":0}}'::jsonb,

    -- Average tokens saved when this skill is applied
    -- (compared to estimated cost without the skill)
    avg_tokens_saved    FLOAT DEFAULT 0,

    -- Tentative confidence: starts at 0.5 for new skills.
    -- Reaches 1.0 after 5+ successful applications.
    -- Formula: min(1.0, 0.5 + (times_succeeded * 0.1))
    confidence          FLOAT NOT NULL DEFAULT 0.5,

    -- Last time this skill was applied
    last_applied_at     TIMESTAMPTZ,

    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Unique constraint: one version number per skill name
    UNIQUE(name, version)
);

-- The skill_executions table logs every time a skill is applied.
-- This is the "experience replay buffer" that the evolver learns from.
CREATE TABLE IF NOT EXISTS skill_executions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Which skill version was applied
    skill_id        UUID NOT NULL REFERENCES skills(id),

    -- Who ran it
    developer_id    TEXT NOT NULL,

    -- Which session it was applied in (links to Engine 2's sessions)
    session_id      TEXT,

    -- Did the skill application succeed?
    success         BOOLEAN NOT NULL,

    -- How many tokens were used
    tokens_used     INT,

    -- If failed, what went wrong
    error_detail    TEXT,

    -- Task complexity at time of execution
    -- 'simple' or 'complex' (based on affected node count and cross-repo flag)
    complexity      TEXT CHECK (complexity IN ('simple', 'complex')),

    -- Which graph nodes were involved
    graph_node_ids  TEXT[],

    -- The developer's original request (for CAPTURED skill extraction)
    task_prompt     TEXT,

    -- The agent's output (for CAPTURED skill extraction)
    task_output     TEXT,

    -- Timestamp
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- INDEXES
-- ============================================================

-- Graph node lookup — "which skills cover this function?"
-- GIN index for array containment queries: WHERE graph_node_ids @> ARRAY['node_id']
CREATE INDEX idx_skills_graph_nodes ON skills USING gin(graph_node_ids);

-- Name lookup — "get all versions of this skill"
CREATE INDEX idx_skills_name ON skills(name);

-- Active filter — "get only active skills"
CREATE INDEX idx_skills_active ON skills(is_active) WHERE is_active = true;

-- Language filter — "get skills for Go repos only"
CREATE INDEX idx_skills_language ON skills(language);

-- Parent lookup — "get the evolution history (children of this skill)"
CREATE INDEX idx_skills_parent ON skills(parent_id);

-- Evolution type — "get all CAPTURED skills"
CREATE INDEX idx_skills_evolution ON skills(evolution);

-- Embedding search — semantic similarity
CREATE INDEX idx_skills_embedding ON skills
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 50);

-- Full-text search on trigger_desc + instruction
ALTER TABLE skills ADD COLUMN fts tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', trigger_desc || ' ' || instruction)
    ) STORED;
CREATE INDEX idx_skills_fts ON skills USING gin(fts);

-- Execution lookups
CREATE INDEX idx_executions_skill ON skill_executions(skill_id);
CREATE INDEX idx_executions_developer ON skill_executions(developer_id);
CREATE INDEX idx_executions_session ON skill_executions(session_id);
CREATE INDEX idx_executions_success ON skill_executions(success);

-- Confidence for pruning low-usage skills
CREATE INDEX idx_skills_confidence ON skills(confidence, last_applied_at);

-- Frozen skills for human review dashboard
CREATE INDEX idx_skills_frozen ON skills(is_frozen) WHERE is_frozen = true;
```

---

## 6. File: `internal/skills/types.go`

All types, constants, and configuration.

```go
package skills

import "time"

// ============================================================
// SKILL — the core data type
// ============================================================

// Skill is one version of a reusable recipe.
type Skill struct {
    ID                   string            `json:"id"`
    Name                 string            `json:"name"`
    Version              int               `json:"version"`
    ParentID             *string           `json:"parent_id,omitempty"`
    Evolution            EvolutionType     `json:"evolution"`
    GraphNodeIDs         []string          `json:"graph_node_ids"`
    Language             string            `json:"language"`
    TriggerDesc          string            `json:"trigger_desc"`
    Instruction          string            `json:"instruction"`
    TestCase             *SkillTestCase    `json:"test_case,omitempty"`
    NegativeTags         []NegativeTag     `json:"negative_tags"`
    Embedding            []float32         `json:"embedding,omitempty"`
    CreatedBy            string            `json:"created_by"`
    Shared               bool              `json:"shared"`
    IsActive             bool              `json:"is_active"`
    IsFrozen             bool              `json:"is_frozen"`
    EvolutionAttemptsToday int             `json:"evolution_attempts_today"`
    TimesApplied         int               `json:"times_applied"`
    TimesSucceeded       int               `json:"times_succeeded"`
    TimesFailed          int               `json:"times_failed"`
    SuccessByComplexity  ComplexityMetrics `json:"success_by_complexity"`
    AvgTokensSaved       float64           `json:"avg_tokens_saved"`
    Confidence           float64           `json:"confidence"`
    LastAppliedAt        *time.Time        `json:"last_applied_at,omitempty"`
    CreatedAt            time.Time         `json:"created_at"`
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
// Future FIX versions must pass this test before activation.
type SkillTestCase struct {
    Input          string `json:"input"`           // the task prompt that triggered capture
    ExpectedOutput string `json:"expected_output"`  // what the agent produced (or key assertions)
    GraphNodeIDs   []string `json:"graph_node_ids"` // which graph nodes were involved
}

// NegativeTag records a context where the skill should NOT be used.
// Added when a skill fails in a specific context.
type NegativeTag struct {
    Context  string `json:"context"`  // e.g., "python repo", "cross-repo with 5+ nodes"
    Reason   string `json:"reason"`   // e.g., "skill is Go-specific, uses go vet"
    AddedAt  string `json:"added_at"` // ISO timestamp
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
    ID           string        `json:"id"`
    Name         string        `json:"name"`
    Version      int           `json:"version"`
    Evolution    EvolutionType `json:"evolution"`
    TriggerDesc  string        `json:"trigger_desc"`
    Language     string        `json:"language"`
    Confidence   float64       `json:"confidence"`
    SuccessRate  float64       `json:"success_rate"`
    GraphOverlap float64       `json:"graph_overlap"` // 0.0-1.0
    SearchScore  float64       `json:"search_score"`  // combined search relevance
    IsFrozen     bool          `json:"is_frozen"`
}

// ============================================================
// EXECUTION — tracking every skill application
// ============================================================

// SkillExecution logs one application of a skill.
type SkillExecution struct {
    ID           string    `json:"id"`
    SkillID      string    `json:"skill_id"`
    DeveloperID  string    `json:"developer_id"`
    SessionID    string    `json:"session_id"`
    Success      bool      `json:"success"`
    TokensUsed   int       `json:"tokens_used"`
    ErrorDetail  string    `json:"error_detail,omitempty"`
    Complexity   string    `json:"complexity"` // "simple" or "complex"
    GraphNodeIDs []string  `json:"graph_node_ids"`
    TaskPrompt   string    `json:"task_prompt"`
    TaskOutput   string    `json:"task_output"`
    ExecutedAt   time.Time `json:"executed_at"`
}

// ============================================================
// MATCHING — input/output for skill search
// ============================================================

// MatchQuery defines what we're looking for.
type MatchQuery struct {
    // The developer's task description
    TaskText string

    // Graph node IDs from the current context
    GraphNodeIDs []string

    // Language of the current repo
    Language string

    // Task complexity (for complexity-weighted scoring)
    Complexity string // "simple" or "complex"

    // Who is requesting (for exploration rate)
    DeveloperID string

    // Maximum results
    Limit int
}

// MatchResult is the output of a skill search.
type MatchResult struct {
    // Best matching skill (nil if no good match)
    BestMatch *SkillSummary `json:"best_match"`

    // All candidates ranked by score (for presenting options)
    Candidates []SkillSummary `json:"candidates"`

    // Was the exploration rate triggered? (10% chance to skip skills)
    ExplorationTriggered bool `json:"exploration_triggered"`

    // Search method used
    SearchMethod string `json:"search_method"`
}

// ============================================================
// EVOLUTION — input/output for skill evolution
// ============================================================

// EvolutionRequest is sent to the evolver after a task completes.
type EvolutionRequest struct {
    // The skill that was applied (nil if no skill matched)
    AppliedSkill *Skill

    // The execution result
    Execution SkillExecution

    // Session events from Engine 2 (for CAPTURED extraction)
    SessionEvents []SessionEventSummary

    // Graph context from Engine 1
    GraphContext string

    // Actual code files involved (for richer FIX context)
    // Map of file path → file content (relevant sections only)
    CodeContext map[string]string
}

// SessionEventSummary is a simplified version of Engine 2's SessionEvent.
// Avoids direct dependency on the memory package.
type SessionEventSummary struct {
    ToolName    string `json:"tool_name"`
    GraphNodeID string `json:"graph_node_id"`
    Input       string `json:"input"`
    Output      string `json:"output"`
    Success     bool   `json:"success"`
}

// EvolutionResult describes what happened during evolution.
type EvolutionResult struct {
    Action       string  `json:"action"`        // "captured", "fixed", "derived", "skipped", "frozen"
    NewSkillID   string  `json:"new_skill_id"`  // ID of the new version (if created)
    Reason       string  `json:"reason"`         // why this action was taken
    TokensSpent  int     `json:"tokens_spent"`   // Opus tokens used for evolution
}

// ============================================================
// SAFETY — scanning and validation
// ============================================================

// SafetyScanResult describes the outcome of scanning a skill instruction.
type SafetyScanResult struct {
    Safe     bool     `json:"safe"`
    Warnings []string `json:"warnings"` // non-blocking issues
    Blocked  []string `json:"blocked"`  // blocking issues — skill not stored
}

// ============================================================
// CONFIGURATION
// ============================================================

type Config struct {
    // Database
    DatabaseURL string

    // Embedding
    EmbeddingAPIKey string
    EmbeddingModel  string // default: "text-embedding-ada-002"

    // LLM for evolution (Opus)
    OpusAPIKey    string
    OpusModel     string // default: "claude-opus-4-20250514"
    OpusEndpoint  string // default: "https://api.anthropic.com/v1/messages"

    // Matching thresholds
    MinConfidenceForMatch     float64 // Minimum confidence to use a skill. Default: 0.5
    MinSuccessRateForMatch    float64 // Minimum success rate. Default: 0.60
    MinGraphOverlapForMatch   float64 // Minimum graph node overlap. Default: 0.3
    SimilarityThresholdForSkip float64 // Skip CAPTURE if similar skill exists. Default: 0.85
    MaxSkillsPerGraphNode     int     // Max active skills per graph node. Default: 5

    // Search weights (3-way hybrid search)
    GraphScoreWeight    float64 // Default: 0.40
    KeywordScoreWeight  float64 // Default: 0.25
    SemanticScoreWeight float64 // Default: 0.35

    // Evolution triggers
    ConsecutiveFailuresForFix int     // Failures before FIX triggers. Default: 3
    FailureRateForFix         float64 // Failure rate over last 10 to trigger FIX. Default: 0.40

    // Safety
    MaxEvolutionAttemptsPerDay int  // Anti-loop: max evolution cycles per skill per day. Default: 3
    MaxTotalSkills             int  // Hard cap on total skills. Default: 500

    // Exploration
    ExplorationRate float64 // Chance to skip skills and reason fresh. Default: 0.10

    // Pruning
    PruneAfterDays       int // Delete unused skills after N days. Default: 30
    PruneRunInterval     string // How often to run pruning. Default: "24h"

    // Tentative confidence
    ConfidenceStartValue  float64 // New skills start at this. Default: 0.5
    ConfidencePerSuccess  float64 // Added per successful application. Default: 0.1
    ConfidenceMaxValue    float64 // Maximum confidence. Default: 1.0
    ConfidenceMinSuccesses int    // Successes needed to reach max. Default: 5
}

func DefaultConfig() Config {
    return Config{
        EmbeddingModel:             "text-embedding-ada-002",
        OpusModel:                  "claude-opus-4-20250514",
        OpusEndpoint:               "https://api.anthropic.com/v1/messages",
        MinConfidenceForMatch:      0.5,
        MinSuccessRateForMatch:     0.60,
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
```

---

## 7. File: `internal/skills/store.go`

PostgreSQL CRUD operations.

```go
package skills

// Store handles all database operations for skills and executions.
// Uses pgx for PostgreSQL access (same as Engine 2).

// NewStore creates a new Store connected to PostgreSQL.
func NewStore(databaseURL string) (*Store, error)

func (s *Store) Close() error

// ============================================================
// SKILL CRUD
// ============================================================

// InsertSkill stores a new skill version.
// Sets confidence to Config.ConfidenceStartValue.
// Returns the stored Skill with generated ID.
func (s *Store) InsertSkill(skill Skill) (*Skill, error)

// GetByID retrieves a skill by UUID.
func (s *Store) GetByID(id string) (*Skill, error)

// GetActiveByName retrieves the active version of a named skill.
// SQL: WHERE name = $1 AND is_active = true
func (s *Store) GetActiveByName(name string) (*Skill, error)

// GetByGraphNodes retrieves all active skills covering any of the given graph nodes.
// This is the primary graph-aware lookup.
//
// SQL:
//   SELECT * FROM skills
//   WHERE is_active = true
//     AND graph_node_ids && $1   -- array overlap operator
//     AND (language IS NULL OR language = $2)
//     AND confidence >= $3
//   ORDER BY confidence DESC, times_succeeded DESC
//   LIMIT $4
func (s *Store) GetByGraphNodes(nodeIDs []string, language string, minConfidence float64, limit int) ([]Skill, error)

// SearchKeyword performs full-text search on trigger_desc + instruction.
//
// SQL:
//   SELECT *, ts_rank(fts, plainto_tsquery('english', $1)) AS score
//   FROM skills
//   WHERE fts @@ plainto_tsquery('english', $1)
//     AND is_active = true
//     AND confidence >= $2
//   ORDER BY score DESC
//   LIMIT $3
func (s *Store) SearchKeyword(query string, minConfidence float64, limit int) ([]SkillSummary, error)

// SearchSemantic performs vector similarity search.
//
// SQL:
//   SELECT *, 1 - (embedding <=> $1) AS score
//   FROM skills
//   WHERE is_active = true
//     AND confidence >= $2
//   ORDER BY embedding <=> $1
//   LIMIT $3
func (s *Store) SearchSemantic(queryEmbedding []float32, minConfidence float64, limit int) ([]SkillSummary, error)

// GetLineage retrieves the full evolution history of a skill.
// Traverses the version DAG upward (child → parent → grandparent...).
//
// SQL (recursive CTE):
//   WITH RECURSIVE lineage AS (
//       SELECT * FROM skills WHERE id = $1
//       UNION ALL
//       SELECT s.* FROM skills s JOIN lineage l ON s.id = l.parent_id
//   )
//   SELECT * FROM lineage ORDER BY version ASC
func (s *Store) GetLineage(skillID string) ([]Skill, error)

// GetChildren retrieves all skills that evolved from a given skill.
// SQL: WHERE parent_id = $1
func (s *Store) GetChildren(skillID string) ([]Skill, error)

// CountSkillsForGraphNode returns how many active skills cover a graph node.
// Used to enforce MaxSkillsPerGraphNode limit.
// SQL: SELECT COUNT(*) FROM skills WHERE graph_node_ids @> ARRAY[$1] AND is_active = true
func (s *Store) CountSkillsForGraphNode(nodeID string) (int, error)

// GetTotalActiveSkills returns the total number of active skills.
// Used to enforce MaxTotalSkills limit.
func (s *Store) GetTotalActiveSkills() (int, error)

// ============================================================
// SKILL UPDATES
// ============================================================

// DeactivateSkill marks a skill as inactive (replaced by a new version).
// SQL: UPDATE skills SET is_active = false WHERE id = $1
func (s *Store) DeactivateSkill(id string) error

// FreezeSkill marks a skill as frozen (stopped evolving, needs human review).
// SQL: UPDATE skills SET is_frozen = true WHERE id = $1
func (s *Store) FreezeSkill(id string) error

// UnfreezeSkill unfreezes a skill (after human review).
// Also resets evolution_attempts_today to 0.
func (s *Store) UnfreezeSkill(id string) error

// IncrementEvolutionAttempts adds 1 to evolution_attempts_today.
// SQL: UPDATE skills SET evolution_attempts_today = evolution_attempts_today + 1 WHERE id = $1
func (s *Store) IncrementEvolutionAttempts(id string) error

// ResetDailyEvolutionAttempts resets all skills' daily counters to 0.
// Called once per day by the monitor.
// SQL: UPDATE skills SET evolution_attempts_today = 0
func (s *Store) ResetDailyEvolutionAttempts() error

// UpdateMetrics updates a skill's quality metrics after an execution.
//
// Parameters:
//   - id: skill UUID
//   - success: whether this execution succeeded
//   - complexity: "simple" or "complex"
//   - tokensSaved: estimated tokens saved by using this skill
//
// Updates: times_applied, times_succeeded/failed, success_by_complexity,
//          avg_tokens_saved, confidence, last_applied_at
func (s *Store) UpdateMetrics(id string, success bool, complexity string, tokensSaved float64) error

// AddNegativeTag adds a "don't use when" tag to a skill.
func (s *Store) AddNegativeTag(id string, tag NegativeTag) error

// MarkGraphNodesStale flags all skills covering the given graph nodes as
// "needs review" by adding a negative tag with context "graph_changed".
//
// SQL:
//   UPDATE skills
//   SET negative_tags = negative_tags || $2::jsonb
//   WHERE graph_node_ids && $1
//     AND is_active = true
func (s *Store) MarkGraphNodesStale(changedNodeIDs []string) error

// ============================================================
// EXECUTION LOG
// ============================================================

// InsertExecution logs a skill application.
func (s *Store) InsertExecution(exec SkillExecution) (*SkillExecution, error)

// GetRecentExecutions retrieves recent executions for a skill.
// Used by the monitor to check failure patterns.
//
// SQL: WHERE skill_id = $1 ORDER BY executed_at DESC LIMIT $2
func (s *Store) GetRecentExecutions(skillID string, limit int) ([]SkillExecution, error)

// GetConsecutiveFailures returns the number of consecutive failures
// for a skill (most recent first, counting until a success is found).
func (s *Store) GetConsecutiveFailures(skillID string) (int, error)

// GetRecentFailureRate returns the failure rate over the last N executions.
func (s *Store) GetRecentFailureRate(skillID string, lastN int) (float64, error)

// GetSuccessfulSessionsWithoutSkill retrieves recent successful task
// executions where NO skill was matched. These are candidates for CAPTURE.
//
// SQL:
//   SELECT * FROM skill_executions
//   WHERE skill_id IS NULL
//     AND success = true
//     AND task_prompt IS NOT NULL
//   ORDER BY executed_at DESC
//   LIMIT $1
func (s *Store) GetSuccessfulSessionsWithoutSkill(limit int) ([]SkillExecution, error)

// ============================================================
// PRUNING
// ============================================================

// PruneUnusedSkills deletes active skills with 0 applications after N days.
//
// SQL:
//   DELETE FROM skills
//   WHERE is_active = true
//     AND times_applied = 0
//     AND created_at < NOW() - interval '$1 days'
//   RETURNING id, name
func (s *Store) PruneUnusedSkills(afterDays int) (int, error)

// ============================================================
// STATS
// ============================================================

type SkillStats struct {
    TotalActive      int            `json:"total_active"`
    TotalFrozen      int            `json:"total_frozen"`
    ByEvolution      map[string]int `json:"by_evolution"`  // captured: 30, fix: 10, derived: 5
    ByLanguage       map[string]int `json:"by_language"`
    AvgConfidence    float64        `json:"avg_confidence"`
    AvgSuccessRate   float64        `json:"avg_success_rate"`
    TotalExecutions  int            `json:"total_executions"`
    TotalTokensSaved float64        `json:"total_tokens_saved"`
}

func (s *Store) GetStats() (*SkillStats, error)
```

---

## 8. File: `internal/skills/matcher.go`

3-way hybrid search: graph node overlap + keyword + semantic.

```go
package skills

// Matcher finds the best skill for a given task.
// Uses 3-way hybrid search (same pattern as Engine 2's retriever).
//
// Search flow:
//   1. Graph node match: skills covering the same graph nodes
//   2. Keyword search: PostgreSQL FTS on trigger_desc + instruction
//   3. Semantic search: pgvector cosine similarity on embedding
//   4. Merge, deduplicate, apply weighted scoring
//   5. Filter: check negative tags, language, confidence, exploration rate
//   6. Return ranked candidates

// NewMatcher creates a new Matcher.
// Parameters:
//   - store: for database access
//   - embedder: function to convert text → embedding (same as Engine 2)
//   - config: matching thresholds and weights
func NewMatcher(store *Store, embedder EmbedFunc, config Config) *Matcher

// EmbedFunc converts text to a vector embedding.
// Same type as Engine 2 — can share the same embedder instance.
type EmbedFunc func(text string) ([]float32, error)

// Match finds the best skill for a task.
//
// ZERO LLM tokens — all database queries.
//
// Implementation (step by step):
//
//   STEP 1: EXPLORATION CHECK
//     Generate a random number 0.0-1.0.
//     If < config.ExplorationRate (default 10%):
//       Return MatchResult with ExplorationTriggered=true, BestMatch=nil.
//       The agent will reason from scratch.
//       This prevents the system from getting stuck on one approach.
//
//   STEP 2: RUN THREE SEARCHES IN PARALLEL
//
//     Search A — Graph node overlap:
//       graphResults := store.GetByGraphNodes(query.GraphNodeIDs, query.Language, config.MinConfidenceForMatch, query.Limit*2)
//       For each result, calculate graphOverlap:
//         overlap = len(intersection(skill.GraphNodeIDs, query.GraphNodeIDs)) / len(query.GraphNodeIDs)
//       Set score = overlap
//
//     Search B — Keyword search:
//       If query.TaskText is not empty:
//         keywordResults := store.SearchKeyword(query.TaskText, config.MinConfidenceForMatch, query.Limit*2)
//         Score comes from ts_rank
//
//     Search C — Semantic search:
//       If query.TaskText is not empty:
//         queryEmbedding := embedder(query.TaskText)
//         semanticResults := store.SearchSemantic(queryEmbedding, config.MinConfidenceForMatch, query.Limit*2)
//         Score comes from cosine similarity
//
//   STEP 3: MERGE AND DEDUPLICATE
//     Create map[skillID]*scoredSkill
//     For each result from all three searches:
//       Accumulate: graphScore, keywordScore, semanticScore
//
//   STEP 4: CALCULATE FINAL WEIGHTED SCORE
//     finalScore = (graphScore * config.GraphScoreWeight) +
//                  (keywordScore * config.KeywordScoreWeight) +
//                  (semanticScore * config.SemanticScoreWeight)
//
//   STEP 5: FILTER
//     Remove skills where:
//       a. graphOverlap < config.MinGraphOverlapForMatch
//       b. successRate < config.MinSuccessRateForMatch
//       c. language doesn't match query.Language (unless skill.Language is empty)
//       d. skill is frozen
//       e. negative tags match the current context:
//          For each negative tag, check if tag.Context matches query attributes
//          (e.g., tag says "python repo" and query.Language is "python" → skip)
//       f. "graph_changed" negative tag exists — skill flagged as stale
//          For stale skills, reduce score by 50% but don't remove
//          (the agent sees it but knows it might be outdated)
//
//   STEP 6: COMPLEXITY-WEIGHTED SCORING
//     If query.Complexity is "complex":
//       Prefer skills with higher success_by_complexity.complex.succeeded rate
//       Multiply score by (complex_success_rate / overall_success_rate)
//       This means a skill with 80% complex success beats one with 95% simple-only success
//
//   STEP 7: SORT AND RETURN
//     Sort by finalScore descending
//     BestMatch = top result (if score > 0.3)
//     Candidates = top N results
//
// Returns: *MatchResult
func (m *Matcher) Match(query MatchQuery) (*MatchResult, error)

// CalculateGraphOverlap computes the fraction of query graph nodes
// that the skill covers.
// overlap = len(intersection) / len(queryNodes)
// Returns 0.0 if queryNodes is empty.
func CalculateGraphOverlap(skillNodes []string, queryNodes []string) float64

// CheckNegativeTags checks if any of the skill's negative tags
// match the current task context.
// Returns true if the skill should be SKIPPED.
func CheckNegativeTags(tags []NegativeTag, language string, complexity string, repoID string) bool
```

---

## 9. File: `internal/skills/executor.go`

Applies a matched skill by injecting it into the agent's prompt.

```go
package skills

// Executor applies a skill: injects the skill instruction into the agent prompt.

// NewExecutor creates a new Executor.
func NewExecutor(store *Store, config Config) *Executor

// Apply applies a skill to a task.
//
// Parameters:
//   - skill: the matched skill to apply
//   - taskPrompt: the developer's original request
//   - graphContext: compressed graph shorthand from Engine 4
//
// Returns:
//   - systemPrompt: the system prompt with skill instruction injected
//   - error
//
// The returned systemPrompt is passed to the LLM (Haiku in Engine 5).
//
// Prompt assembly:
//   SKILL INSTRUCTION (follow this recipe):
//   [skill.Instruction]
//
//   GRAPH CONTEXT:
//   [graphContext from Engine 4]
//
//   NOTE: This skill has been applied [N] times with [X%] success rate.
//   [If "graph_changed" negative tag exists: "WARNING: The code this skill
//    covers may have changed recently. Verify before applying blindly."]
//
//   TASK:
//   [taskPrompt]
func (e *Executor) Apply(skill *Skill, taskPrompt string, graphContext string) (string, error)

// RecordExecution logs the result of applying a skill.
// Called after the agent finishes executing the skill.
//
// Parameters:
//   - skillID: which skill was applied
//   - execution: the execution details
//
// Implementation:
//   1. Insert execution record: store.InsertExecution(execution)
//   2. Update skill metrics: store.UpdateMetrics(skillID, success, complexity, tokensSaved)
//   3. Update confidence:
//      If success: confidence = min(config.ConfidenceMaxValue, confidence + config.ConfidencePerSuccess)
//      If failure: confidence stays the same (confidence only grows, decay handles the rest)
//   4. If failure: check if negative tag should be added
//      If the failure seems context-specific (language mismatch, repo-specific):
//        Call store.AddNegativeTag with the failure context
func (e *Executor) RecordExecution(skillID string, execution SkillExecution) error

// RecordSessionWithoutSkill logs a task execution where no skill matched.
// These are candidates for CAPTURE mode.
// Called by Engine 5's orchestrator when a task succeeds with no skill.
func (e *Executor) RecordSessionWithoutSkill(execution SkillExecution) error
```

---

## 10. File: `internal/skills/evolver.go`

The three evolution modes: CAPTURED, FIX, DERIVED.

```go
package skills

// Evolver handles skill evolution — creating new versions from execution data.
// Uses Opus (premium model) for analysis and instruction generation.

// LLMClient is an interface to call the premium model.
// Injected by Engine 5's orchestrator to break circular dependency.
// Engine 3 doesn't know about Engine 5 directly.
type LLMClient interface {
    CallOpus(systemPrompt, userMessage string, maxTokens int) (string, error)
}

// NewEvolver creates a new Evolver.
func NewEvolver(store *Store, embedder EmbedFunc, llm LLMClient, safety *SafetyScanner, config Config) *Evolver

// OnExecutionComplete is called after every skill execution.
// Decides whether to trigger evolution.
//
// Parameters:
//   - req: EvolutionRequest with execution data, session events, graph context
//
// Returns: *EvolutionResult describing what happened
//
// Decision tree:
//
//   1. Was a skill applied?
//      NO → check if session was successful → try CAPTURE
//      YES → continue
//
//   2. Did the skill succeed?
//      YES → no evolution needed (return "skipped")
//      NO → continue
//
//   3. Is the skill frozen?
//      YES → return "skipped" (frozen skills don't evolve)
//
//   4. Has the skill hit the daily evolution limit?
//      YES → freeze the skill, return "frozen"
//
//   5. Has the skill failed consecutively >= config.ConsecutiveFailuresForFix?
//      YES → try FIX
//      NO → return "skipped" (not enough evidence yet)
func (e *Evolver) OnExecutionComplete(req EvolutionRequest) (*EvolutionResult, error)

// ============================================================
// CAPTURED — extract new skill from successful session
// ============================================================

// tryCaptureSkill extracts a new skill from a successful task execution
// where no existing skill matched.
//
// Parameters:
//   - req: EvolutionRequest with session events and graph context
//
// Implementation:
//
//   STEP 1: CHECK IF CAPTURE IS WORTHWHILE
//     If session had fewer than 3 events → skip (too simple to be a reusable skill)
//     If req.Execution.GraphNodeIDs is empty → skip (can't tag to graph)
//
//   STEP 2: CHECK FOR DUPLICATES
//     Embed the task prompt: embedding := embedder(req.Execution.TaskPrompt)
//     Search existing skills: results := store.SearchSemantic(embedding, 0, 5)
//     If top result similarity > config.SimilarityThresholdForSkip (0.85):
//       Skip — similar skill already exists
//
//   STEP 3: CHECK GRAPH NODE CAPACITY
//     For each graph node in req.Execution.GraphNodeIDs:
//       count := store.CountSkillsForGraphNode(nodeID)
//       If count >= config.MaxSkillsPerGraphNode:
//         Skip this node (or merge with most similar existing skill — DERIVED instead)
//
//   STEP 4: CHECK TOTAL SKILL LIMIT
//     total := store.GetTotalActiveSkills()
//     If total >= config.MaxTotalSkills:
//       Skip — global limit reached. Log warning.
//
//   STEP 5: ASK OPUS TO EXTRACT THE SKILL
//     Prompt:
//       "Extract a reusable skill from this successful agent session.
//
//        SESSION EVENTS:
//        [compressed session events as JSON]
//
//        GRAPH CONTEXT:
//        [graph shorthand from Engine 4]
//
//        CODE CONTEXT:
//        [relevant code file sections]
//
//        ORIGINAL TASK: [task prompt]
//        AGENT OUTPUT: [task output]
//
//        Respond with ONLY this JSON:
//        {
//          "name": "short-kebab-case-name",
//          "trigger_desc": "one sentence: when to use this skill",
//          "instruction": "step-by-step recipe the agent should follow",
//          "language": "go or python or null if language-agnostic"
//        }"
//
//   STEP 6: SAFETY SCAN
//     result := safety.ScanInstruction(instruction)
//     If not safe → skip, log warning
//
//   STEP 7: STORE THE SKILL
//     Create Skill with:
//       Evolution: EvolutionCaptured
//       ParentID: nil
//       GraphNodeIDs: req.Execution.GraphNodeIDs
//       TestCase: {Input: taskPrompt, ExpectedOutput: taskOutput}
//       Confidence: config.ConfidenceStartValue (0.5)
//     Embed trigger_desc + instruction
//     Insert into database
//
//   STEP 8: LOG
//     "Captured new skill [name] v1 covering [N] graph nodes from [developer]'s session"
func (e *Evolver) tryCaptureSkill(req EvolutionRequest) (*EvolutionResult, error)

// ============================================================
// FIX — repair a broken skill
// ============================================================

// tryFixSkill asks Opus to repair a skill that has failed repeatedly.
//
// Parameters:
//   - skill: the broken skill
//   - req: EvolutionRequest with failure details
//
// Implementation:
//
//   STEP 1: CHECK ANTI-LOOP
//     If skill.EvolutionAttemptsToday >= config.MaxEvolutionAttemptsPerDay:
//       Freeze the skill
//       Return "frozen"
//     Increment: store.IncrementEvolutionAttempts(skill.ID)
//
//   STEP 2: GATHER CONTEXT
//     Get last 3 failed executions: failures := store.GetRecentExecutions(skill.ID, 3)
//     Get the error messages from each failure
//
//   STEP 3: ASK OPUS TO FIX THE SKILL
//     Prompt:
//       "This skill has failed repeatedly. Fix the instruction.
//
//        SKILL NAME: [skill.Name]
//        CURRENT INSTRUCTION:
//        [skill.Instruction]
//
//        FAILURE 1: [error detail from execution 1]
//        FAILURE 2: [error detail from execution 2]
//        FAILURE 3: [error detail from execution 3]
//
//        GRAPH CONTEXT:
//        [graph shorthand — shows current state of the code]
//
//        CODE CONTEXT:
//        [actual code files involved — shows what the code looks like NOW]
//
//        The skill may have broken because the code changed. Check the
//        graph context and code context for what's different.
//
//        Respond with ONLY this JSON:
//        {
//          "trigger_desc": "updated trigger description",
//          "instruction": "fixed step-by-step instruction",
//          "what_changed": "one sentence: what you fixed and why"
//        }"
//
//   STEP 4: VALIDATE AGAINST TEST CASE
//     If skill.TestCase is not nil:
//       Run the fixed instruction against the test case input
//       (by calling Haiku with the new instruction + test case input)
//       If Haiku's output doesn't match expected:
//         Return "skipped" — fix didn't work
//
//   STEP 5: SAFETY SCAN
//     result := safety.ScanInstruction(newInstruction)
//     If not safe → skip, log warning
//
//   STEP 6: STORE NEW VERSION
//     Create new Skill with:
//       Name: skill.Name
//       Version: skill.Version + 1
//       ParentID: skill.ID
//       Evolution: EvolutionFix
//       Instruction: fixed instruction
//       TestCase: inherit from parent
//       GraphNodeIDs: inherit from parent
//       Confidence: config.ConfidenceStartValue (0.5 — must prove itself again)
//     Deactivate the old version: store.DeactivateSkill(skill.ID)
//     Insert new version
//
//   STEP 7: LOG
//     "Fixed skill [name] v[N] → v[N+1]: [what_changed]"
func (e *Evolver) tryFixSkill(skill *Skill, req EvolutionRequest) (*EvolutionResult, error)

// ============================================================
// DERIVED — create specialized variant
// ============================================================

// tryDeriveSkill creates a new specialized variant from a different approach.
// Triggered when a developer solves a matched-skill task with a significantly
// different approach (the skill matched but wasn't applied, or was applied
// and then the developer overrode it with a different solution).
//
// Parameters:
//   - parentSkill: the existing skill that was similar but different
//   - req: EvolutionRequest with the new approach
//
// Implementation:
//
//   STEP 1: CHECK SIMILARITY
//     The new approach must be different enough from the parent.
//     Embed the new approach: newEmbedding := embedder(req.Execution.TaskOutput)
//     Compare with parent: similarity := cosineSimilarity(newEmbedding, parentSkill.Embedding)
//     If similarity > 0.90 → skip (too similar, not a real variant)
//     If similarity < 0.30 → skip (too different, should be a new CAPTURE instead)
//
//   STEP 2: ASK OPUS TO CREATE THE VARIANT
//     Prompt:
//       "A developer solved this task with a different approach than the existing skill.
//        Create a specialized variant.
//
//        EXISTING SKILL: [parentSkill.Name]
//        EXISTING INSTRUCTION:
//        [parentSkill.Instruction]
//
//        NEW APPROACH (from developer's session):
//        [session events + task output]
//
//        GRAPH CONTEXT:
//        [graph shorthand]
//
//        Respond with ONLY this JSON:
//        {
//          "name": "variant-name (should indicate what's different)",
//          "trigger_desc": "when to use THIS variant vs the original",
//          "instruction": "step-by-step recipe for the new approach",
//          "language": "if language-specific, which language"
//        }"
//
//   STEP 3: SAFETY SCAN + STORE (same as CAPTURE steps 6-7)
//     New Skill with:
//       ParentID: parentSkill.ID
//       Evolution: EvolutionDerived
//       Both parent and derived remain active (they coexist)
func (e *Evolver) tryDeriveSkill(parentSkill *Skill, req EvolutionRequest) (*EvolutionResult, error)
```

---

## 11. File: `internal/skills/monitor.go`

Quality monitoring and evolution trigger detection.

```go
package skills

// Monitor watches skill health metrics and triggers evolution
// when quality degrades.

// NewMonitor creates a new Monitor.
func NewMonitor(store *Store, evolver *Evolver, config Config) *Monitor

// CheckSkillHealth evaluates a skill after each execution and decides
// if evolution should be triggered.
//
// Called by executor.RecordExecution after logging the result.
//
// Parameters:
//   - skillID: the skill to check
//   - latestExecution: the execution that just completed
//
// Implementation:
//   1. If latestExecution.Success → return (no evolution needed on success)
//   2. Check consecutive failures:
//      count := store.GetConsecutiveFailures(skillID)
//      If count >= config.ConsecutiveFailuresForFix → trigger FIX
//   3. Check failure rate over last 10:
//      rate := store.GetRecentFailureRate(skillID, 10)
//      If rate >= config.FailureRateForFix → trigger FIX
//   4. If neither threshold met → return (not enough evidence yet)
func (m *Monitor) CheckSkillHealth(skillID string, latestExecution SkillExecution)

// RunDailyMaintenance performs scheduled maintenance tasks.
// Call this once per day via background goroutine.
//
// Tasks:
//   1. Reset all daily evolution attempt counters
//   2. Prune unused skills (0 applications after PruneAfterDays)
//   3. Log skill stats
func (m *Monitor) RunDailyMaintenance()

// StartSchedule starts the daily maintenance on a background timer.
func (m *Monitor) StartSchedule()

// Stop stops the background schedule.
func (m *Monitor) Stop()
```

---

## 12. File: `internal/skills/graph_sync.go`

When the knowledge graph changes, flag affected skills.

```go
package skills

// GraphSync listens for graph change events and flags affected skills.
// This is the PROACTIVE stale detection that OpenSpace doesn't have.
//
// When a developer pushes code that changes a function:
//   1. Engine 1's webhook updates the graph node
//   2. A graph change event is published (Redis Stream or direct call)
//   3. GraphSync receives the event
//   4. All skills tagged to the changed graph nodes get a "graph_changed" negative tag
//   5. Next time the skill is matched, the agent sees the warning

// GraphChangeEvent represents a code change detected by the graph.
type GraphChangeEvent struct {
    ChangedNodeIDs []string  `json:"changed_node_ids"`
    ChangeType     string    `json:"change_type"` // "modified", "renamed", "deleted"
    RepoID         string    `json:"repo_id"`
    CommitHash     string    `json:"commit_hash"`
    Timestamp      string    `json:"timestamp"`
}

// NewGraphSync creates a new GraphSync handler.
func NewGraphSync(store *Store) *GraphSync

// OnGraphChange processes a graph change event.
// Flags all skills covering the changed nodes.
//
// Parameters:
//   - event: the graph change event
//
// Implementation:
//   1. Call store.MarkGraphNodesStale(event.ChangedNodeIDs)
//      This adds a negative tag to every skill covering those nodes:
//      {"context": "graph_changed", "reason": "Code changed in commit [hash]: [changeType]"}
//   2. If event.ChangeType is "deleted":
//      Deactivate all skills that ONLY cover the deleted node
//      (skills covering other nodes stay active but flagged)
//   3. If event.ChangeType is "renamed":
//      Update graph_node_ids in skills to the new node ID
//      (so skills don't become orphaned)
//   4. Log: "Graph change in [repo]: [N] skills flagged as stale"
func (gs *GraphSync) OnGraphChange(event GraphChangeEvent) error

// ClearStaleTag removes the "graph_changed" negative tag from a skill.
// Called after a FIX evolution successfully updates the skill for the new code.
func (gs *GraphSync) ClearStaleTag(skillID string) error
```

---

## 13. File: `internal/skills/safety.go`

Safety scanner, anti-loop protection, and negative tagging.

```go
package skills

// SafetyScanner checks skill instructions for dangerous patterns
// before they are stored. Inspired by OpenSpace's safety checks.
//
// Blocks:
//   - External URLs (data exfiltration risk)
//   - Credential references (API keys, passwords, tokens in plaintext)
//   - Shell commands on the blocked list (rm -rf, curl to unknown hosts, etc.)
//   - Prompt injection attempts ("ignore previous instructions", "you are now...")
//   - File system access outside the project directory
//
// Warns (non-blocking):
//   - Very long instructions (>2000 tokens — might be bloated)
//   - Instructions referencing specific file paths (may break across repos)
//   - Instructions with hardcoded values that should be parameters

// NewSafetyScanner creates a new SafetyScanner.
func NewSafetyScanner() *SafetyScanner

// ScanInstruction checks a skill instruction for dangerous patterns.
//
// Parameters:
//   - instruction: the skill instruction text to scan
//
// Returns: *SafetyScanResult
//
// Implementation:
//   Check for each pattern using regex or string matching:
//
//   BLOCKED patterns (skill is rejected):
//     - URLs: regex for http://, https://, ftp:// (except known-safe: docs.go.dev, pkg.go.dev)
//     - Credentials: regex for "api_key", "secret", "password", "token" followed by = or :
//       with a string value (not just the word "token" in context)
//     - Dangerous commands: "rm -rf", "curl", "wget", "nc ", "ncat",
//       "eval(", "exec(", "> /dev/", "chmod 777", "sudo "
//     - Prompt injection: "ignore previous", "ignore all", "you are now",
//       "disregard your", "forget your instructions", "new role", "jailbreak"
//     - Path traversal: "../", absolute paths starting with "/" outside project
//
//   WARNING patterns (skill is stored but flagged):
//     - Instruction length > 2000 tokens (rough estimate: words / 0.75)
//     - Hardcoded file paths (regex for /home/, /usr/, /etc/, C:\)
//     - Hardcoded IP addresses or port numbers
//     - Hardcoded version numbers (may become stale)
func (ss *SafetyScanner) ScanInstruction(instruction string) *SafetyScanResult

// ScanEvolutionOutput checks an Opus-generated evolution response.
// Same checks as ScanInstruction, plus:
//   - Verify the output is valid JSON (for CAPTURE/FIX/DERIVED prompts)
//   - Verify required fields are present (name, trigger_desc, instruction)
func (ss *SafetyScanner) ScanEvolutionOutput(output string) (*SafetyScanResult, error)
```

---

## 14. MCP Tools to Expose

### Tool 1: `find_skill`

```json
{
  "name": "find_skill",
  "description": "Search for a matching skill recipe for the current task. Returns the best matching skill instruction if found. The agent should follow the instruction instead of reasoning from scratch.",
  "input_schema": {
    "type": "object",
    "properties": {
      "task_text": {
        "type": "string",
        "description": "Description of the task to find a skill for."
      },
      "graph_node_ids": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Graph node IDs from the current context."
      },
      "language": {
        "type": "string",
        "description": "Programming language of the current repo."
      }
    },
    "required": ["task_text"]
  }
}
```

### Tool 2: `report_skill_execution`

```json
{
  "name": "report_skill_execution",
  "description": "Report the result of applying a skill. Used to track skill quality and trigger evolution when skills break.",
  "input_schema": {
    "type": "object",
    "properties": {
      "skill_id": {
        "type": "string",
        "description": "The skill that was applied."
      },
      "success": {
        "type": "boolean",
        "description": "Whether the skill application succeeded."
      },
      "error_detail": {
        "type": "string",
        "description": "Error message if the skill failed."
      },
      "tokens_used": {
        "type": "integer",
        "description": "Tokens consumed during execution."
      }
    },
    "required": ["skill_id", "success"]
  }
}
```

### Tool 3: `list_skills`

```json
{
  "name": "list_skills",
  "description": "List all active skills, optionally filtered by graph node or language.",
  "input_schema": {
    "type": "object",
    "properties": {
      "graph_node_id": {
        "type": "string",
        "description": "Filter by skills covering this graph node."
      },
      "language": {
        "type": "string",
        "description": "Filter by language."
      }
    }
  }
}
```

### Tool 4: `get_skill_lineage`

```json
{
  "name": "get_skill_lineage",
  "description": "Get the full evolution history of a skill — all versions from first capture to current.",
  "input_schema": {
    "type": "object",
    "properties": {
      "skill_id": {
        "type": "string",
        "description": "Any version ID — the full lineage is returned."
      }
    },
    "required": ["skill_id"]
  }
}
```

---

## 15. Integration with Other Engines

### 15.1 Engine 1 (Graph) — graph_sync.go

Engine 1 publishes graph change events when code changes (via webhooks).
Engine 3's GraphSync receives these events and flags affected skills.

```go
// In your webhook handler or Redis Stream consumer:
import "universe/internal/skills"

func onGraphChange(changedNodes []string, changeType string, repo string, commit string) {
    event := skills.GraphChangeEvent{
        ChangedNodeIDs: changedNodes,
        ChangeType:     changeType,
        RepoID:         repo,
        CommitHash:     commit,
    }
    graphSync.OnGraphChange(event)
}
```

### 15.2 Engine 2 (Memory) — session events for CAPTURE

Engine 2's session hooks capture events during agent sessions.
When a session ends without a skill match but succeeds, Engine 3 uses those events for CAPTURE.

```go
// In your MCP server's session end handler:
import "universe/internal/skills"

func onSessionEnd(session memory.Session, success bool) {
    if success && !sessionUsedSkill {
        // Convert Engine 2's events to Engine 3's format
        summaries := convertEvents(session.RawEvents)
        
        req := skills.EvolutionRequest{
            AppliedSkill:  nil, // no skill matched
            Execution:     execution,
            SessionEvents: summaries,
            GraphContext:   graphShorthand,
        }
        evolver.OnExecutionComplete(req)
    }
}
```

### 15.3 Engine 4 (Compression) — prompt building

Skills use Engine 4's compression for prompt assembly.

```go
import "universe/internal/compress"

// When applying a skill:
prompt := compress.BuildPrompt(taskPrompt, compress.PromptConfig{
    Level:        compress.LevelCompact,
    GraphContext: graphNodeInfos,
})
// Prepend the skill instruction to the compressed prompt
```

### 15.4 Engine 5 (Orchestrator) — LLMClient interface

Engine 5 provides the LLMClient interface that the evolver uses to call Opus.
This breaks the circular dependency.

```go
// In Engine 5's orchestrator setup:
import "universe/internal/skills"

// Adapter that satisfies skills.LLMClient using the orchestrator's API client
type evolverLLMAdapter struct {
    client *orchestrator.LLMClient
}

func (a *evolverLLMAdapter) CallOpus(system, user string, maxTokens int) (string, error) {
    resp, err := a.client.Call(orchestrator.Opus, system, user, maxTokens)
    if err != nil { return "", err }
    return resp.Content, nil
}

// Wire it up:
evolver := skills.NewEvolver(store, embedder, &evolverLLMAdapter{client}, safety, config)
```

### 15.5 Go module dependencies

```bash
# Same as Engine 2 — no additional dependencies needed
# pgx, pgvector-go, and uuid are already installed
```

---

## 16. Build Order Within Engine 3

Build the files in this order — each step is testable independently:

1. **types.go** — all types first (no dependencies)
2. **003_skills_tables.sql** — run migration
3. **store.go** — database CRUD (test with direct SQL)
4. **safety.go** — safety scanner (test with pattern strings, no DB needed)
5. **matcher.go** — skill search (needs store + embedder)
6. **executor.go** — skill application (needs store)
7. **monitor.go** — quality tracking (needs store)
8. **graph_sync.go** — graph change handler (needs store)
9. **evolver.go** — evolution logic (needs store + embedder + LLM + safety)

Start with CAPTURED mode only in evolver.go. Add FIX after you have ~20 skills. Add DERIVED last.

---

## 17. Testing Strategy

Create `internal/skills/skills_test.go`:

```go
package skills

import "testing"

// ============================================================
// STORE TESTS
// ============================================================

// Test 1: InsertSkill and GetByID round-trip
func TestStore_InsertAndGet(t *testing.T) {}

// Test 2: GetByGraphNodes returns matching skills
func TestStore_GetByGraphNodes(t *testing.T) {
    // Insert skill covering ["auth:validate", "gateway:login"]
    // Query with ["auth:validate"] → should find it
    // Query with ["other:function"] → should not find it
}

// Test 3: GetByGraphNodes respects language filter
func TestStore_GraphNodesLanguageFilter(t *testing.T) {
    // Insert Go skill and Python skill for same graph node
    // Query with language="go" → only Go skill returned
}

// Test 4: SearchKeyword finds by trigger description
func TestStore_SearchKeyword(t *testing.T) {}

// Test 5: GetLineage returns full evolution chain
func TestStore_GetLineage(t *testing.T) {
    // Insert v1 (captured), v2 (fix of v1), v3 (fix of v2)
    // GetLineage(v3.ID) → returns [v1, v2, v3] in order
}

// Test 6: CountSkillsForGraphNode respects limit
func TestStore_CountSkillsPerNode(t *testing.T) {}

// Test 7: PruneUnusedSkills deletes old unused skills
func TestStore_PruneUnused(t *testing.T) {
    // Insert skill with times_applied=0, created 60 days ago
    // PruneUnusedSkills(30) → should delete it
}

// Test 8: MarkGraphNodesStale adds negative tag
func TestStore_MarkStale(t *testing.T) {
    // Insert skill covering node A
    // MarkGraphNodesStale(["A"])
    // Reload skill → negative_tags should contain "graph_changed"
}

// ============================================================
// MATCHER TESTS
// ============================================================

// Test 9: Match finds skill by graph node overlap
func TestMatcher_GraphOverlap(t *testing.T) {}

// Test 10: Match respects exploration rate
func TestMatcher_ExplorationRate(t *testing.T) {
    // Set exploration rate to 1.0 (always explore)
    // Match should return ExplorationTriggered=true, BestMatch=nil
}

// Test 11: Match skips skills with negative tags matching context
func TestMatcher_NegativeTagFilter(t *testing.T) {
    // Skill has negative tag {"context": "python repo"}
    // Query with language="python" → skill should be skipped
}

// Test 12: Match prefers complex-success-rate for complex tasks
func TestMatcher_ComplexityWeighting(t *testing.T) {
    // Skill A: 95% simple success, 30% complex success
    // Skill B: 80% simple success, 80% complex success
    // Query with complexity="complex" → Skill B should rank higher
}

// Test 13: Match reduces score for stale skills (graph_changed tag)
func TestMatcher_StaleSkillPenalty(t *testing.T) {}

// Test 14: CalculateGraphOverlap computes correctly
func TestCalculateGraphOverlap(t *testing.T) {
    // skill covers ["a", "b", "c"], query has ["a", "b"] → 1.0 (all query nodes covered)
    // skill covers ["a"], query has ["a", "b"] → 0.5 (half covered)
    // skill covers ["x"], query has ["a", "b"] → 0.0 (no overlap)
}

// ============================================================
// SAFETY TESTS
// ============================================================

// Test 15: ScanInstruction blocks URLs
func TestSafety_BlocksURLs(t *testing.T) {
    // Instruction contains "curl https://evil.com"
    // Should return Safe=false, Blocked=["external URL detected"]
}

// Test 16: ScanInstruction blocks prompt injection
func TestSafety_BlocksInjection(t *testing.T) {
    // Instruction contains "ignore previous instructions"
    // Should return Safe=false
}

// Test 17: ScanInstruction allows safe instructions
func TestSafety_AllowsSafe(t *testing.T) {
    // Normal instruction about fixing types
    // Should return Safe=true
}

// Test 18: ScanInstruction warns on long instructions
func TestSafety_WarnsLong(t *testing.T) {
    // Instruction over 2000 estimated tokens
    // Should return Safe=true, Warnings=["instruction is very long"]
}

// ============================================================
// EVOLVER TESTS
// ============================================================

// Test 19: CAPTURED mode extracts skill from successful session
func TestEvolver_Capture(t *testing.T) {
    // Mock successful session with events
    // Mock Opus returning valid skill JSON
    // Verify: new skill created with evolution="captured"
}

// Test 20: CAPTURED mode skips if similar skill exists
func TestEvolver_CaptureSkipsDuplicate(t *testing.T) {
    // Insert existing skill with similar embedding
    // Try capture → should skip
}

// Test 21: FIX mode creates new version after 3 failures
func TestEvolver_Fix(t *testing.T) {
    // Insert skill, record 3 consecutive failures
    // Mock Opus returning fixed instruction
    // Verify: new version created, old version deactivated
}

// Test 22: FIX mode freezes after max daily attempts
func TestEvolver_FixFreezesOnLimit(t *testing.T) {
    // Set max_attempts_per_day = 3
    // Trigger 3 FIX attempts
    // 4th attempt → skill should be frozen
}

// Test 23: FIX mode validates against test case
func TestEvolver_FixValidation(t *testing.T) {
    // Skill has a test case
    // Opus produces a fix that fails the test case
    // Verify: fix is rejected, old version stays active
}

// ============================================================
// GRAPH SYNC TESTS
// ============================================================

// Test 24: OnGraphChange flags skills as stale
func TestGraphSync_FlagsStale(t *testing.T) {
    // Insert skill covering node A
    // Fire graph change event for node A
    // Verify: skill has "graph_changed" negative tag
}

// Test 25: OnGraphChange handles node rename
func TestGraphSync_Rename(t *testing.T) {
    // Insert skill covering "auth:OldName"
    // Fire rename event: "auth:OldName" → "auth:NewName"
    // Verify: skill's graph_node_ids updated to include new name
}

// ============================================================
// MONITOR TESTS
// ============================================================

// Test 26: CheckSkillHealth triggers FIX on consecutive failures
func TestMonitor_TriggersFixOnFailures(t *testing.T) {}

// Test 27: RunDailyMaintenance resets counters and prunes
func TestMonitor_DailyMaintenance(t *testing.T) {}

// ============================================================
// EXECUTOR TESTS
// ============================================================

// Test 28: Apply builds correct prompt with skill instruction
func TestExecutor_Apply(t *testing.T) {
    // Verify prompt contains skill instruction + graph context + task
}

// Test 29: RecordExecution updates confidence on success
func TestExecutor_ConfidenceGrowth(t *testing.T) {
    // New skill starts at 0.5
    // 5 successful executions → confidence should be 1.0
}

// Test 30: RecordExecution adds negative tag on context-specific failure
func TestExecutor_NegativeTagOnFailure(t *testing.T) {}
```

### How to run tests

```bash
# Set up test database
createdb universe_test
psql universe_test -c "CREATE EXTENSION vector;"
psql universe_test -f migrations/002_memory_tables.sql
psql universe_test -f migrations/003_skills_tables.sql

# Run tests
DATABASE_URL="postgres://localhost:5432/universe_test?sslmode=disable" \
  go test ./internal/skills/ -v
```

---

## 18. Acceptance Criteria

Engine 3 is complete when:

- [ ] PostgreSQL migration runs successfully (skills + skill_executions + all indexes)
- [ ] `store.InsertSkill()` stores and retrieves skills correctly
- [ ] `store.GetByGraphNodes()` finds skills by graph node overlap with language filtering
- [ ] `store.GetLineage()` returns full version DAG via recursive CTE
- [ ] `matcher.Match()` performs 3-way hybrid search with weighted scoring
- [ ] `matcher.Match()` respects exploration rate (10% skip)
- [ ] `matcher.Match()` filters by negative tags, language, confidence, and complexity
- [ ] `executor.Apply()` builds correct prompt with skill instruction injected
- [ ] `executor.RecordExecution()` updates metrics and confidence
- [ ] `evolver.tryCaptureSkill()` extracts new skills from successful sessions
- [ ] `evolver.tryCaptureSkill()` skips duplicates (similarity > 0.85)
- [ ] `evolver.tryFixSkill()` creates new version with deactivation of old
- [ ] `evolver.tryFixSkill()` validates against test case before activation
- [ ] `evolver.tryFixSkill()` freezes skill after max daily attempts
- [ ] `safety.ScanInstruction()` blocks dangerous patterns (URLs, injection, credentials)
- [ ] `graph_sync.OnGraphChange()` flags affected skills with "graph_changed" tag
- [ ] `graph_sync.OnGraphChange()` handles renames and deletions
- [ ] `monitor.CheckSkillHealth()` triggers FIX on consecutive failures
- [ ] `monitor.RunDailyMaintenance()` prunes unused skills and resets counters
- [ ] All 30 tests pass
- [ ] `go build ./...` succeeds
- [ ] No changes to existing Engine 1, 2, or 4 files

---

## 19. What NOT to Build in This Engine

- Do NOT modify Engine 1, 2, or 4 code — use interfaces only
- Do NOT build the MCP server — that's a separate integration spec
- Do NOT build a cloud skill marketplace — team sharing via DB is enough for V1
- Do NOT build a skill visualization UI — the version DAG can be queried via SQL
- Do NOT implement DERIVED mode initially — start with CAPTURED + FIX only
- Do NOT build real-time streaming of skill evolution — batch is fine for V1
- Do NOT add Redis — all storage is PostgreSQL

---

## 20. Seed Skills for Cold Start

To avoid the cold start problem (Month 1: no skills = no benefit), manually create 5-10 seed skills for common patterns your team encounters:

```sql
INSERT INTO skills (name, version, evolution, graph_node_ids, language, trigger_desc, instruction, confidence, shared, is_active, created_by)
VALUES
('type-mismatch-fix', 1, 'manual', '{}', 'go',
 'When there is a type mismatch between two functions or repos in an API contract',
 'Step 1: Identify which function has the wrong type by checking the API contract.\nStep 2: Check which callers pass the wrong type.\nStep 3: Change the parameter type to match the contract.\nStep 4: Update all callers to pass the correct type.\nStep 5: Run tests to verify.',
 0.8, true, true, 'system'),

('add-unit-test', 1, 'manual', '{}', 'go',
 'When a function needs a unit test added',
 'Step 1: Read the function signature and understand what it does.\nStep 2: Identify edge cases: nil input, empty input, valid input, invalid input.\nStep 3: Create test file with _test.go suffix.\nStep 4: Write table-driven tests with descriptive names.\nStep 5: Run go test and verify all pass.',
 0.8, true, true, 'system');
```

These seed skills have empty `graph_node_ids` — they match by keyword/semantic search only. As they get applied to specific functions, the evolver can DERIVE graph-tagged variants.

---

## 21. Future Improvements (Not in This Build)

1. **DERIVED mode** — add after CAPTURED + FIX are stable with ~20 skills
2. **Skill visualization UI** — tree diagram showing the version DAG with success rates per version
3. **Skill merge** — when two similar skills exist, merge them into one better skill
4. **Cross-team skill sharing** — share skills between different teams (not just within a team)
5. **Skill templates** — pre-built skill structures for common patterns (Go error handling, Python type checking, etc.)
6. **Skill performance benchmarks** — periodic A/B testing of skills vs from-scratch reasoning
7. **Natural language skill editing** — developers describe changes in English, Opus updates the instruction
8. **Skill dependency tracking** — skills that depend on other skills (skill A says "first run skill B")
