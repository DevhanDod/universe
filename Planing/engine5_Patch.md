# Engine 5 — Alignment Patch (Major Rewrite)

## Apply These Changes to engine5.md

**What changed:** Engine 5 was an internal LLM orchestrator that called Opus/Haiku APIs directly. It's now a plan bridge between two Cursor windows + cost tracker + workspace generator. Universe no longer calls any LLM APIs. Cursor does all the AI work.  
**Impact:** 6 files removed, 4 files simplified, 3 files added. Net result: simpler, smaller, no API keys needed.

---

## Change Summary — File by File

| File | Action | Reason |
|------|--------|--------|
| `types.go` | REWRITE | New types for plan-based flow, remove LLM call types |
| `router.go` | SIMPLIFY | Becomes a recommendation engine, not a routing engine |
| `templates.go` | KEEP mostly | Plan templates still useful for structuring Opus output |
| `tracker.go` | MODIFY | Tracks plans instead of internal LLM calls |
| `plans.go` | NEW | CRUD for the plans table — the bridge between two agents |
| `workspace.go` | NEW | Generates .code-workspace files with model presets |
| `setup.go` | NEW | Generates cursor rules, mcp.json, workspace files |
| `llmclient.go` | DELETE | Universe no longer calls LLM APIs |
| `planner.go` | DELETE | Opus in Cursor does the planning |
| `executor.go` | DELETE | Cheap model in Cursor does the execution |
| `verifier.go` | DELETE | Opus in Cursor does the verification |
| `escalation.go` | DELETE | Cursor handles retries |
| `parallel.go` | DELETE | Cursor handles execution |

---

## New Project Structure

```
internal/
└── orchestrator/
    ├── types.go          # REWRITTEN — plan-based types, model config
    ├── router.go         # SIMPLIFIED — recommendation engine, zero LLM calls
    ├── templates.go      # KEPT — plan templates for structuring Opus output
    ├── tracker.go        # MODIFIED — tracks plans and model usage
    ├── plans.go          # NEW — CRUD for plans table (the two-agent bridge)
    ├── workspace.go      # NEW — generates .code-workspace files
    ├── setup.go          # NEW — generates cursor rules + mcp.json + workspaces
    └── orchestrator_test.go  # REWRITTEN — tests for new architecture
```

---

## Database Migration Update

**ADD the plans table. KEEP the agent_costs table but modify it.**

