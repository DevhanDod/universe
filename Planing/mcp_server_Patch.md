# MCP Server — Alignment Patch

## Apply These Changes to mcp-server.md

**What changed:** The MCP server gains 5 plan bridge tools (the two-agent communication layer), loses 1 tool (execute_task), and updates find_skill to include verification requirements. Total tools goes from 10 to 14.  
**No API keys needed.** All tools are database queries and file operations.

---

## Change Summary

| # | What | Action | Detail |
|---|------|--------|--------|
| 1 | `store_plan` tool | ADD | Planner stores a plan for the executor |
| 2 | `get_plan` tool | ADD | Executor retrieves the pending plan |
| 3 | `store_plan_result` tool | ADD | Executor reports what it did |
| 4 | `get_plan_result` tool | ADD | Planner retrieves result to verify |
| 5 | `verify_plan` tool | ADD | Planner approves or rejects the result |
| 6 | `execute_task` tool | REMOVE | Universe no longer orchestrates LLM calls |
| 7 | `find_skill` response | MODIFY | Add verification fields and guidance message |
| 8 | `recall_memory` description | MODIFY | Change to personal memory, remove team references |
| 9 | `store_observation` tool | MODIFY | Remove `shared` parameter |
| 10 | Tool count | UPDATE | Was 10, now 14 |
| 11 | Server description | UPDATE | Reflects plan-based two-agent architecture |
| 12 | ServerConfig | UPDATE | Add PlanStore, remove Orchestrator |

---

## Change 1: Update server description (Section 1)

**REPLACE the description in mcp-server.md section 1:**

```
BEFORE:
  When a developer uses Cursor and asks "fix the type mismatch", 
  Cursor's AI agent needs tools to answer that well.

AFTER:
  Universe MCP server is the shared brain between two Cursor windows — 
  the planner (premium model) and the executor (cheap model). Both 
  windows connect to the same MCP server. The planner stores plans, 
  the executor retrieves and follows them.

  The MCP server also provides the knowledge graph, personal memory, 
  and skill reference tools that both agents use.

  Universe never calls any LLM APIs. It stores data, serves queries, 
  and opens Cursor windows. All AI work happens inside Cursor.
```

---

## Change 2: Update ServerConfig (Section 4)

**In server.go, update the ServerConfig struct:**

```go
// BEFORE:
type ServerConfig struct {
    Version      string
    DatabaseURL  string
    Graph        *graph.Graph
    MemoryStore  *memory.Store
    Retriever    *memory.Retriever
    SessionMgr   *memory.SessionManager
    SkillStore   *skills.Store
    SkillMatcher *skills.Matcher
    SkillExec    *skills.Executor
    Orchestrator *orchestrator.Orchestrator  // ← REMOVE
}

// AFTER:
type ServerConfig struct {
    Version      string
    DatabaseURL  string

    // Engine 1: Knowledge Graph
    Graph        *graph.Graph

    // Engine 2: Personal Memory
    MemoryStore  *memory.Store
    Retriever    *memory.Retriever
    SessionMgr   *memory.SessionManager

    // Engine 3: Skills
    SkillStore   *skills.Store
    SkillMatcher *skills.Matcher
    SkillExec    *skills.Executor

    // Engine 5: Plan Bridge + Cost Tracking
    PlanStore    *orchestrator.PlanStore      // NEW
    Router       *orchestrator.Router         // NEW (recommendation engine)
    Tracker      *orchestrator.Tracker        // NEW (cost tracking)
    OrchestratorConfig *orchestrator.Config   // NEW (for workspace paths and model config)
}
```

---

## Change 3: Update Handlers struct (Section 4)

```go
// BEFORE:
type Handlers struct {
    graph        *graph.Graph
    memoryStore  *memory.Store
    retriever    *memory.Retriever
    sessionMgr   *memory.SessionManager
    skillStore   *skills.Store
    skillMatcher *skills.Matcher
    skillExec    *skills.Executor
    orchestrator *orchestrator.Orchestrator   // ← REMOVE
}

// AFTER:
type Handlers struct {
    graph        *graph.Graph
    memoryStore  *memory.Store
    retriever    *memory.Retriever
    sessionMgr   *memory.SessionManager
    skillStore   *skills.Store
    skillMatcher *skills.Matcher
    skillExec    *skills.Executor
    planStore    *orchestrator.PlanStore      // NEW
    router       *orchestrator.Router         // NEW
    tracker      *orchestrator.Tracker        // NEW
    orchConfig   *orchestrator.Config         // NEW
}
```

---

## Change 4: Update tool registration in RunStdio (Section 4)

**REMOVE this block:**

