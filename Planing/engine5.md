# Engine 5 — Tiered Agent Orchestration

## Build Specification for Claude Code

**Engine name:** Tiered Agent Orchestration (LLM Cascading + Planner-Executor Architecture)  
**Concept:** FrugalGPT model routing + hierarchical task decomposition + speculative execution with verification  
**Estimated effort:** 3-4 days  
**Dependencies:** Engine 1 (Graph) — built. Engine 2 (Memory) — built. Engine 3 (Skills) — built. Engine 4 (Compression) — built.  
**Database required:** PostgreSQL (add cost tracking table to existing DB)  
**New services required:** None — adds to existing Go binary  

---

## 1. What This Engine Does (Plain English)

You have two AI workers. One is expensive and smart (Opus — $15/M input tokens, $75/M output tokens). The other is cheap and fast (Haiku — $0.25/M input tokens, $1.25/M output tokens). That's a 60× cost difference.

Engine 5 is the boss that decides who does what:
- The smart one (Opus) looks at the problem, writes a plan, and checks the result
- The cheap one (Haiku) follows the plan and does the actual work
- If a skill recipe exists (Engine 3), Haiku follows the recipe — Opus doesn't even need to plan
- If memory has a past solution (Engine 2), Haiku applies it directly

The routing decision costs ZERO tokens — it's a database query using the graph, memory, and skills. No AI call needed to decide which AI to call.

Over time, as memory grows and skills evolve, more tasks go to Haiku. Month 1: 60% Opus. Month 6: 10% Opus. The cost curve drops automatically.

**Expected cost reduction:** 87-92% compared to routing everything through Opus.

---

## 2. Current Project Structure

Engine 5 adds an `orchestrator` package under `internal/`. This is the last engine — it imports all four previous engines.

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
│   ├── skills/          ← Engine 3
│   └── orchestrator/    ← NEW (Engine 5)
├── migrations/
│   ├── 002_memory_tables.sql
│   ├── 003_skills_tables.sql
│   └── 004_cost_tracking.sql    ← NEW
├── go.mod
└── go.sum
```

---

## 3. New Files to Create

```
internal/
└── orchestrator/
    ├── types.go          # All types, configs, constants
    ├── router.go         # Routing decision logic (zero LLM tokens)
    ├── planner.go        # Opus: break task into sub-tasks with structured specs
    ├── executor.go       # Haiku: execute sub-tasks from specs
    ├── verifier.go       # Three-tier verification system
    ├── escalation.go     # 3-step failure escalation chain
    ├── parallel.go       # Parallel sub-task execution with dependency tracking
    ├── llmclient.go      # Anthropic API client for Opus and Haiku
    ├── tracker.go        # Cost tracking per task, developer, month
    ├── templates.go      # Structured spec templates for each task type
    └── orchestrator_test.go  # All tests

migrations/
└── 004_cost_tracking.sql     # Cost tracking table + dashboard views
```

---

## 4. Database Migration: `migrations/004_cost_tracking.sql`

```sql
-- ============================================================
-- Engine 5: Tiered Agent Orchestration — Cost Tracking
-- Migration: 004_cost_tracking.sql
-- ============================================================

-- Tracks every LLM call made by the orchestrator.
-- This is the data source for the manager dashboard.
CREATE TABLE IF NOT EXISTS agent_costs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Which task triggered this LLM call
    task_id         TEXT NOT NULL,

    -- Which developer's request
    developer_id    TEXT NOT NULL,

    -- Which model was used
    -- 'opus' = Claude Opus (premium)
    -- 'haiku' = Claude Haiku (low-cost)
    model           TEXT NOT NULL CHECK (model IN ('opus', 'haiku')),

    -- Token counts
    input_tokens    INT NOT NULL,
    output_tokens   INT NOT NULL,

    -- Calculated cost in USD
    -- Opus:  input = $15/M, output = $75/M
    -- Haiku: input = $0.25/M, output = $1.25/M
    cost_usd        FLOAT NOT NULL,

    -- What phase of the orchestration this call was for
    -- 'plan'     = Opus planning step
    -- 'execute'  = Haiku execution step
    -- 'verify'   = Opus verification step
    -- 'escalate' = Opus rephrase during escalation
    -- 'takeover' = Opus took over from failed Haiku
    -- 'direct'   = single model, no split (skill_execute or memory_apply)
    phase           TEXT NOT NULL CHECK (phase IN (
                        'plan', 'execute', 'verify', 'escalate', 'takeover', 'direct'
                    )),

    -- Which routing mode was used
    -- 'skill_execute'      = Haiku followed a skill recipe (cheapest)
    -- 'memory_apply'       = Haiku applied a known solution
    -- 'plan_execute'       = Opus planned, Haiku executed
    -- 'full_orchestration' = Full plan + execute + verify cycle
    -- 'single_opus'        = Opus handled the whole task (no split)
    -- 'single_haiku'       = Haiku handled the whole task (simple)
    routing_mode    TEXT NOT NULL,

    -- Was a skill used? (links to skills table)
    skill_id        UUID,

    -- Was memory recalled? (true if memory contributed to the solution)
    memory_hit      BOOLEAN NOT NULL DEFAULT false,

    -- How many escalation steps were needed (0 = no escalation)
    escalation_steps INT NOT NULL DEFAULT 0,

    -- Was this a takeover? (Opus took over from failed Haiku)
    was_takeover    BOOLEAN NOT NULL DEFAULT false,

    -- Latency in milliseconds
    latency_ms      INT,

    -- Repository context
    repo_id         TEXT,

    -- Timestamp
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for dashboard queries
CREATE INDEX idx_costs_developer ON agent_costs(developer_id);
CREATE INDEX idx_costs_model ON agent_costs(model);
CREATE INDEX idx_costs_routing ON agent_costs(routing_mode);
CREATE INDEX idx_costs_created ON agent_costs(created_at);
CREATE INDEX idx_costs_task ON agent_costs(task_id);

-- ============================================================
-- MATERIALIZED VIEWS for the manager dashboard
-- Refresh these periodically (e.g., every hour via cron)
-- ============================================================

-- Monthly cost summary — the headline number for managers
CREATE MATERIALIZED VIEW monthly_cost_summary AS
SELECT
    date_trunc('month', created_at)   AS month,
    COUNT(*)                           AS total_calls,
    SUM(cost_usd)                      AS actual_cost,
    -- What it WOULD have cost if everything went through Opus
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    )                                  AS would_have_cost_all_opus,
    -- Savings
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    ) - SUM(cost_usd)                  AS savings_usd,
    -- Routing breakdown
    COUNT(*) FILTER (WHERE routing_mode = 'skill_execute')      AS skill_executions,
    COUNT(*) FILTER (WHERE routing_mode = 'memory_apply')       AS memory_applies,
    COUNT(*) FILTER (WHERE routing_mode = 'plan_execute')       AS plan_executes,
    COUNT(*) FILTER (WHERE routing_mode = 'full_orchestration') AS full_orchestrations,
    COUNT(*) FILTER (WHERE routing_mode = 'single_opus')        AS single_opus,
    COUNT(*) FILTER (WHERE was_takeover)                        AS takeovers,
    COUNT(*) FILTER (WHERE memory_hit)                          AS memory_hits,
    COUNT(*) FILTER (WHERE skill_id IS NOT NULL)                AS skill_uses,
    -- Model split
    SUM(cost_usd) FILTER (WHERE model = 'opus')  AS opus_cost,
    SUM(cost_usd) FILTER (WHERE model = 'haiku') AS haiku_cost,
    -- Average latency
    AVG(latency_ms)                    AS avg_latency_ms
FROM agent_costs
GROUP BY 1;

-- Per-developer cost summary
CREATE MATERIALIZED VIEW developer_cost_summary AS
SELECT
    developer_id,
    date_trunc('week', created_at)    AS week,
    COUNT(*)                           AS total_calls,
    SUM(cost_usd)                      AS actual_cost,
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    ) - SUM(cost_usd)                  AS savings_usd,
    COUNT(*) FILTER (WHERE was_takeover) AS takeovers