```sql
-- ============================================================
-- Engine 5 UPDATED: Plan Bridge + Cost Tracking
-- Add to migration or create new migration: 004_plans_table.sql
-- ============================================================

-- The plans table is the bridge between the planner agent and executor agent.
-- Opus writes a plan, stores it here. Haiku reads it, executes, stores result.
CREATE TABLE IF NOT EXISTS plans (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Who created the plan
    developer_id      TEXT NOT NULL,

    -- What the developer asked
    title             TEXT NOT NULL,
    task_prompt       TEXT NOT NULL,

    -- The plan itself — step by step instructions for the executor
    steps             JSONB NOT NULL,        -- ["step 1 text", "step 2 text", ...]

    -- Files the executor should touch
    files_to_change   TEXT[],

    -- Was a skill used in creating this plan?
    -- NULL if planner didn't use a skill, or the skill ID if it did
    skill_used        UUID REFERENCES skills(id),

    -- Was the skill verified by the planner before including?
    skill_verified    BOOLEAN DEFAULT false,

    -- Graph context summary from the planner
    graph_context     TEXT,

    -- Blast radius info
    affected_nodes    TEXT[],
    cross_repo        BOOLEAN DEFAULT false,
    risk_level        TEXT,                  -- "low", "medium", "high"

    -- Which models were used (whatever the developer configured)
    planner_model     TEXT,                  -- "claude-opus-4", "gpt-4o", etc.
    executor_model    TEXT,                  -- "claude-haiku-3.5", "gpt-4o-mini", etc.

    -- Plan lifecycle status
    -- pending    → planner created it, waiting for executor
    -- executing  → executor picked it up
    -- completed  → executor finished successfully
    -- failed     → executor failed
    -- verified   → planner approved the result
    -- rejected   → planner found issues in the result
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'executing', 'completed', 'failed', 'verified', 'rejected')),

    -- Executor result (filled by store_plan_result)
    result_success    BOOLEAN,
    result_summary    TEXT,
    result_files      TEXT[],               -- files actually changed
    result_tests      BOOLEAN,              -- did tests pass?
    result_error      TEXT,                 -- error detail if failed

    -- Verification (filled when planner verifies)
    verified          BOOLEAN,
    verification_note TEXT,

    -- Timestamps
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    executed_at       TIMESTAMPTZ,
    verified_at       TIMESTAMPTZ
);

CREATE INDEX idx_plans_developer ON plans(developer_id);
CREATE INDEX idx_plans_status ON plans(status);
CREATE INDEX idx_plans_created ON plans(created_at);
CREATE INDEX idx_plans_skill ON plans(skill_used);

-- ============================================================
-- UPDATE agent_costs table — simplify for plan-based tracking
-- ============================================================

-- REMOVE these columns (no longer relevant — Universe doesn't call LLMs):
--   model           (Universe doesn't know which model Cursor used)
--   input_tokens    (Universe doesn't see Cursor's token counts)
--   output_tokens
--   cost_usd        (Universe doesn't calculate per-token cost)
--   phase           (no plan/execute/verify phases inside Universe)
--   latency_ms
--   escalation_steps
--   was_takeover

-- REPLACE agent_costs with a simpler plan_costs table:

DROP TABLE IF EXISTS agent_costs;
DROP MATERIALIZED VIEW IF EXISTS monthly_cost_summary;
DROP MATERIALIZED VIEW IF EXISTS developer_cost_summary;

CREATE TABLE IF NOT EXISTS plan_costs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id         UUID REFERENCES plans(id),
    developer_id    TEXT NOT NULL,

    -- Model info (from developer's config, self-reported)
    planner_model   TEXT,
    executor_model  TEXT,

    -- Cost estimates (calculated from config model pricing)
    -- These are ESTIMATES based on the developer's configured pricing.
    -- Universe doesn't see actual token counts from Cursor.
    -- The developer reports estimated tokens via the MCP tool.
    estimated_planner_tokens  INT,
    estimated_executor_tokens INT,
    estimated_planner_cost    FLOAT,
    estimated_executor_cost   FLOAT,
    estimated_total_cost      FLOAT,

    -- What would it have cost if everything was on the premium model?
    estimated_all_premium_cost FLOAT,

    -- Savings
    estimated_savings          FLOAT,

    -- Was a skill used? (reduces tokens because agent follows recipe)
    skill_used      BOOLEAN DEFAULT false,

    -- Was memory helpful? (reduces tokens because agent skips investigation)
    memory_hit      BOOLEAN DEFAULT false,

    -- Routing mode (what the recommendation engine suggested)
    routing_recommendation TEXT,  -- "skill_available", "memory_hit", "plan_from_scratch"

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plan_costs_developer ON plan_costs(developer_id);
CREATE INDEX idx_plan_costs_created ON plan_costs(created_at);
CREATE INDEX idx_plan_costs_plan ON plan_costs(plan_id);

-- Updated materialized views for dashboard
CREATE MATERIALIZED VIEW monthly_cost_summary AS
SELECT
    date_trunc('month', created_at) AS month,
    COUNT(*) AS total_plans,
    SUM(estimated_total_cost) AS actual_cost,
    SUM(estimated_all_premium_cost) AS would_have_cost,
    SUM(estimated_savings) AS savings,
    COUNT(*) FILTER (WHERE skill_used) AS skill_uses,
    COUNT(*) FILTER (WHERE memory_hit) AS memory_hits,
    AVG(estimated_total_cost) AS avg_cost_per_plan
FROM plan_costs
GROUP BY 1;

CREATE MATERIALIZED VIEW developer_cost_summary AS
SELECT
    developer_id,
    date_trunc('week', created_at) AS week,
    COUNT(*) AS total_plans,
    SUM(estimated_total_cost) AS actual_cost,
    SUM(estimated_savings) AS savings
FROM plan_costs
GROUP BY 1, 2;
```

---

## File: `internal/orchestrator/types.go` — REWRITE

**DELETE everything from the current types.go. Replace with:**