```go
// ========================================================
// Register Engine 5 tools: Orchestrator
// ========================================================
mcp.AddTool(server, &mcp.Tool{
    Name:        "get_cost_summary",
    Description: "Get cost and savings summary...",
}, h.HandleGetCostSummary)
```

**REPLACE with:**

```go
// ========================================================
// Register Engine 5 tools: Plan Bridge + Cost Tracking
// ========================================================
mcp.AddTool(server, &mcp.Tool{
    Name:        "store_plan",
    Description: "Store a plan created by the planning agent. The execution agent retrieves and follows it. After storing, the executor Cursor window may open automatically.",
}, h.HandleStorePlan)

mcp.AddTool(server, &mcp.Tool{
    Name:        "get_plan",
    Description: "Retrieve the latest pending plan to execute. Called by the execution agent. Returns the step-by-step plan created by the planning agent.",
}, h.HandleGetPlan)

mcp.AddTool(server, &mcp.Tool{
    Name:        "store_plan_result",
    Description: "Report the result of executing a plan. Called by the execution agent after completing (or failing) the plan steps.",
}, h.HandleStorePlanResult)

mcp.AddTool(server, &mcp.Tool{
    Name:        "get_plan_result",
    Description: "Retrieve the execution result for a plan. Called by the planning agent to review and verify the executor's work.",
}, h.HandleGetPlanResult)

mcp.AddTool(server, &mcp.Tool{
    Name:        "verify_plan",
    Description: "Approve or reject the execution result. Called by the planning agent after reviewing the result.",
}, h.HandleVerifyPlan)

mcp.AddTool(server, &mcp.Tool{
    Name:        "get_cost_summary",
    Description: "Get estimated cost savings — actual cost vs what it would have been if everything used the premium model.",
}, h.HandleGetCostSummary)
```

---

## Change 5: New file — `internal/mcpserver/tools_plans.go`

**CREATE this new file. It contains all 5 plan bridge tool handlers.**