FROM agent_costs
GROUP BY 1, 2;

-- Routing mode effectiveness — which modes are most cost-effective
CREATE MATERIALIZED VIEW routing_effectiveness AS
SELECT
    routing_mode,
    COUNT(*)                           AS total_uses,
    AVG(cost_usd)                      AS avg_cost,
    AVG(latency_ms)                    AS avg_latency,
    AVG(escalation_steps)              AS avg_escalations,
    COUNT(*) FILTER (WHERE was_takeover) AS takeovers,
    SUM(cost_usd)                      AS total_cost
FROM agent_costs
GROUP BY 1;

-- Refresh command (run via cron or background goroutine every hour)
-- REFRESH MATERIALIZED VIEW monthly_cost_summary;
-- REFRESH MATERIALIZED VIEW developer_cost_summary;
-- REFRESH MATERIALIZED VIEW routing_effectiveness;
```

---

## 5. File: `internal/orchestrator/types.go`

All types, constants, and configuration.

```go
package orchestrator

import "time"

// ============================================================
// MODELS — which AI to use
// ============================================================

type ModelTier string

const (
    Opus  ModelTier = "opus"   // Premium: $15/M input, $75/M output
    Haiku ModelTier = "haiku"  // Low-cost: $0.25/M input, $1.25/M output
)

// ModelConfig holds API details for each model tier.
type ModelConfig struct {
    Tier            ModelTier
    ModelID         string  // e.g., "claude-opus-4-20250514", "claude-3-5-haiku-20241022"
    APIKey          string
    Endpoint        string  // default: "https://api.anthropic.com/v1/messages"
    MaxTokens       int     // max output tokens per call
    InputCostPerM   float64 // cost per 1M input tokens in USD
    OutputCostPerM  float64 // cost per 1M output tokens in USD
    MaxConcurrent   int     // max parallel calls to this model
}

// ============================================================
// ROUTING — how the orchestrator decides what to do
// ============================================================

// RoutingMode describes how a task is handled.
type RoutingMode string

const (
    // SkillExecute — a skill recipe was found. Haiku follows it.
    // No Opus planning needed. Cheapest mode.
    ModeSkillExecute RoutingMode = "skill_execute"

    // MemoryApply — memory has a past solution. Haiku applies it.
    // No Opus planning needed. Cheap mode.
    ModeMemoryApply RoutingMode = "memory_apply"

    // PlanExecute — Opus writes a brief plan, Haiku executes.
    // Standard mode for known task types with templates.
    ModePlanExecute RoutingMode = "plan_execute"

    // FullOrchestration — Full cycle: Opus plan + Haiku execute + Opus verify.
    // For complex, cross-repo, or novel tasks.
    ModeFullOrchestration RoutingMode = "full_orchestration"

    // SingleOpus — Opus handles the entire task alone.
    // For tasks that don't fit any template, or open-ended requests.
    ModeSingleOpus RoutingMode = "single_opus"

    // SingleHaiku — Haiku handles the entire task alone.
    // For very simple, small-scope tasks.
    ModeSingleHaiku RoutingMode = "single_haiku"
)

// RoutingDecision is the output of the router.
// Tells the orchestrator exactly how to handle the task.
type RoutingDecision struct {
    Mode            RoutingMode
    PlannerModel    ModelTier     // which model plans (if applicable)
    ExecutorModel   ModelTier     // which model executes
    VerifyTier      VerifyTier    // how to verify the output
    SkipPlanning    bool          // true for skill_execute and memory_apply
    MatchedSkillID  string        // skill ID if skill_execute mode
    MemoryHit       bool          // true if memory contributed
    TemplateID      string        // which spec template to use (if plan_execute)
    Reason          string        // human-readable reason for this routing decision
}

// VerifyTier controls how the output is verified.
type VerifyTier int

const (
    // VerifyAutomated — run tests, lint, syntax check. Zero Opus tokens.
    // Used for: code changes, config changes, formatting, test generation.
    VerifyAutomated VerifyTier = 1

    // VerifySpotCheck — Opus does a structural comparison (50-100 tokens).
    // Checks: does output match the spec's key fields?
    // Used for: PR descriptions, commit messages, impact analysis.
    VerifySpotCheck VerifyTier = 2

    // VerifyFullReview — Opus reads and judges the full output (300-500 tokens).
    // Used for: architecture decisions, cross-repo changes, security-sensitive code,
    //           first-time skill applications.
    VerifyFullReview VerifyTier = 3
)

// ============================================================
// TASK — what the developer asked for
// ============================================================

// Task represents a developer's request.
type Task struct {
    ID            string    `json:"id"`
    DeveloperID   string    `json:"developer_id"`
    RepoID        string    `json:"repo_id"`
    Prompt        string    `json:"prompt"`         // the developer's request text
    GraphNodeIDs  []string  `json:"graph_node_ids"` // affected graph nodes (from context)
    TaskType      TaskType  `json:"task_type"`       // classified task type
    CreatedAt     time.Time `json:"created_at"`
}

// TaskType categorizes what the developer wants.
// Determines which spec template to use.
type TaskType string

const (
    TaskCodeFix       TaskType = "code_fix"
    TaskTestGen       TaskType = "test_gen"
    TaskPRGen         TaskType = "pr_gen"
    TaskRefactor      TaskType = "refactor"
    TaskDepUpdate     TaskType = "dependency_update"
    TaskConfigChange  TaskType = "config_change"
    TaskMigration     TaskType = "migration"
    TaskAnalysis      TaskType = "analysis"
    TaskExplanation   TaskType = "explanation"
    TaskGeneral       TaskType = "general"  // doesn't fit any template
)

// ============================================================
// PLAN — what Opus produces
// ============================================================

// Plan is the structured specification Opus generates.
// Contains one or more sub-tasks, each with a specific template.
type Plan struct {
    TaskID     string     `json:"task_id"`
    SubTasks   []SubTask  `json:"sub_tasks"`
    PRTitle    string     `json:"pr_title,omitempty"`
    PRBody     string     `json:"pr_body,omitempty"`
    TestCmd    string     `json:"test_command,omitempty"`
}

// SubTask is one unit of work within a plan.
// Haiku executes one sub-task at a time.
type SubTask struct {
    ID          string      `json:"id"`          // unique within the plan
    Action      string      `json:"action"`      // "modify_file", "create_file", "run_test", "generate_test", "generate_pr"
    DependsOn   []string    `json:"depends_on"`  // IDs of sub-tasks that must complete first (empty = independent)
    Spec        interface{} `json:"spec"`         // template-specific spec (see templates.go)
    VerifyTier  VerifyTier  `json:"verify_tier"`  // how to verify this sub-task's output
}

// SubTaskResult is what the executor returns for each sub-task.
type SubTaskResult struct {
    SubTaskID    string `json:"sub_task_id"`
    Success      bool   `json:"success"`
    Output       string `json:"output"`       // the generated code, text, or structured output
    ErrorMessage string `json:"error,omitempty"`
    TokensUsed   int    `json:"tokens_used"`
    LatencyMs    int    `json:"latency_ms"`
    Model        ModelTier `json:"model"`     // which model actually handled this
}

// ============================================================
// ESCALATION — handling failures
// ============================================================

// EscalationStep tracks one step in the escalation chain.
type EscalationStep struct {
    Step    int       `json:"step"`     // 1 = retry with error, 2 = opus rephrase, 3 = opus takeover
    Action  string    `json:"action"`   // "retry_with_error", "opus_rephrase", "opus_takeover"
    Error   string    `json:"error"`    // the error from the previous attempt
    Success bool      `json:"success"`  // did this escalation step succeed?
}

// ============================================================
// COST TRACKING
// ============================================================

// CostRecord is one row in the agent_costs table.
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
// CONFIGURATION
// ============================================================