```go
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
    ID              string     `json:"id"`
    DeveloperID     string     `json:"developer_id"`
    Title           string     `json:"title"`
    TaskPrompt      string     `json:"task_prompt"`
    Steps           []string   `json:"steps"`
    FilesToChange   []string   `json:"files_to_change,omitempty"`
    SkillUsed       string     `json:"skill_used,omitempty"`
    SkillVerified   bool       `json:"skill_verified"`
    GraphContext    string     `json:"graph_context,omitempty"`
    AffectedNodes   []string   `json:"affected_nodes,omitempty"`
    CrossRepo       bool       `json:"cross_repo"`
    RiskLevel       string     `json:"risk_level,omitempty"`
    PlannerModel    string     `json:"planner_model,omitempty"`
    ExecutorModel   string     `json:"executor_model,omitempty"`
    Status          PlanStatus `json:"status"`
    ResultSuccess   *bool      `json:"result_success,omitempty"`
    ResultSummary   string     `json:"result_summary,omitempty"`
    ResultFiles     []string   `json:"result_files,omitempty"`
    ResultTests     *bool      `json:"result_tests,omitempty"`
    ResultError     string     `json:"result_error,omitempty"`
    Verified        *bool      `json:"verified,omitempty"`
    VerificationNote string   `json:"verification_note,omitempty"`
    CreatedAt       time.Time  `json:"created_at"`
    ExecutedAt      *time.Time `json:"executed_at,omitempty"`
    VerifiedAt      *time.Time `json:"verified_at,omitempty"`
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
    // What the router found
    SkillAvailable    bool   `json:"skill_available"`
    SkillID           string `json:"skill_id,omitempty"`
    SkillName         string `json:"skill_name,omitempty"`
    SkillConfidence   float64 `json:"skill_confidence,omitempty"`
    MemoryAvailable   bool   `json:"memory_available"`
    MemoryCount       int    `json:"memory_count,omitempty"`

    // Graph analysis
    AffectedNodeCount int    `json:"affected_node_count"`
    CrossRepo         bool   `json:"cross_repo"`
    RiskLevel         string `json:"risk_level"`  // "low", "medium", "high"

    // Recommendation text (shown to the planner agent)
    Recommendation    string `json:"recommendation"`
    // Examples:
    //   "Skill 'type-fix-v3' available (92% success). Verify and include in plan."
    //   "3 past memories found for this function. Check recall_memory for context."
    //   "Cross-repo change affecting 5 nodes. Plan carefully, high risk."
    //   "Simple single-file change. Low risk."
}

// ============================================================
// CONFIGURATION
// ============================================================

type Config struct {
    DatabaseURL string

    // Developer's model preferences (from universe config)
    PremiumModel  ModelConfig
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
```

---

## File: `internal/orchestrator/plans.go` — NEW

```go
package orchestrator

// PlanStore handles all database operations for the plans table.
// This is the core of the two-agent bridge.

// NewPlanStore creates a new PlanStore.
func NewPlanStore(databaseURL string) (*PlanStore, error)

func (ps *PlanStore) Close() error

// ============================================================
// PLAN CRUD — used by MCP tools
// ============================================================

// StorePlan saves a new plan created by the planner agent.
// Called by the store_plan MCP tool.
//
// Parameters:
//   - plan: the plan to store (ID is generated by the database)
//
// Returns: the stored plan with generated ID
//
// Implementation:
//   INSERT INTO plans (developer_id, title, task_prompt, steps, ...)
//   RETURNING id, created_at
func (ps *PlanStore) StorePlan(plan Plan) (*Plan, error)

// GetLatestPlan retrieves the most recent pending plan for a developer.
// Called by the get_plan MCP tool (executor agent calls this).
//
// SQL:
//   SELECT * FROM plans
//   WHERE developer_id = $1
//     AND status = 'pending'
//   ORDER BY created_at DESC
//   LIMIT 1
//
// Also updates status to 'executing' when retrieved:
//   UPDATE plans SET status = 'executing' WHERE id = $1
func (ps *PlanStore) GetLatestPlan(developerID string) (*Plan, error)

// GetPlanByID retrieves a specific plan by UUID.
func (ps *PlanStore) GetPlanByID(id string) (*Plan, error)

// StorePlanResult saves the executor's result for a plan.
// Called by the store_plan_result MCP tool.
//
// SQL:
//   UPDATE plans SET
//     status = CASE WHEN $2 THEN 'completed' ELSE 'failed' END,
//     result_success = $2,
//     result_summary = $3,
//     result_files = $4,
//     result_tests = $5,
//     result_error = $6,
//     executed_at = NOW()
//   WHERE id = $1
func (ps *PlanStore) StorePlanResult(planID string, success bool, summary string, files []string, testsPassed bool, errorDetail string) error

// GetPlanResult retrieves the result of a plan for the planner to verify.
// Called by the get_plan_result MCP tool.
func (ps *PlanStore) GetPlanResult(planID string) (*Plan, error)

// VerifyPlan marks a plan as verified or rejected by the planner.
// Called when the planner reviews the executor's work.
//
// SQL:
//   UPDATE plans SET
//     status = CASE WHEN $2 THEN 'verified' ELSE 'rejected' END,
//     verified = $2,
//     verification_note = $3,
//     verified_at = NOW()
//   WHERE id = $1
func (ps *PlanStore) VerifyPlan(planID string, approved bool, note string) error

// ListPlans returns paginated plan summaries for a developer.
// Used by the dashboard.
//
// SQL:
//   SELECT id, title, status, jsonb_array_length(steps) as step_count,
//          skill_used IS NOT NULL as skill_used, cross_repo,
//          planner_model, executor_model, created_at
//   FROM plans
//   WHERE developer_id = $1
//   ORDER BY created_at DESC
//   LIMIT $2 OFFSET $3
func (ps *PlanStore) ListPlans(developerID string, limit int, offset int) ([]PlanSummary, int, error)

// GetPlanStats returns aggregate statistics.
type PlanStats struct {
    TotalPlans     int     `json:"total_plans"`
    Completed      int     `json:"completed"`
    Failed         int     `json:"failed"`
    Verified       int     `json:"verified"`
    Rejected       int     `json:"rejected"`
    Pending        int     `json:"pending"`
    SkillUsedCount int     `json:"skill_used_count"`
    CrossRepoCount int     `json:"cross_repo_count"`
    AvgStepsPerPlan float64 `json:"avg_steps_per_plan"`
}

func (ps *PlanStore) GetPlanStats(developerID string) (*PlanStats, error)
```