```go
package mcpserver

import (
    "context"
    "fmt"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"
    "universe/internal/orchestrator"
)

// ============================================================
// Tool: store_plan
// Called by: planner agent (premium model in Cursor)
// ============================================================

type StorePlanInput struct {
    Title         string   `json:"title" jsonschema:"required,description=Short title for the plan"`
    TaskPrompt    string   `json:"task_prompt" jsonschema:"required,description=The developer's original request"`
    Steps         []string `json:"steps" jsonschema:"required,description=Step-by-step instructions for the executor"`
    FilesToChange []string `json:"files_to_change,omitempty" jsonschema:"description=Files the executor should modify"`
    SkillUsed     string   `json:"skill_used,omitempty" jsonschema:"description=Skill ID if a skill was verified and included in the plan"`
    SkillVerified bool     `json:"skill_verified,omitempty" jsonschema:"description=true if the planner verified the skill before including"`
    GraphContext  string   `json:"graph_context,omitempty" jsonschema:"description=Blast radius summary"`
    AffectedNodes []string `json:"affected_nodes,omitempty" jsonschema:"description=Graph node IDs affected by this change"`
    RiskLevel     string   `json:"risk_level,omitempty" jsonschema:"description=Risk assessment: low medium high"`

    // Cost estimation (self-reported by the planner)
    EstimatedPlannerTokens int `json:"estimated_planner_tokens,omitempty" jsonschema:"description=Approximate tokens used for planning"`
}

type StorePlanOutput struct {
    PlanID  string `json:"plan_id"`
    Message string `json:"message"`
}

// HandleStorePlan saves a plan to the database and optionally opens
// the executor Cursor window.
//
// Implementation:
//   1. Build a Plan struct from the input
//   2. Set planner_model from config (developer's chosen premium model)
//   3. Set executor_model from config (developer's chosen execution model)
//   4. Set status = "pending"
//   5. Call planStore.StorePlan(plan)
//   6. If orchConfig.AutoOpenExecutor is true:
//      Open the executor workspace via exec.Command("cursor", executorWorkspacePath)
//   7. Log cost estimate if tokens provided
//   8. Return the plan ID and message
func (h *Handlers) HandleStorePlan(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input StorePlanInput,
) (*mcp.CallToolResult, StorePlanOutput, error) {

    if h.planStore == nil {
        return nil, StorePlanOutput{
            Message: "Plan storage not available. Connect a database: universe config set db postgres://...",
        }, nil
    }

    // Build plan
    plan := orchestrator.Plan{
        DeveloperID:   getDeveloperID(ctx), // from session context or config
        Title:         input.Title,
        TaskPrompt:    input.TaskPrompt,
        Steps:         input.Steps,
        FilesToChange: input.FilesToChange,
        SkillUsed:     input.SkillUsed,
        SkillVerified: input.SkillVerified,
        GraphContext:  input.GraphContext,
        AffectedNodes: input.AffectedNodes,
        RiskLevel:     input.RiskLevel,
        Status:        orchestrator.PlanPending,
    }

    // Set model names from config
    if h.orchConfig != nil {
        plan.PlannerModel = h.orchConfig.PremiumModel.Name
        plan.ExecutorModel = h.orchConfig.ExecutionModel.Name
    }

    // Store the plan
    stored, err := h.planStore.StorePlan(plan)
    if err != nil {
        return nil, StorePlanOutput{}, fmt.Errorf("failed to store plan: %w", err)
    }

    // Auto-open executor workspace
    if h.orchConfig != nil && h.orchConfig.AutoOpenExecutor {
        wsPath := h.orchConfig.ExecutorWorkspacePath
        if wsPath != "" {
            go func() {
                // Fire and forget — don't block the tool response
                exec.Command("cursor", wsPath).Start()
            }()
        }
    }

    // Log cost estimate
    if input.EstimatedPlannerTokens > 0 && h.tracker != nil {
        go h.logPlannerCost(stored.ID, input.EstimatedPlannerTokens)
    }

    // Track in session manager (Engine 2)
    if h.sessionMgr != nil {
        h.sessionMgr.OnToolCall(
            getDeveloperID(ctx), "store_plan", "",
            input.Title, stored.ID, true, "",
        )
    }

    msg := fmt.Sprintf("Plan stored (ID: %s). %d steps.", stored.ID, len(input.Steps))
    if h.orchConfig != nil && h.orchConfig.AutoOpenExecutor {
        msg += " Executor window opening — tell the developer to switch to it and say 'execute'."
    } else {
        msg += " Tell the developer to open the executor window and say 'execute'."
    }

    return nil, StorePlanOutput{
        PlanID:  stored.ID,
        Message: msg,
    }, nil
}

// ============================================================
// Tool: get_plan
// Called by: executor agent (cheap model in Cursor)
// ============================================================

type GetPlanInput struct {
    PlanID string `json:"plan_id,omitempty" jsonschema:"description=Specific plan ID. Leave empty to get the latest pending plan."`
}

type GetPlanOutput struct {
    Found         bool     `json:"found"`
    PlanID        string   `json:"plan_id,omitempty"`
    Title         string   `json:"title,omitempty"`
    Steps         []string `json:"steps,omitempty"`
    FilesToChange []string `json:"files_to_change,omitempty"`
    SkillUsed     string   `json:"skill_used,omitempty"`
    GraphContext  string   `json:"graph_context,omitempty"`
    RiskLevel     string   `json:"risk_level,omitempty"`
    Message       string   `json:"message"`
}

// HandleGetPlan retrieves the latest pending plan for the executor.
// Also marks the plan status as "executing".
//
// Implementation:
//   1. If PlanID is provided, get that specific plan
//   2. If PlanID is empty, get the latest pending plan for this developer
//   3. If no pending plan found, return Found=false with helpful message
//   4. Update plan status to "executing"
//   5. Return the plan steps and context
func (h *Handlers) HandleGetPlan(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetPlanInput,
) (*mcp.CallToolResult, GetPlanOutput, error) {

    if h.planStore == nil {
        return nil, GetPlanOutput{
            Found:   false,
            Message: "Plan storage not available. Connect a database: universe config set db postgres://...",
        }, nil
    }

    var plan *orchestrator.Plan
    var err error

    if input.PlanID != "" {
        plan, err = h.planStore.GetPlanByID(input.PlanID)
    } else {
        plan, err = h.planStore.GetLatestPlan(getDeveloperID(ctx))
    }

    if err != nil || plan == nil {
        return nil, GetPlanOutput{
            Found:   false,
            Message: "No pending plan found. Ask the planning agent to create one first.",
        }, nil
    }

    return nil, GetPlanOutput{
        Found:         true,
        PlanID:        plan.ID,
        Title:         plan.Title,
        Steps:         plan.Steps,
        FilesToChange: plan.FilesToChange,
        SkillUsed:     plan.SkillUsed,
        GraphContext:  plan.GraphContext,
        RiskLevel:     plan.RiskLevel,
        Message:       fmt.Sprintf("Plan retrieved: '%s' (%d steps). Follow each step exactly.", plan.Title, len(plan.Steps)),
    }, nil
}

// ============================================================
// Tool: store_plan_result
// Called by: executor agent (cheap model in Cursor)
// ============================================================

type StorePlanResultInput struct {
    PlanID       string   `json:"plan_id" jsonschema:"required,description=The plan that was executed"`
    Success      bool     `json:"success" jsonschema:"required,description=Did execution succeed overall?"`
    Summary      string   `json:"summary" jsonschema:"required,description=Brief summary of what was done"`
    FilesChanged []string `json:"files_changed,omitempty" jsonschema:"description=Files actually modified"`
    TestsPassed  bool     `json:"tests_passed,omitempty" jsonschema:"description=Did the tests pass?"`
    ErrorDetail  string   `json:"error_detail,omitempty" jsonschema:"description=Error detail if execution failed"`

    // Cost estimation (self-reported by executor)
    EstimatedExecutorTokens int `json:"estimated_executor_tokens,omitempty" jsonschema:"description=Approximate tokens used for execution"`
}

type StorePlanResultOutput struct {
    Message string `json:"message"`
}

// HandleStorePlanResult saves the executor's result and logs cost.
//
// Implementation:
//   1. Call planStore.StorePlanResult(...)
//   2. Log cost estimate with tracker
//   3. If success and a skill was used: call skillExec.RecordExecution with success=true
//   4. Store observation in memory (Engine 2) about what was done
//   5. Return message telling the developer to switch to planner for verification
func (h *Handlers) HandleStorePlanResult(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input StorePlanResultInput,
) (*mcp.CallToolResult, StorePlanResultOutput, error) {

    if h.planStore == nil {
        return nil, StorePlanResultOutput{
            Message: "Plan storage not available.",
        }, nil
    }

    // Store the result
    err := h.planStore.StorePlanResult(
        input.PlanID,
        input.Success,
        input.Summary,
        input.FilesChanged,
        input.TestsPassed,
        input.ErrorDetail,
    )
    if err != nil {
        return nil, StorePlanResultOutput{}, fmt.Errorf("failed to store result: %w", err)
    }

    // Log cost estimate
    if input.EstimatedExecutorTokens > 0 && h.tracker != nil {
        go h.logExecutorCost(input.PlanID, input.EstimatedExecutorTokens)
    }

    // If a skill was used, report the execution result to Engine 3
    plan, _ := h.planStore.GetPlanByID(input.PlanID)
    if plan != nil && plan.SkillUsed != "" && h.skillExec != nil {
        go h.skillExec.RecordExecution(plan.SkillUsed, skills.SkillExecution{
            SkillID:     plan.SkillUsed,
            DeveloperID: getDeveloperID(ctx),
            Success:     input.Success,
            ErrorDetail: input.ErrorDetail,
        })
    }

    // Store observation in personal memory (Engine 2)
    if h.sessionMgr != nil {
        h.sessionMgr.OnToolCall(
            getDeveloperID(ctx), "store_plan_result", "",
            input.Summary, input.PlanID, input.Success, "",
        )
    }

    msg := "Result stored."
    if input.Success {
        msg += " Execution succeeded. Tell the developer to switch to the planner window and say 'verify'."
    } else {
        msg += " Execution failed. Tell the developer to switch to the planner window to review the error."
    }

    return nil, StorePlanResultOutput{Message: msg}, nil
}

// ============================================================
// Tool: get_plan_result
// Called by: planner agent (premium model in Cursor)
// ============================================================

type GetPlanResultInput struct {
    PlanID string `json:"plan_id,omitempty" jsonschema:"description=Plan ID to check. Empty = get the latest completed/failed plan."`
}

type GetPlanResultOutput struct {
    Found           bool     `json:"found"`
    PlanID          string   `json:"plan_id,omitempty"`
    Title           string   `json:"title,omitempty"`
    Status          string   `json:"status,omitempty"`
    Steps           []string `json:"steps,omitempty"`
    ResultSuccess   bool     `json:"result_success"`
    ResultSummary   string   `json:"result_summary,omitempty"`
    ResultFiles     []string `json:"result_files,omitempty"`
    ResultTests     bool     `json:"result_tests"`
    ResultError     string   `json:"result_error,omitempty"`
    SkillUsed       string   `json:"skill_used,omitempty"`
    Message         string   `json:"message"`
}

// HandleGetPlanResult retrieves the execution result for verification.
//
// Implementation:
//   1. If PlanID provided, get that plan
//   2. If empty, get the latest completed/failed plan for this developer
//   3. Return the plan + result for the planner to review
func (h *Handlers) HandleGetPlanResult(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input GetPlanResultInput,
) (*mcp.CallToolResult, GetPlanResultOutput, error) {

    if h.planStore == nil {
        return nil, GetPlanResultOutput{
            Found: false,
            Message: "Plan storage not available.",
        }, nil
    }

    var plan *orchestrator.Plan
    var err error

    if input.PlanID != "" {
        plan, err = h.planStore.GetPlanByID(input.PlanID)
    } else {
        // Get latest plan that has a result (completed or failed)
        plan, err = h.planStore.GetLatestCompletedPlan(getDeveloperID(ctx))
    }

    if err != nil || plan == nil {
        return nil, GetPlanResultOutput{
            Found:   false,
            Message: "No completed plan found. The executor may still be working.",
        }, nil
    }

    success := false
    if plan.ResultSuccess != nil {
        success = *plan.ResultSuccess
    }
    tests := false
    if plan.ResultTests != nil {
        tests = *plan.ResultTests
    }

    return nil, GetPlanResultOutput{
        Found:         true,
        PlanID:        plan.ID,
        Title:         plan.Title,
        Status:        string(plan.Status),
        Steps:         plan.Steps,
        ResultSuccess: success,
        ResultSummary: plan.ResultSummary,
        ResultFiles:   plan.ResultFiles,
        ResultTests:   tests,
        ResultError:   plan.ResultError,
        SkillUsed:     plan.SkillUsed,
        Message:       fmt.Sprintf("Plan '%s': %s. Review the result and call verify_plan to approve or reject.", plan.Title, plan.Status),
    }, nil
}

// ============================================================
// Tool: verify_plan
// Called by: planner agent (premium model in Cursor)
// ============================================================

type VerifyPlanInput struct {
    PlanID   string `json:"plan_id" jsonschema:"required,description=The plan to verify"`
    Approved bool   `json:"approved" jsonschema:"required,description=true if the result is correct and approved"`
    Note     string `json:"note,omitempty" jsonschema:"description=Verification notes or rejection reason"`
}

type VerifyPlanOutput struct {
    Message string `json:"message"`
}

// HandleVerifyPlan marks a plan as verified (approved) or rejected.
//
// Implementation:
//   1. Call planStore.VerifyPlan(planID, approved, note)
//   2. If approved and skill was used: boost the skill's confidence
//   3. If rejected and skill was used: report skill failure
//   4. Store verification as a memory observation (Engine 2)
//   5. Return confirmation message
func (h *Handlers) HandleVerifyPlan(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input VerifyPlanInput,
) (*mcp.CallToolResult, VerifyPlanOutput, error) {

    if h.planStore == nil {
        return nil, VerifyPlanOutput{Message: "Plan storage not available."}, nil
    }

    err := h.planStore.VerifyPlan(input.PlanID, input.Approved, input.Note)
    if err != nil {
        return nil, VerifyPlanOutput{}, fmt.Errorf("failed to verify: %w", err)
    }

    // Get plan for skill feedback
    plan, _ := h.planStore.GetPlanByID(input.PlanID)

    // Report to Engine 3 (skills)
    if plan != nil && plan.SkillUsed != "" && h.skillExec != nil {
        if input.Approved {
            // Skill worked and was verified — boost confidence
            go h.skillExec.RecordExecution(plan.SkillUsed, skills.SkillExecution{
                SkillID: plan.SkillUsed,
                Success: true,
            })
        } else {
            // Plan was rejected — skill may have been part of the problem
            go h.skillExec.RecordExecution(plan.SkillUsed, skills.SkillExecution{
                SkillID:     plan.SkillUsed,
                Success:     false,
                ErrorDetail: "Plan rejected during verification: " + input.Note,
            })
        }
    }

    msg := ""
    if input.Approved {
        msg = "Plan verified and approved. ✓"
    } else {
        msg = "Plan rejected. Reason: " + input.Note + ". Create a new plan to retry."
    }

    return nil, VerifyPlanOutput{Message: msg}, nil
}

// ============================================================
// HELPERS
// ============================================================

// getDeveloperID extracts the developer ID from the session context or config.
// In V1, this comes from the UNIVERSE_DEVELOPER_ID environment variable
// or the config file.
func getDeveloperID(ctx context.Context) string {
    // Check environment variable first
    if id := os.Getenv("UNIVERSE_DEVELOPER_ID"); id != "" {
        return id
    }
    // Fall back to config
    cfg := LoadConfig()
    if cfg.DeveloperID != "" {
        return cfg.DeveloperID
    }
    // Fall back to OS username
    if user, err := os.Hostname(); err == nil {
        return user
    }
    return "unknown"
}

// logPlannerCost estimates and logs the planner's cost.
func (h *Handlers) logPlannerCost(planID string, tokens int) {
    if h.tracker == nil || h.orchConfig == nil {
        return
    }
    cost := float64(tokens) * h.orchConfig.PremiumModel.InputCostPerM / 1_000_000
    // Rough estimate: assume 70% input, 30% output for planning
    outputCost := float64(tokens) * 0.3 * h.orchConfig.PremiumModel.OutputCostPerM / 1_000_000
    totalCost := cost + outputCost

    h.tracker.LogPlanCost(orchestrator.PlanCost{
        PlanID:                 planID,
        DeveloperID:            getDeveloperID(context.Background()),
        PlannerModel:           h.orchConfig.PremiumModel.Name,
        EstimatedPlannerTokens: tokens,
        EstimatedPlannerCost:   totalCost,
    })
}

// logExecutorCost estimates and logs the executor's cost.
func (h *Handlers) logExecutorCost(planID string, tokens int) {
    if h.tracker == nil || h.orchConfig == nil {
        return
    }
    cost := float64(tokens) * h.orchConfig.ExecutionModel.InputCostPerM / 1_000_000
    outputCost := float64(tokens) * 0.5 * h.orchConfig.ExecutionModel.OutputCostPerM / 1_000_000
    totalCost := cost + outputCost

    // Calculate what it would have cost on premium
    premiumCost := float64(tokens) * h.orchConfig.PremiumModel.InputCostPerM / 1_000_000
    premiumOutputCost := float64(tokens) * 0.5 * h.orchConfig.PremiumModel.OutputCostPerM / 1_000_000
    allPremiumCost := premiumCost + premiumOutputCost

    h.tracker.LogPlanCost(orchestrator.PlanCost{
        PlanID:                  planID,
        DeveloperID:             getDeveloperID(context.Background()),
        ExecutorModel:           h.orchConfig.ExecutionModel.Name,
        EstimatedExecutorTokens: tokens,
        EstimatedExecutorCost:   totalCost,
        EstimatedAllPremiumCost: allPremiumCost,
        EstimatedSavings:        allPremiumCost - totalCost,
    })
}
```

