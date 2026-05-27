package mcp

import (
	"encoding/json"

	"github.com/Universe/universe/internal/orchestrator"
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
	// Routing recommendation fields
	SkillAvailable    bool    `json:"skill_available"`
	SkillID           string  `json:"skill_id,omitempty"`
	SkillName         string  `json:"skill_name,omitempty"`
	SkillConfidence   float64 `json:"skill_confidence,omitempty"`
	MemoryAvailable   bool    `json:"memory_available"`
	MemoryCount       int     `json:"memory_count,omitempty"`
	AffectedNodeCount int     `json:"affected_node_count"`
	CrossRepo         bool    `json:"cross_repo"`
	RiskLevel         string  `json:"risk_level"`
	Recommendation    string  `json:"recommendation"`
	// Task classification and model hints
	TaskType     string `json:"task_type"`
	PremiumModel string `json:"premium_model"`
	LowCostModel string `json:"low_cost_model"`
}

// RegisterRouteTask adds the universe_route_task tool to the registry.
func RegisterRouteTask(reg *Registry, router *orchestrator.Router, cfg *orchestrator.UserConfig) {
	reg.Register(ToolDef{
		Name: "universe_route_task",
		Description: "Analyse a developer task and return routing recommendations. " +
			"Returns whether a skill or past memory is available, the blast radius, " +
			"risk level, and a plain-English recommendation for the planner. " +
			"Zero LLM tokens — pure logic.",
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

		developerID := in.DeveloperID
		if developerID == "" {
			developerID = "cursor-agent"
		}

		rec, err := router.Recommend(in.GraphNodeIDs, in.Task, developerID)
		if err != nil {
			return nil, err
		}

		out := routeTaskOutput{
			SkillAvailable:    rec.SkillAvailable,
			SkillID:           rec.SkillID,
			SkillName:         rec.SkillName,
			SkillConfidence:   rec.SkillConfidence,
			MemoryAvailable:   rec.MemoryAvailable,
			MemoryCount:       rec.MemoryCount,
			AffectedNodeCount: rec.AffectedNodeCount,
			CrossRepo:         rec.CrossRepo,
			RiskLevel:         rec.RiskLevel,
			Recommendation:    rec.Recommendation,
			TaskType:          orchestrator.ClassifyTaskType(in.Task),
			PremiumModel:      cfg.PremiumModel,
			LowCostModel:      cfg.LowCostModel,
		}

		data, _ := json.MarshalIndent(out, "", "  ")
		return TextContent(string(data)), nil
	})
}