---

## File: `internal/orchestrator/router.go` — SIMPLIFY

**DELETE all the old routing logic (SkillMatcher interface, MemoryRecaller interface, GraphAnalyzer interface, Route function with 6 modes, ClassifyTaskType).**

**REPLACE with a simpler recommendation engine:**

```go
package orchestrator

// Router provides RECOMMENDATIONS to the planner agent.
// It does NOT make routing decisions — the planner (premium model in
// Cursor) reads the recommendations and decides what to do.
//
// The router checks:
//   1. Is there a matching skill? (via Engine 3)
//   2. Is there relevant memory? (via Engine 2)
//   3. What's the blast radius? (via Engine 1 graph)
//
// Then returns a RoutingRecommendation with all findings.
// Zero LLM calls. All database queries.

// Dependencies — same interfaces but used for recommendation, not routing
type SkillChecker interface {
    HasMatch(graphNodeIDs []string, taskText string) (bool, string, string, float64, error)
    // Returns: found, skillID, skillName, confidence, error
}

type MemoryChecker interface {
    HasRelevant(graphNodeIDs []string, developerID string) (bool, int, error)
    // Returns: found, count, error
}

type GraphChecker interface {
    CountAffectedNodes(nodeIDs []string) (int, error)
    IsCrossRepo(nodeIDs []string) (bool, error)
}

// NewRouter creates a new recommendation router.
func NewRouter(skills SkillChecker, memory MemoryChecker, graph GraphChecker) *Router

// Recommend analyzes a task and returns recommendations for the planner.
// Called by the MCP tool (or by store_plan to add context).
//
// Zero LLM calls. All database queries.
//
// Parameters:
//   - graphNodeIDs: affected graph nodes
//   - taskText: the developer's request
//   - developerID: for memory lookup
//
// Implementation:
//   1. Check skills: skills.HasMatch(graphNodeIDs, taskText)
//   2. Check memory: memory.HasRelevant(graphNodeIDs, developerID)
//   3. Check graph: graph.CountAffectedNodes + graph.IsCrossRepo
//   4. Calculate risk level:
//      - 1-2 nodes, same repo → "low"
//      - 3-5 nodes OR cross-repo → "medium"
//      - 6+ nodes AND cross-repo → "high"
//   5. Build recommendation text:
//      - If skill found: "Skill 'X' available (Y% confidence). Verify before using."
//      - If memory found: "N past observations found. Check recall_memory."
//      - Risk context: "Cross-repo change, 5 nodes affected. Plan carefully."
//   6. Return RoutingRecommendation
func (r *Router) Recommend(graphNodeIDs []string, taskText string, developerID string) (*RoutingRecommendation, error)

// ClassifyTaskType is kept but simplified — used for dashboard categorization.
// Determines what type of task this is based on keywords.
func ClassifyTaskType(prompt string) string {
    // "fix", "bug" → "code_fix"
    // "test" → "test_gen"
    // "refactor" → "refactor"
    // etc.
    // Used for dashboard grouping, not for routing decisions
}
```

---

## File: `internal/orchestrator/tracker.go` — MODIFY

**DELETE the old tracker that tracked internal LLM calls. REPLACE with plan-based tracking:**

```go
package orchestrator

// Tracker logs plan costs and provides dashboard data.
// Cost estimates are based on the developer's configured model pricing.

// NewTracker creates a new Tracker.
func NewTracker(databaseURL string) (*Tracker, error)

// LogPlanCost records the estimated cost of a plan execution.
// Called after a plan is completed (store_plan_result).
//
// Parameters:
//   - cost: the PlanCost record with estimated tokens and costs
//
// The estimates are calculated by the MCP tool:
//   estimatedPlannerTokens = len(plan.Steps) * avgTokensPerStep
//   estimatedExecutorTokens = len(plan.Steps) * avgTokensPerExecution
//   costs = tokens * model pricing from config
//   allPremiumCost = totalTokens * premiumModel pricing
//   savings = allPremiumCost - actualCost
func (t *Tracker) LogPlanCost(cost PlanCost) error

// ============================================================
// DASHBOARD QUERIES
// ============================================================

type MonthlySummary struct {
    Month         string  `json:"month"`
    TotalPlans    int     `json:"total_plans"`
    ActualCost    float64 `json:"actual_cost"`
    WouldHaveCost float64 `json:"would_have_cost"`
    Savings       float64 `json:"savings"`
    SavingsPercent float64 `json:"savings_percent"`
    SkillUses     int     `json:"skill_uses"`
    MemoryHits    int     `json:"memory_hits"`
    AvgCostPerPlan float64 `json:"avg_cost_per_plan"`
}

func (t *Tracker) GetMonthlySummary() ([]MonthlySummary, error)

type DeveloperSummary struct {
    Week       string  `json:"week"`
    TotalPlans int     `json:"total_plans"`
    ActualCost float64 `json:"actual_cost"`
    Savings    float64 `json:"savings"`
}

func (t *Tracker) GetDeveloperSummary(developerID string) ([]DeveloperSummary, error)

// RefreshViews refreshes the materialized views.
func (t *Tracker) RefreshViews() error
```