---

## Change 6: Update `find_skill` in tools_skills.go

**REPLACE the HandleFindSkill function and output type:**

```go
// BEFORE:
type FindSkillOutput struct {
    Found              bool
    SkillID            string
    SkillName          string
    Version            int
    Instruction        string
    SuccessRate        float64
    Confidence         float64
    ExplorationSkipped bool
    Message            string
}

// AFTER:
type FindSkillOutput struct {
    Found              bool    `json:"found"`
    SkillID            string  `json:"skill_id,omitempty"`
    SkillName          string  `json:"skill_name,omitempty"`
    Version            int     `json:"version,omitempty"`
    Instruction        string  `json:"instruction,omitempty"`
    SuccessRate        float64 `json:"success_rate,omitempty"`
    Confidence         float64 `json:"confidence,omitempty"`
    TimesApplied       int     `json:"times_applied,omitempty"`
    ExplorationSkipped bool    `json:"exploration_skipped"`
    Message            string  `json:"message"`

    // Verification requirement — ALWAYS true when a skill is found
    RequiresVerification bool   `json:"requires_verification"`
    VerificationPrompt   string `json:"verification_prompt,omitempty"`
    StaleWarning         bool   `json:"stale_warning"`
    LastUpdated          string `json:"last_updated,omitempty"`
}
```