type Config struct {
    // Model configurations
    OpusConfig  ModelConfig
    HaikuConfig ModelConfig

    // Routing thresholds
    SkillMatchMinSuccessRate float64 // Minimum success rate for skill-based routing. Default: 0.85
    MemoryMatchMinConfidence float64 // Minimum confidence for memory-based routing. Default: 0.80
    SimpleTaskMaxNodes       int     // Max affected nodes for "simple" classification. Default: 3
    MinTaskSizeForSplit      int     // Below this token estimate, don't split. Default: 300

    // Escalation settings
    MaxHaikuRetries      int // Max Haiku attempts before Opus rephrase. Default: 2
    MaxTotalAttempts     int // Max total attempts before Opus takeover. Default: 3

    // Verification settings
    AsyncVerifyForInteractive bool // Verify async for interactive requests. Default: true

    // Parallel execution
    MaxParallelHaikuCalls int // Max Haiku calls running simultaneously. Default: 5

    // Rate limiting
    OpusCallsPerMinute  int // Max Opus API calls per minute. Default: 30
    HaikuCallsPerMinute int // Max Haiku API calls per minute. Default: 100
    RetryBackoffBase    int // Base backoff in milliseconds. Default: 1000

    // Cost tracking
    DatabaseURL string // PostgreSQL connection string
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
            ModelID:        "claude-3-5-haiku-20241022",
            Endpoint:       "https://api.anthropic.com/v1/messages",
            MaxTokens:      4096,
            InputCostPerM:  0.25,
            OutputCostPerM: 1.25,
            MaxConcurrent:  10,
        },
        SkillMatchMinSuccessRate: 0.85,
        MemoryMatchMinConfidence: 0.80,
        SimpleTaskMaxNodes:       3,
        MinTaskSizeForSplit:      300,
        MaxHaikuRetries:          2,
        MaxTotalAttempts:         3,
        AsyncVerifyForInteractive: true,
        MaxParallelHaikuCalls:    5,
        OpusCallsPerMinute:       30,
        HaikuCallsPerMinute:      100,
        RetryBackoffBase:         1000,
    }
}
```

---

## 6. File: `internal/orchestrator/templates.go`

Structured spec templates that constrain what Opus produces. Each task type has a fixed JSON structure — Opus fills the fields, Haiku executes the fields.

```go
package orchestrator

// ============================================================
// SPEC TEMPLATES — one per task type
// ============================================================
// Each template defines the exact JSON structure Opus must produce.
// This prevents Opus from rambling and ensures Haiku can execute
// without interpretation.
//
// ARCHITECT'S RULE: If you can't write a template for it, don't split it.

// CodeFixSpec — template for code fix tasks.
// Opus fills this; Haiku applies each change.
type CodeFixSpec struct {
    Changes []CodeChange `json:"changes"`
}

type CodeChange struct {
    File        string `json:"file"`         // relative path: "internal/auth/validate.go"
    LineRange   [2]int `json:"line_range"`   // [start, end] line numbers to modify
    CurrentCode string `json:"current_code"` // exact code currently at that location
    TargetCode  string `json:"target_code"`  // exact replacement code
    Reason      string `json:"reason"`       // one sentence: why this change (for Haiku's context)
}

// TestGenSpec — template for test generation tasks.
type TestGenSpec struct {
    Tests []TestCase `json:"tests"`
}

type TestCase struct {
    File         string `json:"file"`          // "internal/auth/validate_test.go"
    FunctionName string `json:"function_name"` // "TestValidateToken_StringInput"
    Description  string `json:"description"`   // what this test verifies
    Setup        string `json:"setup"`         // any setup code needed
    Input        string `json:"input"`         // test input
    Expected     string `json:"expected"`      // expected output/behavior
    CoversNode   string `json:"covers_node"`   // graph node ID this test covers
}

// PRGenSpec — template for PR description generation.
type PRGenSpec struct {
    Title         string   `json:"title"`
    ChangeSummary string   `json:"change_summary"`  // 2-3 sentences
    AffectedFiles []string `json:"affected_files"`
    AffectedNodes []string `json:"affected_nodes"`  // graph node IDs
    TestingNotes  string   `json:"testing_notes"`
    Labels        []string `json:"labels"`
}

// RefactorSpec — template for refactoring tasks.
type RefactorSpec struct {
    Objective  string       `json:"objective"`   // one sentence: what the refactor achieves
    Changes    []CodeChange `json:"changes"`     // same as CodeFixSpec changes
    MoveFiles  []FileMove   `json:"move_files"`  // any files to rename/move
}

type FileMove struct {
    From string `json:"from"`
    To   string `json:"to"`
}

// DepUpdateSpec — template for dependency update tasks.
type DepUpdateSpec struct {
    Dependency    string       `json:"dependency"`     // "github.com/some/package"
    FromVersion   string       `json:"from_version"`
    ToVersion     string       `json:"to_version"`
    CodeChanges   []CodeChange `json:"code_changes"`   // breaking change fixes
    ConfigChanges []CodeChange `json:"config_changes"`  // go.mod, go.sum, etc.
}

// ConfigChangeSpec — template for configuration changes.
type ConfigChangeSpec struct {
    Changes []ConfigEntry `json:"changes"`
}

type ConfigEntry struct {
    File     string `json:"file"`      // config file path
    Key      string `json:"key"`       // config key or path (e.g., "database.host")
    OldValue string `json:"old_value"`
    NewValue string `json:"new_value"`
    Format   string `json:"format"`    // "yaml", "json", "toml", "env"
}

// AnalysisSpec — template for impact analysis tasks.
// NOTE: Analysis output is also structured — not free-form prose.
type AnalysisSpec struct {
    Question      string `json:"question"`       // what to analyze
    Scope         string `json:"scope"`          // "function", "package", "repo", "cross-repo"
    AffectedNodes []struct {
        NodeID string `json:"node_id"`
        Impact string `json:"impact"`   // "high", "medium", "low"
        Reason string `json:"reason"`
    } `json:"affected_nodes"`
    RiskLevel     string `json:"risk_level"`     // "high", "medium", "low"
    Recommendation string `json:"recommendation"` // one sentence
}

// ============================================================
// TEMPLATE REGISTRY — maps task types to templates
// ============================================================

// HasTemplate returns true if a structured spec template exists for this task type.
// Tasks without templates are routed to SingleOpus mode (no split).
func HasTemplate(taskType TaskType) bool {
    switch taskType {
    case TaskCodeFix, TaskTestGen, TaskPRGen, TaskRefactor,
         TaskDepUpdate, TaskConfigChange, TaskAnalysis:
        return true
    case TaskExplanation, TaskGeneral, TaskMigration:
        return false // open-ended — don't split
    default:
        return false
    }
}

// GetTemplateName returns the template identifier for prompt construction.
func GetTemplateName(taskType TaskType) string {
    // Returns: "code_fix", "test_gen", "pr_gen", etc.
    return string(taskType)
}

// GetPlannerPrompt returns the system prompt for Opus when creating a plan.
// This prompt instructs Opus to output JSON matching the specific template.
//
// Parameters:
//   - taskType: which template to use
//   - graphContext: compressed graph shorthand from Engine 4
//   - memoryContext: relevant observations from Engine 2 (if any)
//   - skillContext: matched skill instruction from Engine 3 (if any, for reference)
//
// Returns: the complete system prompt for the planner
//
// The prompt structure:
//   1. Role instruction: "You are a task planner. Output ONLY valid JSON."
//   2. Template schema: the exact JSON structure to fill
//   3. Template example: a concrete filled example
//   4. Graph context: compressed shorthand from Engine 4
//   5. Memory context: relevant past observations (if any)
//   6. Constraints: max sub-tasks, field requirements, etc.
//
// Example system prompt for code_fix:
//
//   You are a task planner. Analyze the developer's request and produce
//   a structured specification.
//
//   OUTPUT FORMAT — respond with ONLY valid JSON matching this schema:
//   {
//     "sub_tasks": [
//       {
//         "id": "unique_id",
//         "action": "modify_file",
//         "depends_on": [],
//         "spec": {
//           "changes": [
//             {
//               "file": "path/to/file.go",
//               "line_range": [40, 45],
//               "current_code": "exact current code",
//               "target_code": "exact replacement code",
//               "reason": "why"
//             }
//           ]
//         },
//         "verify_tier": 1
//       }
//     ],
//     "test_command": "go test ./...",
//     "pr_title": "fix: description"
//   }
//
//   GRAPH CONTEXT:
//   [graph shorthand from Engine 4]
//
//   MEMORY CONTEXT:
//   [relevant observations from Engine 2, if any]
//
//   RULES:
//   - current_code MUST match exactly what is in the file
//   - target_code MUST be complete, compilable code
//   - reason MUST be one sentence
//   - Max 5 sub-tasks per plan
//   - Set verify_tier: 1 for code changes, 2 for PR/docs, 3 for cross-repo
//   - Set depends_on when order matters (e.g., tests depend on code changes)
//   - Independent sub-tasks should have empty depends_on (enables parallel execution)
func GetPlannerPrompt(taskType TaskType, graphContext string, memoryContext string, skillContext string) string
```

---

## 7. File: `internal/orchestrator/router.go`

The routing decision logic. Costs ZERO tokens — all decisions are database queries.

```go
package orchestrator