---

## File: `internal/orchestrator/workspace.go` — NEW

```go
package orchestrator

import (
    "encoding/json"
    "os"
    "path/filepath"
)

// WorkspaceGenerator creates .code-workspace files for the planner
// and executor Cursor windows, pre-configured with the developer's
// chosen models.

// GenerateWorkspaces creates both workspace files.
//
// Parameters:
//   - projectDir: the project root directory
//   - premiumModel: model name for the planner workspace
//   - executionModel: model name for the executor workspace
//
// Creates:
//   .universe/workspaces/planner.code-workspace
//   .universe/workspaces/executor.code-workspace
//
// Each workspace file:
//   - Points to the same project folder
//   - Sets "ai.model" to the appropriate model
//   - Sets "window.title" to identify which window is which
func GenerateWorkspaces(projectDir string, premiumModel string, executionModel string) error {
    wsDir := filepath.Join(projectDir, ".universe", "workspaces")
    os.MkdirAll(wsDir, 0755)

    // Planner workspace
    planner := map[string]interface{}{
        "folders": []map[string]string{
            {"path": "../.."},
        },
        "settings": map[string]string{
            "ai.model":     premiumModel,
            "window.title":  "🧠 Universe Planner — ${activeEditorShort}",
        },
    }

    // Executor workspace
    executor := map[string]interface{}{
        "folders": []map[string]string{
            {"path": "../.."},
        },
        "settings": map[string]string{
            "ai.model":     executionModel,
            "window.title":  "⚡ Universe Executor — ${activeEditorShort}",
        },
    }

    // Write planner workspace
    plannerPath := filepath.Join(wsDir, "planner.code-workspace")
    plannerJSON, _ := json.MarshalIndent(planner, "", "  ")
    os.WriteFile(plannerPath, plannerJSON, 0644)

    // Write executor workspace
    executorPath := filepath.Join(wsDir, "executor.code-workspace")
    executorJSON, _ := json.MarshalIndent(executor, "", "  ")
    os.WriteFile(executorPath, executorJSON, 0644)

    return nil
}

// OpenPlannerWorkspace opens the planner workspace in Cursor.
// Called by: universe plan
func OpenPlannerWorkspace(projectDir string) error {
    wsPath := filepath.Join(projectDir, ".universe", "workspaces", "planner.code-workspace")
    return openInCursor(wsPath)
}

// OpenExecutorWorkspace opens the executor workspace in Cursor.
// Called by: universe exec
// Also called by store_plan MCP tool when AutoOpenExecutor is true.
func OpenExecutorWorkspace(projectDir string) error {
    wsPath := filepath.Join(projectDir, ".universe", "workspaces", "executor.code-workspace")
    return openInCursor(wsPath)
}

// openInCursor launches Cursor with the given workspace file.
func openInCursor(workspacePath string) error {
    // Try "cursor" command first, then fall back to "code" (VS Code compatible)
    // exec.Command("cursor", workspacePath).Start()
    // If cursor not found: exec.Command("code", workspacePath).Start()
    return nil
}
```

---

## File: `internal/orchestrator/setup.go` — NEW

```go
package orchestrator

import (
    "encoding/json"
    "os"
    "path/filepath"
)

// Setup generates all configuration files for the two-agent workflow.
// Called by: universe setup

// RunSetup generates all config files based on the developer's choices.
//
// Parameters:
//   - projectDir: the project root
//   - premiumModel: chosen premium model name
//   - executionModel: chosen execution model name
//   - premiumCosts: pricing for premium model
//   - executionCosts: pricing for execution model
//   - dbURL: database URL (empty if not configured yet)
//
// Creates:
//   .universe/workspaces/planner.code-workspace
//   .universe/workspaces/executor.code-workspace
//   .cursor/rules/universe-planner.mdc
//   .cursor/rules/universe-executor.mdc
//   .cursor/rules/universe-compression.mdc
//   .cursor/mcp.json (if not already exists)
//   .vscode/settings.json (sets default model to execution model)
func RunSetup(projectDir string, premiumModel string, executionModel string, premiumCosts ModelConfig, executionCosts ModelConfig, dbURL string) error {
    // 1. Generate workspace files
    GenerateWorkspaces(projectDir, premiumModel, executionModel)

    // 2. Generate cursor rules
    generateCursorRules(projectDir, premiumModel, executionModel)

    // 3. Generate MCP config (if not exists)
    generateMCPConfig(projectDir)

    // 4. Set default model in VS Code settings
    setDefaultModel(projectDir, executionModel)

    return nil
}

// generateCursorRules creates the .cursor/rules/ directory and 3 rule files.
func generateCursorRules(projectDir string, premiumModel string, executionModel string) error {
    rulesDir := filepath.Join(projectDir, ".cursor", "rules")
    os.MkdirAll(rulesDir, 0755)

    // Planner rule
    plannerRule := `---