**UPDATE the return logic when a skill is found:**

```go
// BEFORE:
return nil, FindSkillOutput{
    Found:       true,
    Instruction: skill.Instruction,
    Message:     "Skill found. Follow the instruction below for this task.",
}, nil

// AFTER:
return nil, FindSkillOutput{
    Found:                true,
    SkillID:              skill.ID,
    SkillName:            skill.Name,
    Version:              skill.Version,
    Instruction:          skill.Instruction,
    SuccessRate:          result.BestMatch.SuccessRate,
    Confidence:           result.BestMatch.Confidence,
    TimesApplied:         skill.TimesApplied,
    RequiresVerification: true,
    VerificationPrompt:   result.BestMatch.VerificationPrompt,
    StaleWarning:         hasGraphChangedTag(skill),
    LastUpdated:          skill.CreatedAt.Format("2006-01-02"),
    Message: "Skill found but requires YOUR verification before use. " +
             "You are the planning agent (premium model). Review this " +
             "skill instruction against the current code. If it's correct, " +
             "include its steps in your plan via store_plan. If it's " +
             "outdated, ignore it and plan from scratch.",
}, nil
```

**UPDATE the tool description:**

```go
// BEFORE:
mcp.AddTool(server, &mcp.Tool{
    Name:        "find_skill",
    Description: "Search for a matching skill recipe for the current task. If found, follow the skill instruction instead of reasoning from scratch — it saves tokens and time.",
}, h.HandleFindSkill)

// AFTER:
mcp.AddTool(server, &mcp.Tool{
    Name:        "find_skill",
    Description: "Search for a matching skill recipe. If found, the PLANNING agent (premium model) must verify the skill is still correct before including it in a plan. Skills are reference knowledge — never use them without verification.",
}, h.HandleFindSkill)
```