// Router decides how to handle each task using structural signals.
// NO LLM calls are made during routing. All signals come from:
//   - Engine 3 (skills): is there a matching recipe?
//   - Engine 2 (memory): has this been solved before?
//   - Engine 1 (graph): how complex is the scope?
//   - Templates: does a spec template exist for this task type?

// Interfaces for the engines this router depends on.
// These break the circular dependency between engines.

// SkillMatcher finds the best skill for a task.
// Implemented by Engine 3's skills/matcher.go.
type SkillMatcher interface {
    Match(graphNodeIDs []string, taskText string) (*SkillMatch, error)
}

type SkillMatch struct {
    SkillID     string
    Name        string
    Instruction string
    SuccessRate float64
    GraphOverlap float64 // 0.0-1.0: how many of the task's graph nodes this skill covers
}

// MemoryRecaller retrieves relevant past observations.
// Implemented by Engine 2's memory/retriever.go.
type MemoryRecaller interface {
    QuickCheck(graphNodeIDs []string, developerID string) (*MemoryCheck, error)
}

type MemoryCheck struct {
    HasExactMatch bool    // memory for the exact same graph node + same category
    Confidence    float64 // highest confidence among matches
    Summary       string  // top observation summary (for context injection)
}

// GraphAnalyzer provides structural complexity signals.
// Implemented as an adapter for your existing graph package.
type GraphAnalyzer interface {
    CountAffectedNodes(nodeIDs []string) (int, error)
    IsCrossRepo(nodeIDs []string) (bool, error)
    GetLanguage(nodeIDs []string) (string, error) // "go", "python", etc.
}

// NewRouter creates a new Router.
// Parameters:
//   - skills: Engine 3's skill matcher
//   - memory: Engine 2's memory recaller
//   - graph: Engine 1's graph analyzer
//   - config: orchestrator config with routing thresholds
func NewRouter(skills SkillMatcher, memory MemoryRecaller, graph GraphAnalyzer, config Config) *Router

// Route makes the routing decision for a task.
// This is the core function — called for every developer request.
//
// ZERO LLM tokens. All decisions are database queries.
//
// Decision tree (executed top to bottom, first match wins):
//
//   1. SKILL MATCH? → skill.Match(task.GraphNodeIDs, task.Prompt)
//      If skill found AND successRate > 0.85 AND graphOverlap > 0.5:
//        → ModeSkillExecute (Haiku follows recipe, Tier 1 verify)
//
//   2. MEMORY MATCH? → memory.QuickCheck(task.GraphNodeIDs, task.DeveloperID)
//      If exact match AND confidence > 0.80:
//        → ModeMemoryApply (Haiku applies known solution, Tier 2 verify)
//
//   3. COMPLEXITY CHECK → graph.CountAffectedNodes + graph.IsCrossRepo
//
//      3a. SIMPLE + HAS TEMPLATE:
//          affectedNodes <= 3 AND NOT crossRepo AND HasTemplate(task.TaskType):
//            → ModePlanExecute (Opus plans briefly, Haiku executes)
//
//      3b. SIMPLE + NO TEMPLATE:
//          affectedNodes <= 2 AND NOT crossRepo AND NOT HasTemplate:
//            → ModeSingleHaiku (Haiku handles alone, Tier 2 verify)
//
//      3c. COMPLEX + HAS TEMPLATE:
//          affectedNodes > 3 OR crossRepo AND HasTemplate:
//            → ModeFullOrchestration (Opus plan + Haiku execute + Opus verify Tier 3)
//
//      3d. COMPLEX + NO TEMPLATE:
//          → ModeSingleOpus (Opus handles the whole task, Tier 3 verify)
//
// Parameters:
//   - task: the developer's request
//
// Returns: RoutingDecision with mode, models, verify tier, and reason
func (r *Router) Route(task Task) (*RoutingDecision, error)

// ClassifyTaskType determines the TaskType from the developer's prompt.
// Uses keyword matching + simple heuristics — NOT an LLM call.
//
// Keyword rules:
//   - "fix", "bug", "error", "mismatch", "broken" → TaskCodeFix
//   - "test", "cover", "assert", "verify" → TaskTestGen
//   - "pr", "pull request", "description", "review" → TaskPRGen
//   - "refactor", "rename", "extract", "move" → TaskRefactor
//   - "update", "upgrade", "dependency", "version" → TaskDepUpdate
//   - "config", "setting", "environment", "env" → TaskConfigChange
//   - "migrate", "migration", "schema" → TaskMigration
//   - "analyze", "impact", "affect", "break" → TaskAnalysis
//   - "explain", "why", "how does", "what is" → TaskExplanation
//   - None of the above → TaskGeneral
//
// For ambiguous cases, default to TaskGeneral (routes to SingleOpus).
// Better to over-route to Opus than to mis-classify and send to Haiku
// with the wrong template.
func ClassifyTaskType(prompt string) TaskType
```

---

## 8. File: `internal/orchestrator/planner.go`

Opus creates structured plans. Constrained by templates — no free-form text.

```go
package orchestrator

// Planner uses Opus to create structured execution plans.
// The plan is a JSON spec matching the template for the task type.
// Opus is constrained — it fills a fixed structure, not free-form.

// NewPlanner creates a new Planner.
// Parameters:
//   - client: the LLM client for Opus calls
//   - compressor: Engine 4's prompt compression
//   - config: orchestrator config
func NewPlanner(client *LLMClient, compressor CompressFunc, config Config) *Planner

// CompressFunc is an interface to Engine 4's BuildPrompt.
// Injected to avoid direct dependency on the compress package.
type CompressFunc func(basePrompt string, graphContext string, memoryContext string) string

// CreatePlan generates a structured plan for a task.
//
// Parameters:
//   - task: the developer's request
//   - decision: the routing decision (contains template ID, memory context, etc.)
//
// Returns: *Plan with structured sub-tasks, error
//
// Implementation:
//   1. Get the planner prompt for this task type:
//      prompt := GetPlannerPrompt(task.TaskType, graphContext, memoryContext, skillContext)
//   2. Compress the prompt via Engine 4:
//      compressed := compressor(prompt, graphShorthand, memoryText)
//   3. Call Opus via LLM client:
//      response := client.Call(Opus, systemPrompt, compressed, maxTokens=1000)
//   4. Parse the JSON response into a Plan struct
//   5. Validate the plan:
//      - Each sub-task has a valid action
//      - depends_on references exist
//      - verify_tier is set
//      - No circular dependencies in depends_on
//   6. Return the plan
//
// Error handling:
//   - If Opus returns invalid JSON: retry once with stricter prompt
//   - If retry fails: fall back to SingleOpus mode (Opus does everything)
//   - If plan has circular dependencies: break the cycle, log warning
func (p *Planner) CreatePlan(task Task, decision *RoutingDecision) (*Plan, error)