description: "Universe planning agent — use with your PREMIUM model (` + premiumModel + `)"
globs: ["**/*"]
alwaysApply: false
---

You are the PLANNING agent. Your job is to analyze, check, and plan — NOT to write code.

For every task:
1. Call get_impact_analysis to understand the blast radius
2. Call find_skill to check for existing recipes
3. If a skill is found (requires_verification = true):
   - READ the skill instruction carefully
   - VERIFY it matches the current code (check file paths, function names)
   - If the skill has stale_warning = true, be EXTRA careful
   - If verified: include skill steps in your plan
   - If outdated: ignore the skill, plan from scratch
4. Call recall_memory to check your past work on this code
5. Write a step-by-step plan (be specific: file paths, line numbers, exact changes)
6. Call store_plan to save the plan

Do NOT write code. Do NOT edit files. Only analyze and plan.
After storing the plan, tell the developer: "Plan stored. Switch to the executor window."
`
    os.WriteFile(filepath.Join(rulesDir, "universe-planner.mdc"), []byte(plannerRule), 0644)

    // Executor rule
    executorRule := `---
description: "Universe execution agent — use with your EXECUTION model (` + executionModel + `)"
globs: ["**/*"]
alwaysApply: false
---

You are the EXECUTION agent. Your job is to follow plans and write code — NOT to analyze or re-think.

When the developer says "execute", "run plan", or "do the task":
1. Call get_plan to retrieve the latest pending plan
2. Read each step carefully
3. Follow each step EXACTLY as written — do not deviate
4. Write the code changes
5. Run the tests if specified in the plan
6. Call store_plan_result with:
   - success: true/false
   - summary: what you did
   - files changed
   - tests passed or failed
   - error detail if anything went wrong

Do NOT re-analyze the problem. Do NOT question the plan. Do NOT skip steps.
After storing the result, tell the developer: "Done. Switch to the planner window to verify."
`
    os.WriteFile(filepath.Join(rulesDir, "universe-executor.mdc"), []byte(executorRule), 0644)

    // Compression rule (applies to ALL chats)
    compressionRule := `---
description: "Universe output compression — reduces token waste"
globs: ["**/*"]
alwaysApply: true
---

Output rules (apply to EVERY response):
- No "I'd be happy to help", "Sure!", "Great question!", "Let me help you with that"
- No "It might be worth considering", "Perhaps you could", "You may want to"
- No "Let me explain", "As you can see", "It's important to note that"
- Keep ALL code blocks, function names, variable names, error messages EXACT
- Max 2 sentences for explanations unless the developer asks for more
- If code alone answers the question, output code only — no surrounding prose
- For git commits and PR descriptions, write normally (not compressed)
`
    os.WriteFile(filepath.Join(rulesDir, "universe-compression.mdc"), []byte(compressionRule), 0644)

    return nil
}

// generateMCPConfig creates .cursor/mcp.json if it doesn't exist.
func generateMCPConfig(projectDir string) error {
    mcpPath := filepath.Join(projectDir, ".cursor", "mcp.json")

    // Don't overwrite existing config
    if _, err := os.Stat(mcpPath); err == nil {
        return nil // already exists
    }

    mcpConfig := map[string]interface{}{
        "mcpServers": map[string]interface{}{
            "universe": map[string]interface{}{
                "command": "universe",
                "args":    []string{"mcp", "--stdio"},
            },
        },
    }

    os.MkdirAll(filepath.Join(projectDir, ".cursor"), 0755)
    data, _ := json.MarshalIndent(mcpConfig, "", "  ")
    return os.WriteFile(mcpPath, data, 0644)
}

// setDefaultModel updates .vscode/settings.json with the execution model as default.
func setDefaultModel(projectDir string, executionModel string) error {
    settingsPath := filepath.Join(projectDir, ".vscode", "settings.json")
    os.MkdirAll(filepath.Join(projectDir, ".vscode"), 0755)

    // Read existing settings or create new
    settings := map[string]interface{}{}
    if data, err := os.ReadFile(settingsPath); err == nil {
        json.Unmarshal(data, &settings)
    }

    // Set the default model to the cheap one
    // (developer spends 80% of time in execution, default should be cheap)
    settings["ai.model"] = executionModel

    data, _ := json.MarshalIndent(settings, "", "  ")
    return os.WriteFile(settingsPath, data, 0644)
}
```

