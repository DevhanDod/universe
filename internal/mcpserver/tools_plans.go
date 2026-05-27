package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/orchestrator"
	"github.com/Universe/universe/internal/skills"
)

// getDeveloperID returns the developer identifier from env or falls back to "cursor-agent".
func getDeveloperID(_ context.Context) string {
	if id := os.Getenv("UNIVERSE_DEVELOPER_ID"); id != "" {
		return id
	}
	return "cursor-agent"
}

// ============================================================
// Tool: store_plan
// Called by: planner agent (premium model)
// ============================================================

type StorePlanInput struct {
	Title                  string   `json:"title"`
	TaskPrompt             string   `json:"task_prompt"`
	Steps                  []string `json:"steps"`
	FilesToChange          []string `json:"files_to_change,omitempty"`
	SkillUsed              string   `json:"skill_used,omitempty"`
	SkillVerified          bool     `json:"skill_verified,omitempty"`
	GraphContext           string   `json:"graph_context,omitempty"`
	AffectedNodes          []string `json:"affected_nodes,omitempty"`
	RiskLevel              string   `json:"risk_level,omitempty"`
	EstimatedPlannerTokens int      `json:"estimated_planner_tokens,omitempty"`
}

type StorePlanOutput struct {
	PlanID  string `json:"plan_id"`
	Message string `json:"message"`
}

func (h *Handlers) HandleStorePlan(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input StorePlanInput,
) (*mcp.CallToolResult, StorePlanOutput, error) {
	if h.planStore == nil {
		return nil, StorePlanOutput{
			Message: "Plan store not available. Connect a database: universe config set db postgres://...",
		}, nil
	}
	if input.Title == "" || input.TaskPrompt == "" || len(input.Steps) == 0 {
		return nil, StorePlanOutput{
			Message: "title, task_prompt, and steps are required.",
		}, nil
	}

	plan := orchestrator.Plan{
		DeveloperID:   getDeveloperID(ctx),
		Title:         input.Title,
		TaskPrompt:    input.TaskPrompt,
		Steps:         input.Steps,
		FilesToChange: input.FilesToChange,
		SkillUsed:     input.SkillUsed,
		SkillVerified: input.SkillVerified,
		GraphContext:  input.GraphContext,
		AffectedNodes: input.AffectedNodes,
		RiskLevel:     input.RiskLevel,
	}

	if h.orchConfig != nil {
		plan.PlannerModel = h.orchConfig.PremiumModel.Name
		plan.ExecutorModel = h.orchConfig.ExecutionModel.Name
	}

	stored, err := h.planStore.StorePlan(plan)
	if err != nil {
		return nil, StorePlanOutput{}, err
	}

	// Auto-open executor workspace
	if h.orchConfig != nil && h.orchConfig.AutoOpenExecutor && h.orchConfig.ExecutorWorkspacePath != "" {
		wsPath := h.orchConfig.ExecutorWorkspacePath
		go exec.Command("cursor", wsPath).Start() //nolint:errcheck
	}

	// Log planner cost estimate asynchronously
	if input.EstimatedPlannerTokens > 0 && h.tracker != nil {
		go h.logPlannerCost(stored.ID, input.EstimatedPlannerTokens)
	}

	if h.sessionMgr != nil {
		h.sessionMgr.OnToolCall(
			getDeveloperID(ctx), "store_plan", "",
			input.Title, stored.ID, true, "",
		)
	}

	msg := fmt.Sprintf("Plan stored (ID: %s, %d steps).", stored.ID, len(stored.Steps))
	if h.orchConfig != nil && h.orchConfig.AutoOpenExecutor {
		msg += " Executor window opening — tell the developer to switch to it and say 'execute'."
	} else {
		msg += " Tell the developer to open the executor window and say 'execute'."
	}

	return nil, StorePlanOutput{PlanID: stored.ID, Message: msg}, nil
}

// ============================================================
// Tool: get_plan
// Called by: executor agent (cheap model)
// ============================================================