// ValidatePlan checks a plan for structural correctness.
// Called after parsing Opus's JSON output.
//
// Checks:
//   - All sub-task IDs are unique
//   - All depends_on references point to existing sub-task IDs
//   - No circular dependencies
//   - All actions are valid ("modify_file", "create_file", "run_test", etc.)
//   - All verify_tiers are valid (1, 2, or 3)
//   - At most 5 sub-tasks (prevent Opus from over-decomposing)
func ValidatePlan(plan *Plan) error
```

---

## 9. File: `internal/orchestrator/executor.go`

Haiku executes sub-tasks from the plan. Follows instructions — doesn't make decisions.

```go
package orchestrator

// Executor uses Haiku to execute sub-tasks from a plan.
// Each sub-task is a specific, constrained action.

// NewExecutor creates a new Executor.
func NewExecutor(client *LLMClient, compressor CompressFunc, config Config) *Executor

// ExecuteSubTask runs one sub-task using Haiku.
//
// Parameters:
//   - subTask: the sub-task from the plan
//   - taskContext: the original developer request (for context)
//   - previousResults: results from earlier sub-tasks (for dependent tasks)
//
// Returns: *SubTaskResult, error
//
// Implementation:
//   1. Build the execution prompt:
//      - System: "Execute this task exactly as specified. Output ONLY the result."
//      - User: the sub-task spec as JSON
//      - If subTask.DependsOn has results, append: "Context from previous steps: [results]"
//   2. Compress via Engine 4 (LevelFull for code output, LevelCompact for text)
//   3. Call Haiku via LLM client
//   4. Parse the response based on the action type:
//      - "modify_file" → expect the modified code
//      - "create_file" → expect the new file content
//      - "generate_test" → expect test function code
//      - "generate_pr" → expect PR title + body
//   5. Return SubTaskResult with success/failure, output, and token count
//
// For skill_execute mode:
//   When RoutingDecision.Mode is ModeSkillExecute:
//   1. Get the skill instruction from Engine 3
//   2. Use the skill instruction as the system prompt
//   3. Pass the developer's request as the user message
//   4. Haiku follows the skill recipe directly — no plan needed
func (e *Executor) ExecuteSubTask(subTask SubTask, taskContext string, previousResults []SubTaskResult) (*SubTaskResult, error)

// ExecuteWithSkill runs a task using a skill recipe.
// Used in ModeSkillExecute — the cheapest routing mode.
//
// Parameters:
//   - task: the developer's request
//   - skillInstruction: the skill's instruction text from Engine 3
//
// Returns: *SubTaskResult, error
func (e *Executor) ExecuteWithSkill(task Task, skillInstruction string) (*SubTaskResult, error)

// ExecuteWithMemory applies a known solution from memory.
// Used in ModeMemoryApply.
//
// Parameters:
//   - task: the developer's request
//   - memorySummary: the relevant observation from Engine 2
//
// Returns: *SubTaskResult, error
func (e *Executor) ExecuteWithMemory(task Task, memorySummary string) (*SubTaskResult, error)
```

---

## 10. File: `internal/orchestrator/verifier.go`

Three-tier verification system. Most outputs verified for zero Opus tokens.

```go
package orchestrator

// Verifier checks outputs using the appropriate verification tier.
//   Tier 1 (Automated):  Run tests, lint, syntax check. Zero Opus tokens.
//   Tier 2 (Spot Check): Opus structural comparison. 50-100 tokens.
//   Tier 3 (Full Review): Opus reads and judges output. 300-500 tokens.

// NewVerifier creates a new Verifier.
func NewVerifier(client *LLMClient, config Config) *Verifier

// Verify checks a sub-task result based on its verify tier.
//
// Parameters:
//   - subTask: the sub-task spec (to compare against)
//   - result: the executor's output
//   - tier: which verification tier to apply
//
// Returns: *VerifyResult, error
type VerifyResult struct {
    Passed       bool   `json:"passed"`
    Tier         VerifyTier `json:"tier"`
    Reason       string `json:"reason"`       // why it passed or failed
    TokensUsed   int    `json:"tokens_used"`  // 0 for Tier 1
}

func (v *Verifier) Verify(subTask SubTask, result *SubTaskResult, tier VerifyTier) (*VerifyResult, error)

// ============================================================
// TIER 1: AUTOMATED VERIFICATION
// ============================================================

// VerifyAutomatedCheck runs automated checks based on the action type.
//
// Action "modify_file" or "create_file":
//   1. Check if the output is valid Go code: run "go vet" on it
//   2. If test_command is specified in the plan: run the tests
//   3. Pass if both succeed
//
// Action "generate_test":
//   1. Write the test to a temp file
//   2. Run "go test -run <function_name>" 
//   3. Pass if tests compile and run (even if some fail — test existence is the goal)
//
// Action "config_change":
//   1. Parse the output as the specified format (JSON/YAML/TOML)
//   2. Pass if it parses without errors
//
// Returns: *VerifyResult with Passed=true/false, TokensUsed=0
func (v *Verifier) verifyAutomated(subTask SubTask, result *SubTaskResult) (*VerifyResult, error)

// ============================================================
// TIER 2: SPOT CHECK
// ============================================================

// VerifySpotCheck asks Opus for a quick structural comparison.
// NOT a quality review — just "does the output match the spec?"
//
// Opus prompt (50-100 tokens):
//   "Compare this output against the spec. Answer ONLY with JSON:
//    {"passed": true/false, "reason": "one sentence"}
//
//    Spec fields to check:
//    - [list key fields from the sub-task spec]
//
//    Output to check:
//    - [the executor's output, truncated to 500 tokens]
//
//    Check ONLY: are the spec's key fields present in the output?
//    Do NOT judge quality. Do NOT suggest improvements."
//
// Returns: *VerifyResult with TokensUsed ~50-100
func (v *Verifier) verifySpotCheck(subTask SubTask, result *SubTaskResult) (*VerifyResult, error)

// ============================================================
// TIER 3: FULL REVIEW
// ============================================================

// VerifyFullReview asks Opus for a thorough quality check.
// Used for: cross-repo changes, security-sensitive code, architecture decisions.
//
// Opus prompt (300-500 tokens):
//   "Review this output for correctness and completeness.
//    
//    Original task: [developer's request]
//    Plan spec: [the sub-task spec]
//    Output: [the executor's full output]
//
//    Check:
//    1. Does the output correctly implement the spec?
//    2. Are there any bugs, errors, or omissions?
//    3. For code: are there security concerns?
//    4. For cross-repo: is it consistent across repos?
//
//    Answer with JSON:
//    {"passed": true/false, "reason": "explanation", "issues": ["list of issues"]}"
//
// Returns: *VerifyResult with TokensUsed ~300-500
func (v *Verifier) verifyFullReview(subTask SubTask, result *SubTaskResult, taskContext string) (*VerifyResult, error)

// ============================================================
// BATCH VERIFICATION
// ============================================================

// VerifyBatch checks multiple sub-task results in a single Opus call.
// Used to reduce API calls and latency when multiple outputs need spot check or full review.
//
// Instead of 3 separate Opus calls for 3 sub-tasks:
//   "Verify these 3 outputs against these 3 specs. For each, answer..."
// One API call, one round-trip, shared prompt template.
//
// Saves ~60% verification latency, ~40% verification tokens.
//
// Parameters:
//   - items: list of (subTask, result, tier) tuples
//
// Returns: list of VerifyResults in same order
func (v *Verifier) VerifyBatch(items []VerifyItem) ([]VerifyResult, error)

type VerifyItem struct {
    SubTask SubTask
    Result  *SubTaskResult
    Tier    VerifyTier
}
```

---

## 11. File: `internal/orchestrator/escalation.go`

Three-step failure recovery chain.

```go
package orchestrator

// Escalation handles failures in sub-task execution.
// Three steps: retry with error → Opus rephrase → Opus takeover.

// NewEscalation creates a new Escalation handler.
func NewEscalation(executor *Executor, planner *Planner, client *LLMClient, config Config) *Escalation