---

## Change 7: Update `recall_memory` in tools_memory.go

**UPDATE the tool registration description:**

```go
// BEFORE:
mcp.AddTool(server, &mcp.Tool{
    Name:        "recall_memory",
    Description: "Search past observations from the memory store. Returns compact summaries. Use get_observation_details to load full details for specific IDs.",
}, h.HandleRecallMemory)

// AFTER:
mcp.AddTool(server, &mcp.Tool{
    Name:        "recall_memory",
    Description: "Search YOUR past observations from previous sessions. Returns compact summaries of your own fixes, patterns, decisions, and conventions. Only your personal memory is searched — not other developers'.",
}, h.HandleRecallMemory)
```

---

## Change 8: Update `store_observation` in tools_memory.go

**REMOVE the `shared` field from StoreObservationInput:**

```go
// BEFORE:
type StoreObservationInput struct {
    GraphNodeID string `json:"graph_node_id" jsonschema:"required,..."`
    Category    string `json:"category" jsonschema:"required,..."`
    Content     string `json:"content" jsonschema:"required,..."`
    Shared      bool   `json:"shared,omitempty" jsonschema:"description=Make visible to the whole team (default false)"`
}

// AFTER:
type StoreObservationInput struct {
    GraphNodeID string `json:"graph_node_id" jsonschema:"required,description=The graph node this observation relates to"`
    Category    string `json:"category" jsonschema:"required,description=Category: fix pattern decision failure convention"`
    Content     string `json:"content" jsonschema:"required,description=The observation text. Saved to your personal memory."`
    // shared field REMOVED — all observations are personal in V1
}
```