type GetPlanInput struct {
	PlanID string `json:"plan_id,omitempty"`
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

func (h *Handlers) HandleGetPlan(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetPlanInput,
) (*mcp.CallToolResult, GetPlanOutput, error) {
	if h.planStore == nil {
		return nil, GetPlanOutput{
			Found:   false,
			Message: "Plan store not available. Connect a database.",
		}, nil
	}

	var plan *orchestrator.Plan
	var err error

	if input.PlanID != "" {
		plan, err = h.planStore.GetPlanByID(input.PlanID)
	} else {
		plan, err = h.planStore.GetLatestPlan(getDeveloperID(ctx))
	}

	if err != nil {
		return nil, GetPlanOutput{}, err
	}
	if plan == nil {
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
// Called by: executor agent (cheap model)
// ============================================================

type StorePlanResultInput struct {
	PlanID                  string   `json:"plan_id"`
	Success                 bool     `json:"success"`
	Summary                 string   `json:"summary"`
	FilesChanged            []string `json:"files_changed,omitempty"`
	TestsPassed             bool     `json:"tests_passed,omitempty"`
	ErrorDetail             string   `json:"error_detail,omitempty"`
	EstimatedExecutorTokens int      `json:"estimated_executor_tokens,omitempty"`
}

type StorePlanResultOutput struct {
	Message string `json:"message"`
}

func (h *Handlers) HandleStorePlanResult(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input StorePlanResultInput,
) (*mcp.CallToolResult, StorePlanResultOutput, error) {
	if h.planStore == nil {
		return nil, StorePlanResultOutput{Message: "Plan store not available."}, nil
	}
	if input.PlanID == "" {
		return nil, StorePlanResultOutput{Message: "plan_id is required."}, nil
	}

	if err := h.planStore.StorePlanResult(
		input.PlanID, input.Success, input.Summary,
		input.FilesChanged, input.TestsPassed, input.ErrorDetail,
	); err != nil {
		return nil, StorePlanResultOutput{}, err
	}

	// Log executor cost estimate asynchronously
	if input.EstimatedExecutorTokens > 0 && h.tracker != nil {
		go h.logExecutorCost(input.PlanID, input.EstimatedExecutorTokens)
	}

	// Report skill execution to Engine 3
	plan, _ := h.planStore.GetPlanByID(input.PlanID)
	if plan != nil && plan.SkillUsed != "" && h.skillExec != nil {
		go func() { //nolint:errcheck
			h.skillExec.RecordExecution(plan.SkillUsed, skills.SkillExecution{
				SkillID:     plan.SkillUsed,
				DeveloperID: getDeveloperID(ctx),
				Success:     input.Success,
				ErrorDetail: input.ErrorDetail,
			})
		}()
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
// Called by: planner agent (premium model) for verification
// ============================================================

type GetPlanResultInput struct {
	PlanID string `json:"plan_id,omitempty"`
}

type GetPlanResultOutput struct {
	Found         bool     `json:"found"`
	PlanID        string   `json:"plan_id,omitempty"`
	Title         string   `json:"title,omitempty"`
	Status        string   `json:"status,omitempty"`
	Steps         []string `json:"steps,omitempty"`
	ResultSuccess bool     `json:"result_success"`
	ResultSummary string   `json:"result_summary,omitempty"`
	ResultFiles   []string `json:"result_files,omitempty"`
	ResultTests   bool     `json:"result_tests"`
	ResultError   string   `json:"result_error,omitempty"`
	SkillUsed     string   `json:"skill_used,omitempty"`
	Message       string   `json:"message"`
}

func (h *Handlers) HandleGetPlanResult(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetPlanResultInput,
) (*mcp.CallToolResult, GetPlanResultOutput, error) {
	if h.planStore == nil {
		return nil, GetPlanResultOutput{
			Found:   false,
			Message: "Plan store not available.",
		}, nil
	}

	var plan *orchestrator.Plan
	var err error

	if input.PlanID != "" {
		plan, err = h.planStore.GetPlanByID(input.PlanID)
	} else {
		plan, err = h.planStore.GetLatestCompletedPlan(getDeveloperID(ctx))
	}

	if err != nil {
		return nil, GetPlanResultOutput{}, err
	}
	if plan == nil {
		return nil, GetPlanResultOutput{
			Found:   false,
			Message: "No completed plan found. The executor may still be working.",
		}, nil
	}

	if plan.ResultSuccess == nil {
		return nil, GetPlanResultOutput{
			Found:   true,
			PlanID:  plan.ID,
			Title:   plan.Title,
			Status:  string(plan.Status),
			Steps:   plan.Steps,
			Message: "Executor has not stored a result yet. Status: " + string(plan.Status),
		}, nil
	}

	success := *plan.ResultSuccess
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
// Called by: planner agent (premium model) after reviewing result
// ============================================================

type VerifyPlanInput struct {
	PlanID   string `json:"plan_id"`
	Approved bool   `json:"approved"`
	Note     string `json:"note,omitempty"`
}

type VerifyPlanOutput struct {
	Message string `json:"message"`
}

func (h *Handlers) HandleVerifyPlan(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input VerifyPlanInput,
) (*mcp.CallToolResult, VerifyPlanOutput, error) {
	if h.planStore == nil {
		return nil, VerifyPlanOutput{Message: "Plan store not available."}, nil
	}
	if input.PlanID == "" {
		return nil, VerifyPlanOutput{Message: "plan_id is required."}, nil
	}

	if err := h.planStore.VerifyPlan(input.PlanID, input.Approved, input.Note); err != nil {
		return nil, VerifyPlanOutput{}, err
	}

	// Report skill outcome to Engine 3
	plan, _ := h.planStore.GetPlanByID(input.PlanID)
	if plan != nil && plan.SkillUsed != "" && h.skillExec != nil {
		errDetail := ""
		if !input.Approved {
			errDetail = "Plan rejected during verification: " + input.Note
		}
		go func() { //nolint:errcheck
			h.skillExec.RecordExecution(plan.SkillUsed, skills.SkillExecution{
				SkillID:     plan.SkillUsed,
				Success:     input.Approved,
				ErrorDetail: errDetail,
			})
		}()
	}

	if input.Approved {
		return nil, VerifyPlanOutput{Message: "Plan verified and approved."}, nil
	}
	return nil, VerifyPlanOutput{
		Message: "Plan rejected. Reason: " + input.Note + ". Create a new plan to retry.",
	}, nil
}

// ============================================================
// HELPERS
// ============================================================

func (h *Handlers) logPlannerCost(planID string, tokens int) {
	if h.tracker == nil || h.orchConfig == nil {
		return
	}
	inputCost := float64(tokens) * 0.7 * h.orchConfig.PremiumModel.InputCostPerM / 1_000_000
	outputCost := float64(tokens) * 0.3 * h.orchConfig.PremiumModel.OutputCostPerM / 1_000_000
	total := inputCost + outputCost

	h.tracker.LogPlanCost(orchestrator.PlanCost{ //nolint:errcheck
		PlanID:                 planID,
		DeveloperID:            getDeveloperID(context.Background()),
		PlannerModel:           h.orchConfig.PremiumModel.Name,
		EstimatedPlannerTokens: tokens,
		EstimatedPlannerCost:   total,
	})
}

func (h *Handlers) logExecutorCost(planID string, tokens int) {
	if h.tracker == nil || h.orchConfig == nil {
		return
	}
	inputCost := float64(tokens) * 0.5 * h.orchConfig.ExecutionModel.InputCostPerM / 1_000_000
	outputCost := float64(tokens) * 0.5 * h.orchConfig.ExecutionModel.OutputCostPerM / 1_000_000
	executorTotal := inputCost + outputCost

	premiumInput := float64(tokens) * 0.5 * h.orchConfig.PremiumModel.InputCostPerM / 1_000_000
	premiumOutput := float64(tokens) * 0.5 * h.orchConfig.PremiumModel.OutputCostPerM / 1_000_000
	allPremium := premiumInput + premiumOutput

	h.tracker.LogPlanCost(orchestrator.PlanCost{ //nolint:errcheck
		PlanID:                  planID,
		DeveloperID:             getDeveloperID(context.Background()),
		ExecutorModel:           h.orchConfig.ExecutionModel.Name,
		EstimatedExecutorTokens: tokens,
		EstimatedExecutorCost:   executorTotal,
		EstimatedAllPremiumCost: allPremium,
		EstimatedSavings:        allPremium - executorTotal,
	})
}