// HandleFailure attempts to recover from a failed sub-task execution.
//
// Parameters:
//   - subTask: the sub-task that failed
//   - failedResult: the failed result (with error message)
//   - taskContext: the original developer request
//   - previousResults: results from earlier sub-tasks
//
// Returns: *SubTaskResult (the successful result), *EscalationRecord, error
//
// Implementation:
//
//   STEP 1: RETRY WITH ERROR FEEDBACK (cheap — same Haiku cost)
//     Append to the sub-task spec:
//       "PREVIOUS ATTEMPT FAILED WITH: [error message]
//        Fix the error and try again."
//     Call executor.ExecuteSubTask with the augmented spec.
//     If success → return result with EscalationSteps=1
//
//   STEP 2: OPUS REPHRASE (medium — ~300 Opus tokens + Haiku retry)
//     Send to Opus:
//       "This sub-task spec failed twice.
//        Spec: [original spec]
//        Error 1: [first error]
//        Error 2: [second error]
//        Rewrite the spec to avoid these errors. Keep the same JSON structure."
//     Call executor.ExecuteSubTask with the new spec.
//     If success → return result with EscalationSteps=2
//
//   STEP 3: OPUS TAKEOVER (expensive — full Opus execution)
//     Send to Opus:
//       "Complete this sub-task yourself.
//        Original task: [developer request]
//        Sub-task spec: [spec]
//        Previous errors: [error1, error2, error3]
//        Generate the correct output."
//     Return result with EscalationSteps=3, WasTakeover=true
//
// The returned EscalationRecord is logged for analysis.
// If the same task type triggers takeover repeatedly,
// it signals the spec template needs improvement.
type EscalationRecord struct {
    SubTaskID  string           `json:"sub_task_id"`
    Steps      []EscalationStep `json:"steps"`
    FinalModel ModelTier        `json:"final_model"`  // which model ultimately succeeded
    TotalExtraTokens int        `json:"total_extra_tokens"` // tokens burned on escalation
}

func (esc *Escalation) HandleFailure(
    subTask SubTask,
    failedResult *SubTaskResult,
    taskContext string,
    previousResults []SubTaskResult,
) (*SubTaskResult, *EscalationRecord, error)
```

---

## 12. File: `internal/orchestrator/parallel.go`

Parallel sub-task execution with dependency tracking.

```go
package orchestrator

// ParallelExecutor runs independent sub-tasks in parallel
// and sequential sub-tasks in dependency order.
//
// Architecture:
//   1. Build a dependency graph from sub-task depends_on fields
//   2. Find all sub-tasks with no dependencies → run in parallel
//   3. Wait for all to complete
//   4. Find all sub-tasks whose dependencies are now satisfied → run in parallel
//   5. Repeat until all sub-tasks are done
//
// This is a standard topological sort execution.

// NewParallelExecutor creates a new ParallelExecutor.
func NewParallelExecutor(executor *Executor, escalation *Escalation, config Config) *ParallelExecutor

// ExecuteAll runs all sub-tasks in a plan with maximum parallelism.
//
// Parameters:
//   - plan: the plan containing sub-tasks
//   - taskContext: the original developer request
//
// Returns: []SubTaskResult (one per sub-task, in plan order), []EscalationRecord, error
//
// Implementation:
//   1. Build dependency map: map[subTaskID][]dependsOnIDs
//   2. Build reverse map: map[subTaskID][]dependedByIDs
//   3. Find ready tasks (no dependencies or all deps completed)
//   4. Launch ready tasks in parallel (up to config.MaxParallelHaikuCalls)
//   5. As each completes:
//      a. If success → mark as done, check if dependents are now ready
//      b. If failure → run escalation.HandleFailure
//      c. If escalation succeeds → mark as done
//      d. If escalation fails after all steps → mark as failed, log error
//   6. Return all results
//
// Thread safety:
//   - Use sync.WaitGroup for parallel goroutines
//   - Use sync.Mutex for the shared results slice and dependency tracking
//   - Use a semaphore (buffered channel) to limit max concurrent calls
func (pe *ParallelExecutor) ExecuteAll(plan *Plan, taskContext string) ([]SubTaskResult, []EscalationRecord, error)

// buildDependencyGraph creates adjacency lists for topological execution.
// Returns: (readyTasks []string, dependsOn map[string][]string, dependedBy map[string][]string)
func buildDependencyGraph(subTasks []SubTask) ([]string, map[string][]string, map[string][]string)
```

---

## 13. File: `internal/orchestrator/llmclient.go`

Anthropic API client with rate limiting and cost tracking.

```go
package orchestrator

// LLMClient handles all communication with the Anthropic API.
// Supports both Opus and Haiku with separate rate limits.

// NewLLMClient creates a new LLM client.
// Parameters:
//   - opusConfig: model config for Opus
//   - haikuConfig: model config for Haiku
func NewLLMClient(opusConfig ModelConfig, haikuConfig ModelConfig) *LLMClient

// LLMResponse is the parsed response from the Anthropic API.
type LLMResponse struct {
    Content      string    // the text output
    InputTokens  int       // tokens in the prompt
    OutputTokens int       // tokens in the response
    Model        ModelTier // which model was used
    LatencyMs    int       // round-trip time
    CostUSD      float64   // calculated cost
}

// Call makes a request to the Anthropic API.
//
// Parameters:
//   - model: Opus or Haiku
//   - systemPrompt: the system prompt
//   - userMessage: the user message
//   - maxTokens: max output tokens
//
// Returns: *LLMResponse, error
//
// Implementation:
//   1. Select the model config based on the model tier
//   2. Acquire a slot from the rate limiter (block if at limit)
//   3. Build the request body:
//      {
//        "model": config.ModelID,
//        "max_tokens": maxTokens,
//        "system": systemPrompt,
//        "messages": [{"role": "user", "content": userMessage}]
//      }
//   4. POST to config.Endpoint
//   5. Set headers:
//      "Content-Type": "application/json"
//      "x-api-key": config.APIKey
//      "anthropic-version": "2023-06-01"
//   6. Parse response:
//      - Extract content[0].text
//      - Extract usage.input_tokens, usage.output_tokens
//   7. Calculate cost:
//      cost = (input_tokens * config.InputCostPerM / 1_000_000) +
//             (output_tokens * config.OutputCostPerM / 1_000_000)
//   8. Release rate limiter slot
//   9. Return LLMResponse
//
// Error handling:
//   - HTTP 429 (rate limit): exponential backoff, retry up to 3 times
//     Wait: 1s → 2s → 4s
//   - HTTP 500/502/503: retry once after 2s
//   - HTTP 400: return error (bad request, don't retry)
//   - Timeout (30s): return error
func (c *LLMClient) Call(model ModelTier, systemPrompt, userMessage string, maxTokens int) (*LLMResponse, error)

// Rate limiter implementation using a token bucket per model.
//
// Uses a buffered channel as a semaphore:
//   opusSemaphore  = make(chan struct{}, config.OpusConfig.MaxConcurrent)
//   haikuSemaphore = make(chan struct{}, config.HaikuConfig.MaxConcurrent)
//
// Also tracks calls per minute using a sliding window.
// If calls_per_minute > limit, block until the window slides.
```

---

## 14. File: `internal/orchestrator/tracker.go`

Cost tracking for the manager dashboard.

```go
package orchestrator

// Tracker logs every LLM call to the agent_costs table.
// Provides methods to query cost summaries for dashboards.

// NewTracker creates a new Tracker.
func NewTracker(databaseURL string) (*Tracker, error)

// LogCall records a single LLM call to the database.
//
// Called by the orchestrator after every LLM call (plan, execute, verify, escalate).
// Runs asynchronously — never blocks the main execution flow.
//
// Parameters:
//   - record: the CostRecord to log
func (t *Tracker) LogCall(record CostRecord)

// LogTask records the complete cost of a task (sum of all calls).
// Called after the full orchestration cycle completes.
func (t *Tracker) LogTask(taskID string, decision *RoutingDecision, results []SubTaskResult, escalations []EscalationRecord)

// ============================================================
// DASHBOARD QUERIES
// ============================================================