**UPDATE the tool description:**

```go
// BEFORE:
mcp.AddTool(server, &mcp.Tool{
    Name:        "store_observation",
    Description: "Manually store an observation — a pattern, decision, convention, or fix that should be remembered across sessions.",
}, h.HandleStoreObservation)

// AFTER:
mcp.AddTool(server, &mcp.Tool{
    Name:        "store_observation",
    Description: "Save an observation to your personal memory — a pattern, decision, convention, or fix from this session. Will be recalled in your future sessions when you work on the same code.",
}, h.HandleStoreObservation)
```

---

## Change 9: Update `get_cost_summary` in tools_orchestrator.go

**UPDATE to reflect plan-based cost tracking:**

```go
// BEFORE:
type GetCostSummaryOutput struct {
    ActualCost      float64
    WouldHaveCost   float64
    ...
    RoutingBreakdown map[string]int
}

// AFTER:
type GetCostSummaryOutput struct {
    ActualCost       float64            `json:"actual_cost"`
    WouldHaveCost    float64            `json:"would_have_cost"`
    SavingsUSD       float64            `json:"savings_usd"`
    SavingsPercent   float64            `json:"savings_percent"`
    TotalPlans       int                `json:"total_plans"`
    SkillUses        int                `json:"skill_uses"`
    MemoryHits       int                `json:"memory_hits"`
    AvgCostPerPlan   float64            `json:"avg_cost_per_plan"`
    PremiumModel     string             `json:"premium_model"`
    ExecutionModel   string             `json:"execution_model"`
    Message          string             `json:"message"`
}
```

---

## Change 10: Add `GetLatestCompletedPlan` to PlanStore

**The get_plan_result tool needs to find the latest completed/failed plan. Add this function to plans.go in the Engine 5 patch:**

```go
// GetLatestCompletedPlan retrieves the most recent plan with a result
// (status = completed or failed) for verification.
//
// SQL:
//   SELECT * FROM plans
//   WHERE developer_id = $1
//     AND status IN ('completed', 'failed')
//     AND verified IS NULL
//   ORDER BY executed_at DESC
//   LIMIT 1
func (ps *PlanStore) GetLatestCompletedPlan(developerID string) (*Plan, error)
```

---

## Change 11: Update the CLI integration (mcp.go)

**In cmd/universe/mcp.go, update the engine initialization:**

```go
// BEFORE:
// Orchestrator was initialized and passed to ServerConfig

// AFTER:
// Initialize plan store, router, and tracker instead

// Engine 5: Plan Bridge + Cost Tracking
var planStore *orchestrator.PlanStore
var router *orchestrator.Router
var tracker *orchestrator.Tracker

if dbURL != "" {
    planStore, err = orchestrator.NewPlanStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Warning: plan store unavailable: %v\n", err)
    }

    // Router needs skill checker and memory checker interfaces
    if skillMatcher != nil && retriever != nil {
        router = orchestrator.NewRouter(skillMatcher, retriever, graphAnalyzer)
    }

    tracker, err = orchestrator.NewTracker(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Warning: cost tracker unavailable: %v\n", err)
    }
}

// Build server config
config := mcpserver.ServerConfig{
    Version:      Version,
    Graph:        g,
    MemoryStore:  memStore,
    Retriever:    retriever,
    SessionMgr:   sessionMgr,
    SkillStore:   skillStore,
    SkillMatcher: skillMatcher,
    SkillExec:    skillExec,
    PlanStore:    planStore,       // NEW
    Router:       router,          // NEW
    Tracker:      tracker,         // NEW
    OrchestratorConfig: &orchConfig, // NEW (loaded from universe config)
}
```

