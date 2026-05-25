package mcp

import (
	"encoding/json"

	"github.com/Universe/universe/internal/skills"
)

// getSkillInput is the argument schema for universe_get_skill.
type getSkillInput struct {
	GraphNodeIDs []string `json:"graph_node_ids"`
	TaskText     string   `json:"task_text"`
	Language     string   `json:"language"`
	DeveloperID  string   `json:"developer_id"`
}

// skillResult is the tool response when a skill is found.
type skillResult struct {
	Found       bool    `json:"found"`
	SkillID     string  `json:"skill_id,omitempty"`
	Name        string  `json:"name,omitempty"`
	Instruction string  `json:"instruction,omitempty"`
	TriggerDesc string  `json:"trigger_desc,omitempty"`
	SuccessRate float64 `json:"success_rate,omitempty"`
	GraphOverlap float64 `json:"graph_overlap,omitempty"`
	TimesApplied int    `json:"times_applied,omitempty"`
}

// RegisterGetSkill adds the universe_get_skill tool to the registry.
// store may be nil — returns {"found": false} when no DB is configured.
func RegisterGetSkill(reg *Registry, store *skills.Store, matcher *skills.Matcher) {
	reg.Register(ToolDef{
		Name: "universe_get_skill",
		Description: "Find the best matching skill recipe for a task. " +
			"If found, the agent should follow the instruction using its low-cost model. " +
			"Returns {\"found\": false} if no suitable skill exists.",
		InputSchema: jsonSchema(map[string]interface{}{
			"task_text":      strProp("Description of the task to match against skill library"),
			"graph_node_ids": arrStrProp("Graph node IDs of affected symbols (optional)"),
			"language":       strProp("Programming language (optional, e.g. 'go')"),
			"developer_id":   strProp("Developer identifier (optional)"),
		}, []string{"task_text"}),
	}, func(args json.RawMessage) (interface{}, error) {
		var in getSkillInput
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}

		if matcher == nil || store == nil {
			out, _ := json.Marshal(skillResult{Found: false})
			return TextContent(string(out)), nil
		}

		result, err := matcher.Match(skills.MatchQuery{
			TaskText:     in.TaskText,
			GraphNodeIDs: in.GraphNodeIDs,
			Language:     in.Language,
			DeveloperID:  in.DeveloperID,
			Limit:        1,
		})
		if err != nil || result.BestMatch == nil {
			out, _ := json.Marshal(skillResult{Found: false})
			return TextContent(string(out)), nil
		}

		best := result.BestMatch

		// fetch full skill to get the instruction text
		full, err := store.GetByID(best.ID)
		if err != nil || full == nil {
			out, _ := json.Marshal(skillResult{Found: false})
			return TextContent(string(out)), nil
		}

		out, _ := json.MarshalIndent(skillResult{
			Found:        true,
			SkillID:      full.ID,
			Name:         full.Name,
			Instruction:  full.Instruction,
			TriggerDesc:  full.TriggerDesc,
			SuccessRate:  best.SuccessRate,
			GraphOverlap: best.GraphOverlap,
			TimesApplied: best.TimesApplied,
		}, "", "  ")
		return TextContent(string(out)), nil
	})
}