// GetMonthlySummary returns the monthly cost summary.
// Reads from the monthly_cost_summary materialized view.
type MonthlySummary struct {
    Month              string  `json:"month"`
    TotalCalls         int     `json:"total_calls"`
    ActualCost         float64 `json:"actual_cost"`
    WouldHaveCost      float64 `json:"would_have_cost_all_opus"`
    SavingsUSD         float64 `json:"savings_usd"`
    SavingsPercent     float64 `json:"savings_percent"`
    SkillExecutions    int     `json:"skill_executions"`
    MemoryApplies      int     `json:"memory_applies"`
    PlanExecutes       int     `json:"plan_executes"`
    FullOrchestrations int     `json:"full_orchestrations"`
    Takeovers          int     `json:"takeovers"`
    OpusCost           float64 `json:"opus_cost"`
    HaikuCost          float64 `json:"haiku_cost"`
    AvgLatencyMs       float64 `json:"avg_latency_ms"`
}

func (t *Tracker) GetMonthlySummary() ([]MonthlySummary, error)

// GetDeveloperSummary returns per-developer cost breakdown.
func (t *Tracker) GetDeveloperSummary(developerID string) ([]DeveloperWeekSummary, error)

type DeveloperWeekSummary struct {
    Week       string  `json:"week"`
    TotalCalls int     `json:"total_calls"`
    ActualCost float64 `json:"actual_cost"`
    SavingsUSD float64 `json:"savings_usd"`
    Takeovers  int     `json:"takeovers"`
}

// GetRoutingEffectiveness returns which routing modes are most cost-effective.
func (t *Tracker) GetRoutingEffectiveness() ([]RoutingStats, error)

type RoutingStats struct {
    Mode           RoutingMode `json:"mode"`
    TotalUses      int         `json:"total_uses"`
    AvgCost        float64     `json:"avg_cost"`
    AvgLatency     float64     `json:"avg_latency_ms"`
    AvgEscalations float64    `json:"avg_escalations"`
    Takeovers      int         `json:"takeovers"`
    TotalCost      float64     `json:"total_cost"`
}

// RefreshViews refreshes the materialized views.
// Call this periodically (every hour) via a background goroutine.
func (t *Tracker) RefreshViews() error
```

---

## 15. The Main Orchestrator — Tying It All Together

```go
package orchestrator

// Orchestrator is the main entry point for Engine 5.
// It wires together: router, planner, executor, verifier, escalation,
// parallel executor, and cost tracker.
//
// This is the function your MCP server calls for every developer request.

// NewOrchestrator creates a fully wired orchestrator.
func NewOrchestrator(
    skills SkillMatcher,      // Engine 3
    memory MemoryRecaller,    // Engine 2
    graph GraphAnalyzer,      // Engine 1
    compressor CompressFunc,  // Engine 4
    config Config,
) (*Orchestrator, error)

// Execute handles a developer's request end-to-end.
//
// This is THE function. Everything flows through here.
//
// Parameters:
//   - task: the developer's request
//
// Returns: *TaskResult, error
//
// Implementation:
//
//   STEP 1: CLASSIFY
//     task.TaskType = ClassifyTaskType(task.Prompt)
//
//   STEP 2: ROUTE (zero tokens)
//     decision := router.Route(task)
//     Log: "Task [ID] routed to [mode] because [reason]"
//
//   STEP 3: EXECUTE (based on routing mode)
//
//     Case ModeSkillExecute:
//       result := executor.ExecuteWithSkill(task, decision.MatchedSkillID)
//       verify := verifier.Verify(subTask, result, VerifyAutomated)
//       Track cost.
//
//     Case ModeMemoryApply:
//       result := executor.ExecuteWithMemory(task, memoryContext)
//       verify := verifier.Verify(subTask, result, VerifySpotCheck)
//       Track cost.
//
//     Case ModePlanExecute:
//       plan := planner.CreatePlan(task, decision)
//       results, escalations := parallelExecutor.ExecuteAll(plan, task.Prompt)
//       verifyResults := verifier.VerifyBatch(buildVerifyItems(plan, results))
//       Track cost.
//
//     Case ModeFullOrchestration:
//       plan := planner.CreatePlan(task, decision)
//       results, escalations := parallelExecutor.ExecuteAll(plan, task.Prompt)
//       verifyResults := verifier.VerifyBatch(buildVerifyItems(plan, results))
//       Track cost.
//
//     Case ModeSingleOpus:
//       result := client.Call(Opus, compressedPrompt, task.Prompt, 4096)
//       Track cost.
//
//     Case ModeSingleHaiku:
//       result := client.Call(Haiku, compressedPrompt, task.Prompt, 4096)
//       verify := verifier.Verify(subTask, result, VerifySpotCheck)
//       Track cost.
//
//   STEP 4: TRACK (async — never blocks)
//     tracker.LogTask(task.ID, decision, results, escalations)
//
//   STEP 5: FEED BACK TO ENGINE 2 + 3
//     Engine 2 (memory): session manager captures the execution as events
//     Engine 3 (skills): if no skill matched and task succeeded,
//                        trigger CAPTURED skill extraction
//
//   STEP 6: RETURN
//     Assemble results into TaskResult and return to MCP server.

type TaskResult struct {
    TaskID       string           `json:"task_id"`
    Success      bool             `json:"success"`
    Output       string           `json:"output"`        // assembled output from all sub-tasks
    RoutingMode  RoutingMode      `json:"routing_mode"`
    TotalCost    float64          `json:"total_cost_usd"`
    TotalTokens  int              `json:"total_tokens"`
    LatencyMs    int              `json:"latency_ms"`
    SubResults   []SubTaskResult  `json:"sub_results,omitempty"`
    Escalations  []EscalationRecord `json:"escalations,omitempty"`
    SkillUsed    string           `json:"skill_used,omitempty"`
    MemoryHit    bool             `json:"memory_hit"`
}

func (o *Orchestrator) Execute(task Task) (*TaskResult, error)

// Stop gracefully shuts down the orchestrator.
// Waits for in-flight tasks, flushes cost tracker, stops background goroutines.
func (o *Orchestrator) Stop()
```

---

## 16. MCP Tools to Expose

When you build the MCP server, add these tools for Engine 5:

### Tool 1: `execute_task`

```json
{
  "name": "execute_task",
  "description": "Execute a development task through the orchestrator. Automatically routes to the optimal model tier, uses skills and memory when available, and tracks costs.",
  "input_schema": {
    "type": "object",
    "properties": {
      "prompt": {
        "type": "string",
        "description": "The developer's request."
      },
      "graph_node_ids": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Graph nodes affected by the current context (from diff or open file)."
      }
    },
    "required": ["prompt"]
  }
}
```

### Tool 2: `get_cost_summary`

```json
{
  "name": "get_cost_summary",
  "description": "Get cost and savings summary for the dashboard.",
  "input_schema": {
    "type": "object",
    "properties": {
      "period": {
        "type": "string",
        "enum": ["week", "month", "all"],
        "description": "Time period for the summary."
      },
      "developer_id": {
        "type": "string",
        "description": "Filter by developer. Empty for team-wide."
      }
    }
  }
}
```

---

## 17. Testing Strategy

Create `internal/orchestrator/orchestrator_test.go`:

```go
package orchestrator

import "testing"

// ============================================================
// ROUTER TESTS
// ============================================================

// Test 1: Skill match routes to ModeSkillExecute
func TestRouter_SkillMatch(t *testing.T) {
    // Mock: skill matcher returns a match with 90% success rate
    // Expect: ModeSkillExecute, SkipPlanning=true, VerifyTier=1
}

// Test 2: Memory match routes to ModeMemoryApply
func TestRouter_MemoryMatch(t *testing.T) {
    // Mock: memory returns exact match with confidence 0.9
    // Expect: ModeMemoryApply, SkipPlanning=true, VerifyTier=2
}

// Test 3: Simple task with template routes to ModePlanExecute
func TestRouter_SimplePlanExecute(t *testing.T) {
    // Mock: no skill, no memory, 2 affected nodes, not cross-repo, TaskCodeFix
    // Expect: ModePlanExecute
}

