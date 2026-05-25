package mcp

import (
	"encoding/json"
	"time"

	"github.com/Universe/universe/internal/orchestrator"
	"github.com/google/uuid"
)

// routeTaskInput is the argument schema for universe_route_task.
type routeTaskInput struct {
	Task         string   `json:"task"`
	GraphNodeIDs []string `json:"graph_node_ids"`
	DeveloperID  string   `json:"developer_id"`
	Language     string   `json:"language"`
	RepoID       string   `json:"repo_id"`
}

// routeTaskOutput is the full response sent back to the agent.
type routeTaskOutput struct {
	// Routing decision fields
	Mode           string `json:"mode"`
	PlannerRole    string `json:"planner_role"`
	ExecutorRole   string `json:"executor_role"`
	VerifierRole   string `json:"verifier_role"`
	NextStep       string `json:"next_step"`
	SkipPlanning   bool   `json:"skip_planning"`
	TemplateID     string `json:"template_id,omitempty"`
	MatchedSkillID string `json:"matched_skill_id,omitempty"`
	MemoryHit      bool   `json:"memory_hit"`
	Reason         string `json:"reason"`
	// Task classification
	TaskType string `json:"task_type"`
	// Model hints from user config — agent maps these to its models
	PremiumModel string `json:"premium_model"`
	LowCostModel string `json:"low_cost_model"`
}

// RegisterRouteTask adds the universe_route_task tool to the registry.
func RegisterRouteTask(reg *Registry, router *orchestrator.Router, cfg *orchestrator.UserConfig) {
	reg.Register(ToolDef{
		Name: "universe_route_task",
		Description: "Analyse a developer task and return a routing decision. " +
			"Returns which model role to use for planning, execution, and verification, " +
			"plus the task type and model name hints. Zero LLM tokens — pure logic.",
		InputSchema: jsonSchema(map[string]interface{}{
			"task":           strProp("The developer's request text"),
			"graph_node_ids": arrStrProp("Graph node IDs of affected symbols (optional — pass [] if unknown)"),
			"developer_id":   strProp("Developer identifier (optional)"),
			"language":       strProp("Primary language of the task context (optional, e.g. 'go')"),
			"repo_id":        strProp("Repository identifier (optional)"),
		}, []string{"task"}),
	}, func(args json.RawMessage) (interface{}, error) {
		var in routeTaskInput
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}

		task := orchestrator.Task{
			ID:           uuid.NewString(),
			DeveloperID:  in.DeveloperID,
			RepoID:       in.RepoID,
			Prompt:       in.Task,
			GraphNodeIDs: in.GraphNodeIDs,
			TaskType:     orchestrator.ClassifyTaskType(in.Task),
			CreatedAt:    time.Now(),
		}

		decision, err := router.Route(task)
		if err != nil {
			return nil, err
		}

		out := routeTaskOutput{
			Mode:           string(decision.Mode),
			PlannerRole:    decision.PlannerRole,
			ExecutorRole:   decision.ExecutorRole,
			VerifierRole:   decision.VerifierRole,
			NextStep:       decision.NextStep,
			SkipPlanning:   decision.SkipPlanning,
			TemplateID:     decision.TemplateID,
			MatchedSkillID: decision.MatchedSkillID,
			MemoryHit:      decision.MemoryHit,
			Reason:         decision.Reason,
			TaskType:       string(task.TaskType),
			PremiumModel:   cfg.PremiumModel,
			LowCostModel:   cfg.LowCostModel,
		}

		data, _ := json.MarshalIndent(out, "", "  ")
		return TextContent(string(data)), nil
	})
}