---

## File: `internal/orchestrator/templates.go` — KEEP with minor changes

**The spec templates (CodeFixSpec, TestGenSpec, etc.) are still useful.** They help structure the planner's output so the executor has clear instructions.

**ONLY CHANGE: update the description at the top:**

```go
// BEFORE:
// Each template defines the exact JSON structure Opus must produce.
// This prevents Opus from rambling and ensures Haiku can execute
// without interpretation.

// AFTER:
// Each template defines a structured format for plans.
// The planner agent (premium model in Cursor) can use these as a guide
// for writing clear, specific plans. The executor agent follows the plan
// step by step.
//
// These templates are NOT enforced by Universe. They're provided as
// MCP tool documentation so the planner knows what format works best.
// The planner can write plans in any format — these are suggestions.
```

**No other changes to templates.go.** The CodeFixSpec, TestGenSpec, PRGenSpec etc. all stay as-is.

---

## New MCP Tools (add to mcp-server.md patch)

```go
// ============================================================
// 4 new plan bridge tools
// ============================================================

// Tool: store_plan
// Called by: planner agent (premium model)
type StorePlanInput struct {
    Title         string   `json:"title" jsonschema:"required,description=Short title for the plan"`
    TaskPrompt    string   `json:"task_prompt" jsonschema:"required,description=The developer's original request"`
    Steps         []string `json:"steps" jsonschema:"required,description=Step-by-step instructions for the executor"`
    FilesToChange []string `json:"files_to_change,omitempty" jsonschema:"description=Files the executor should modify"`
    SkillUsed     string   `json:"skill_used,omitempty" jsonschema:"description=Skill ID if a skill was verified and used"`
    SkillVerified bool     `json:"skill_verified,omitempty" jsonschema:"description=Was the skill verified before including?"`
    GraphContext  string   `json:"graph_context,omitempty" jsonschema:"description=Blast radius summary"`
    AffectedNodes []string `json:"affected_nodes,omitempty" jsonschema:"description=Graph nodes affected"`
    RiskLevel     string   `json:"risk_level,omitempty" jsonschema:"description=low medium high"`
}

type StorePlanOutput struct {
    PlanID  string `json:"plan_id"`
    Message string `json:"message"`
}

// After saving the plan, if config.AutoOpenExecutor is true:
//   exec.Command("cursor", executorWorkspacePath).Start()

// Tool: get_plan
// Called by: executor agent (cheap model)
type GetPlanInput struct {
    PlanID string `json:"plan_id,omitempty" jsonschema:"description=Specific plan ID. Empty = get latest pending plan."`
}

type GetPlanOutput struct {
    Found   bool   `json:"found"`
    Plan    *Plan  `json:"plan,omitempty"`
    Message string `json:"message"`
}

// Tool: store_plan_result
// Called by: executor agent (cheap model)
type StorePlanResultInput struct {
    PlanID       string   `json:"plan_id" jsonschema:"required,description=The plan that was executed"`
    Success      bool     `json:"success" jsonschema:"required,description=Did execution succeed?"`
    Summary      string   `json:"summary" jsonschema:"required,description=What was done"`
    FilesChanged []string `json:"files_changed,omitempty" jsonschema:"description=Files actually modified"`
    TestsPassed  bool     `json:"tests_passed,omitempty" jsonschema:"description=Did tests pass?"`
    ErrorDetail  string   `json:"error_detail,omitempty" jsonschema:"description=Error detail if failed"`
}

type StorePlanResultOutput struct {
    Message string `json:"message"`
}

// Tool: get_plan_result
// Called by: planner agent (premium model) for verification
type GetPlanResultInput struct {
    PlanID string `json:"plan_id" jsonschema:"required,description=The plan to check results for"`
}

type GetPlanResultOutput struct {
    Found  bool   `json:"found"`
    Plan   *Plan  `json:"plan,omitempty"`  // includes result fields
    Message string `json:"message"`
}

// Tool: verify_plan
// Called by: planner agent (premium model) after reviewing result
type VerifyPlanInput struct {
    PlanID   string `json:"plan_id" jsonschema:"required"`
    Approved bool   `json:"approved" jsonschema:"required,description=true if result is correct"`
    Note     string `json:"note,omitempty" jsonschema:"description=Verification notes or rejection reason"`
}

type VerifyPlanOutput struct {
    Message string `json:"message"`
}
```

---

## Updated Testing