// Test 4: Complex cross-repo routes to ModeFullOrchestration
func TestRouter_ComplexFullOrch(t *testing.T) {
    // Mock: no skill, no memory, 5 affected nodes, cross-repo, TaskCodeFix
    // Expect: ModeFullOrchestration, VerifyTier=3
}

// Test 5: No template routes to ModeSingleOpus
func TestRouter_NoTemplateOpus(t *testing.T) {
    // Mock: no skill, no memory, TaskExplanation (no template)
    // Expect: ModeSingleOpus
}

// Test 6: Simple no template routes to ModeSingleHaiku
func TestRouter_SimpleHaiku(t *testing.T) {
    // Mock: no skill, no memory, 1 affected node, TaskGeneral, simple
    // Expect: ModeSingleHaiku
}

// Test 7: Skill match below threshold doesn't route to skill
func TestRouter_SkillBelowThreshold(t *testing.T) {
    // Mock: skill match with 60% success rate (below 85% threshold)
    // Expect: NOT ModeSkillExecute — falls through to next check
}

// Test 8: ClassifyTaskType correctly classifies prompts
func TestClassifyTaskType(t *testing.T) {
    // "fix the type mismatch" → TaskCodeFix
    // "write tests for ValidateToken" → TaskTestGen
    // "create a PR description" → TaskPRGen
    // "explain why this breaks" → TaskExplanation
    // "update the database config" → TaskConfigChange
}

// ============================================================
// PLANNER TESTS
// ============================================================

// Test 9: CreatePlan produces valid JSON for code_fix template
func TestPlanner_CodeFix(t *testing.T) {
    // Mock Opus response with valid code_fix JSON
    // Verify: plan has sub-tasks with correct structure
}

// Test 10: CreatePlan rejects invalid JSON and retries
func TestPlanner_RetryOnInvalidJSON(t *testing.T) {
    // Mock: first Opus call returns invalid JSON, second returns valid
    // Verify: plan is valid after retry
}

// Test 11: ValidatePlan detects circular dependencies
func TestValidatePlan_CircularDeps(t *testing.T) {
    // Plan where A depends on B and B depends on A
    // Verify: ValidatePlan returns error
}

// ============================================================
// EXECUTOR TESTS
// ============================================================

// Test 12: ExecuteSubTask with code_fix spec
func TestExecutor_CodeFix(t *testing.T) {
    // Mock Haiku returning valid code changes
    // Verify: result.Success=true, output contains the modified code
}

// Test 13: ExecuteWithSkill follows skill recipe
func TestExecutor_SkillExecute(t *testing.T) {
    // Mock Haiku with a skill instruction
    // Verify: the skill instruction was used as system prompt
}

// ============================================================
// VERIFIER TESTS
// ============================================================

// Test 14: Tier 1 automated verification catches syntax errors
func TestVerifier_AutomatedCatchesSyntax(t *testing.T) {
    // Output with invalid Go syntax
    // Verify: VerifyResult.Passed=false, TokensUsed=0
}

// Test 15: Tier 2 spot check with Opus
func TestVerifier_SpotCheck(t *testing.T) {
    // Mock Opus returning {"passed": true}
    // Verify: TokensUsed ~50-100
}

// Test 16: VerifyBatch combines multiple checks
func TestVerifier_Batch(t *testing.T) {
    // 3 items to verify
    // Verify: only 1 Opus call made (not 3)
}

// ============================================================
// ESCALATION TESTS
// ============================================================

// Test 17: Step 1 retry with error succeeds
func TestEscalation_RetrySucceeds(t *testing.T) {
    // First attempt fails, retry with error succeeds
    // Verify: EscalationSteps=1, WasTakeover=false
}

// Test 18: Step 2 Opus rephrase succeeds
func TestEscalation_RephraseSucceeds(t *testing.T) {
    // Two Haiku failures, Opus rephrase, Haiku succeeds with new spec
    // Verify: EscalationSteps=2
}

// Test 19: Step 3 Opus takeover
func TestEscalation_OpusTakeover(t *testing.T) {
    // Three failures, Opus takes over
    // Verify: EscalationSteps=3, WasTakeover=true, Model=Opus
}

// ============================================================
// PARALLEL EXECUTION TESTS
// ============================================================

// Test 20: Independent sub-tasks run in parallel
func TestParallel_IndependentTasks(t *testing.T) {
    // 3 sub-tasks with no dependencies
    // Verify: all 3 launched concurrently (check timing)
}

// Test 21: Dependent sub-tasks run in order
func TestParallel_DependentTasks(t *testing.T) {
    // Task B depends on Task A
    // Verify: B doesn't start until A completes
}

// Test 22: Mixed parallel and sequential
func TestParallel_MixedDeps(t *testing.T) {
    // A: no deps, B: no deps, C: depends on A
    // Verify: A and B run parallel, C waits for A
}

// ============================================================
// TRACKER TESTS
// ============================================================

// Test 23: LogCall writes to database
func TestTracker_LogCall(t *testing.T) {
    // Log a cost record
    // Query the database directly
    // Verify: record exists with correct fields
}

// Test 24: GetMonthlySummary returns correct totals
func TestTracker_MonthlySummary(t *testing.T) {
    // Insert multiple cost records
    // Refresh materialized view
    // Verify: totals match
}

// ============================================================
// FULL ORCHESTRATION TEST
// ============================================================

// Test 25: End-to-end orchestration
func TestOrchestrator_EndToEnd(t *testing.T) {
    // Submit a TaskCodeFix
    // Mock all engines
    // Verify: routes correctly, plans, executes, verifies, tracks cost
    // Check: TaskResult has all fields populated
}
```

---

## 18. Acceptance Criteria

Engine 5 is complete when:

- [ ] Router makes correct routing decisions for all 6 modes without any LLM calls
- [ ] ClassifyTaskType correctly identifies all task types from keywords
- [ ] Planner produces valid structured JSON specs using templates
- [ ] Executor executes sub-tasks using Haiku and returns structured results
- [ ] ExecuteWithSkill correctly uses skill instructions as system prompts
- [ ] Verifier implements all three tiers (automated, spot check, full review)
- [ ] VerifyBatch combines multiple verifications into a single Opus call
- [ ] Escalation chain works: retry → rephrase → takeover
- [ ] Parallel executor runs independent sub-tasks concurrently
- [ ] Parallel executor respects dependency ordering
- [ ] LLM client handles rate limits with exponential backoff
- [ ] Cost tracker logs every LLM call to PostgreSQL
- [ ] Materialized views produce correct dashboard summaries
- [ ] All 25 tests pass
- [ ] `go build ./...` succeeds
- [ ] No changes to existing Engine 1-4 files (uses interfaces only)

---

## 19. What NOT to Build in This Engine

- Do NOT modify Engine 1-4 code — use the interfaces defined in router.go
- Do NOT build the MCP server — that's a separate integration spec
- Do NOT build the web dashboard UI — this engine produces the DATA for dashboards
- Do NOT implement real model fine-tuning — we use off-the-shelf Opus and Haiku
- Do NOT implement streaming responses — batch is fine for V1
- Do NOT add WebSocket support — HTTP REST for V1

---

## 20. Future Improvements (Not in This Build)

1. **Streaming responses** — stream Haiku's output to the developer in real-time instead of waiting for completion
2. **Adaptive routing thresholds** — auto-tune the skill match and memory match thresholds based on actual success rates
3. **Model benchmarking** — periodically test both models on sample tasks to detect capability changes after model updates
4. **Cost budgets** — per-developer or per-team monthly cost budgets with automatic downgrade to cheaper routing when approaching limits
5. **A/B testing** — route 5% of tasks through alternative modes to compare effectiveness
6. **Takeover analysis** — weekly report of all Opus takeovers with root cause analysis, automatically generates template improvement suggestions
7. **Latency-optimized mode** — for interactive requests, skip verification entirely and verify async after responding
8. **Multi-provider routing** — add OpenAI, Google models as alternatives for cost optimization