---

## Updated Tool Count

```
Engine 1 (Graph):        3 tools  (get_dependencies, get_impact_analysis, search_graph)
Engine 2 (Memory):       3 tools  (recall_memory, get_observation_details, store_observation)
Engine 3 (Skills):       4 tools  (find_skill, report_skill_execution, list_skills, get_skill_lineage)
Engine 5 (Plan Bridge):  5 tools  (store_plan, get_plan, store_plan_result, get_plan_result, verify_plan)
Engine 5 (Tracking):     1 tool   (get_cost_summary)
                        ─────────
Total:                  16 tools
```

---

## Updated Testing

**ADD these tests to mcpserver_test.go:**

```go
// Test 13: store_plan saves and returns plan_id
func TestHandleStorePlan(t *testing.T) {
    // Call with valid input
    // Verify plan_id returned
    // Verify plan exists in database with status "pending"
}

// Test 14: get_plan retrieves latest pending plan
func TestHandleGetPlan(t *testing.T) {
    // Store a plan
    // Call get_plan with empty plan_id
    // Verify returns the plan with correct steps
    // Verify plan status changed to "executing"
}

// Test 15: store_plan_result updates plan with result
func TestHandleStorePlanResult(t *testing.T) {
    // Store a plan, get it (sets to executing)
    // Store result with success=true
    // Verify plan status is "completed"
}

// Test 16: get_plan_result returns completed plan for verification
func TestHandleGetPlanResult(t *testing.T) {
    // Complete a plan
    // Call get_plan_result
    // Verify result fields are populated
}

// Test 17: verify_plan marks as verified or rejected
func TestHandleVerifyPlan(t *testing.T) {
    // Complete a plan
    // Call verify_plan with approved=true
    // Verify status is "verified"
    // Repeat with approved=false
    // Verify status is "rejected"
}

// Test 18: Full round-trip: store → get → result → verify
func TestPlanFullRoundTrip(t *testing.T) {
    // store_plan → plan_id
    // get_plan → returns steps
    // store_plan_result → success
    // get_plan_result → result visible
    // verify_plan → approved
    // Final status: "verified"
}

// Test 19: get_plan returns "no pending plan" when none exists
func TestHandleGetPlan_NoPending(t *testing.T) {}

// Test 20: find_skill returns requires_verification=true
func TestHandleFindSkill_RequiresVerification(t *testing.T) {
    // Find a skill
    // Verify RequiresVerification == true
    // Verify VerificationPrompt is not empty
}

// Test 21: store_observation has no shared field
func TestHandleStoreObservation_NoShared(t *testing.T) {
    // Store an observation
    // Verify it's stored as personal (developer-scoped)
}

// Test 22: Tool count is 16
func TestToolCount(t *testing.T) {
    // Create server
    // Count registered tools
    // Verify: 16 tools
}
```

---

## Updated Acceptance Criteria

- [ ] 16 MCP tools registered (3 graph + 3 memory + 4 skills + 5 plan + 1 cost)
- [ ] `store_plan` saves plan to database and returns plan_id
- [ ] `store_plan` auto-opens executor workspace when configured
- [ ] `get_plan` returns latest pending plan and sets status to "executing"
- [ ] `get_plan` returns helpful message when no plan exists
- [ ] `store_plan_result` updates plan with executor's result
- [ ] `store_plan_result` reports skill execution to Engine 3
- [ ] `store_plan_result` stores observation in Engine 2
- [ ] `get_plan_result` returns the completed plan for verification
- [ ] `verify_plan` marks plan as verified or rejected
- [ ] `verify_plan` updates skill confidence based on outcome
- [ ] `find_skill` always returns `requires_verification: true`
- [ ] `find_skill` includes verification prompt with skill metadata
- [ ] `recall_memory` only returns personal observations (developer-scoped)
- [ ] `store_observation` has no `shared` parameter
- [ ] Cost estimation logged for both planner and executor steps
- [ ] Graceful degradation: all plan tools return helpful message when no DB
- [ ] Full round-trip test passes: store → get → result → verify
- [ ] All 22 tests pass