```go
package orchestrator

import "testing"

// ============================================================
// PLAN STORE TESTS
// ============================================================

// Test 1: StorePlan and GetPlanByID round-trip
func TestPlanStore_StoreAndGet(t *testing.T) {}

// Test 2: GetLatestPlan returns most recent pending plan
func TestPlanStore_GetLatest(t *testing.T) {}

// Test 3: GetLatestPlan updates status to executing
func TestPlanStore_GetLatestUpdatesStatus(t *testing.T) {}

// Test 4: StorePlanResult updates plan with result
func TestPlanStore_StoreResult(t *testing.T) {}

// Test 5: VerifyPlan marks plan as verified/rejected
func TestPlanStore_Verify(t *testing.T) {}

// Test 6: ListPlans returns paginated summaries
func TestPlanStore_ListPlans(t *testing.T) {}

// Test 7: Plan status transitions: pending → executing → completed → verified
func TestPlanStore_StatusTransitions(t *testing.T) {}

// ============================================================
// ROUTER TESTS (simplified — recommendations only)
// ============================================================

// Test 8: Recommend returns skill info when available
func TestRouter_SkillAvailable(t *testing.T) {}

// Test 9: Recommend returns memory info when available
func TestRouter_MemoryAvailable(t *testing.T) {}

// Test 10: Recommend calculates correct risk level
func TestRouter_RiskLevel(t *testing.T) {
    // 1-2 nodes, same repo → low
    // 3-5 nodes OR cross-repo → medium
    // 6+ AND cross-repo → high
}

// Test 11: Recommend with no skill and no memory
func TestRouter_NoSkillNoMemory(t *testing.T) {}

// ============================================================
// WORKSPACE TESTS
// ============================================================

// Test 12: GenerateWorkspaces creates both files
func TestGenerateWorkspaces(t *testing.T) {
    // Generate in temp dir
    // Verify planner.code-workspace exists and has correct ai.model
    // Verify executor.code-workspace exists and has correct ai.model
}

// Test 13: Workspace files point to correct project directory
func TestWorkspaces_CorrectPaths(t *testing.T) {}

// ============================================================
// SETUP TESTS
// ============================================================

// Test 14: RunSetup generates all files
func TestRunSetup(t *testing.T) {
    // Run setup in temp dir
    // Verify all 6 files created:
    //   .universe/workspaces/planner.code-workspace
    //   .universe/workspaces/executor.code-workspace
    //   .cursor/rules/universe-planner.mdc
    //   .cursor/rules/universe-executor.mdc
    //   .cursor/rules/universe-compression.mdc
    //   .cursor/mcp.json
}

// Test 15: RunSetup doesn't overwrite existing mcp.json
func TestRunSetup_PreserveMCPConfig(t *testing.T) {}

// Test 16: Cursor rules contain correct model names
func TestCursorRules_ModelNames(t *testing.T) {
    // Run setup with "gpt-4o" and "gpt-4o-mini"
    // Verify planner rule contains "gpt-4o"
    // Verify executor rule contains "gpt-4o-mini"
}

// ============================================================
// TRACKER TESTS
// ============================================================

// Test 17: LogPlanCost writes to database
func TestTracker_LogCost(t *testing.T) {}

// Test 18: GetMonthlySummary returns correct totals
func TestTracker_MonthlySummary(t *testing.T) {}

// Test 19: ClassifyTaskType identifies types correctly
func TestClassifyTaskType(t *testing.T) {
    // "fix the bug" → "code_fix"
    // "write tests" → "test_gen"
    // "refactor" → "refactor"
}
```

---

## Updated Acceptance Criteria

**REMOVE these from the old engine5.md:**
- ~~Planner produces valid structured JSON specs using templates~~
- ~~Executor executes sub-tasks using Haiku~~
- ~~Verifier implements all three tiers~~
- ~~Escalation chain works~~
- ~~Parallel executor runs independent sub-tasks~~
- ~~LLM client handles rate limits~~

**REPLACE with:**
- [ ] Plans table created with correct schema
- [ ] `StorePlan` and `GetPlanByID` round-trip correctly
- [ ] `GetLatestPlan` returns most recent pending and updates status to executing
- [ ] `StorePlanResult` updates plan with executor's result
- [ ] `VerifyPlan` marks as verified or rejected
- [ ] `ListPlans` returns paginated results
- [ ] Router `Recommend` returns skill/memory/risk info with zero LLM calls
- [ ] `GenerateWorkspaces` creates both workspace files with correct model settings
- [ ] `RunSetup` generates all 6 config files
- [ ] `RunSetup` doesn't overwrite existing mcp.json
- [ ] Cursor rules contain the developer's chosen model names
- [ ] Cost tracker logs plan costs and calculates savings estimates
- [ ] Materialized views produce correct dashboard summaries
- [ ] `store_plan` MCP tool auto-opens executor workspace (when configured)
- [ ] All 19 tests pass
- [ ] `go build ./...` succeeds
- [ ] No LLM API calls anywhere in the package
