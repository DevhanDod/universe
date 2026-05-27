package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/skills"
)

// ============================================================
// Tool: find_skill
// ============================================================

type FindSkillInput struct {
	TaskText     string   `json:"task_text"`
	GraphNodeIDs []string `json:"graph_node_ids,omitempty"`
	Language     string   `json:"language,omitempty"`
}

type FindSkillOutput struct {
	Found              bool    `json:"found"`
	SkillID            string  `json:"skill_id,omitempty"`
	SkillName          string  `json:"skill_name,omitempty"`
	Version            int     `json:"version,omitempty"`
	Instruction        string  `json:"instruction,omitempty"`
	SuccessRate        float64 `json:"success_rate,omitempty"`
	Confidence         float64 `json:"confidence,omitempty"`
	ExplorationSkipped bool    `json:"exploration_skipped"`
	Message            string  `json:"message"`

	// Verification fields — always set when Found=true.
	RequiresVerification bool   `json:"requires_verification"`
	VerificationPrompt   string `json:"verification_prompt,omitempty"`
	StaleWarning         bool   `json:"stale_warning"`
	LastUpdated          string `json:"last_updated,omitempty"`
	TimesApplied         int    `json:"times_applied,omitempty"`
}

func (h *Handlers) HandleFindSkill(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input FindSkillInput,
) (*mcp.CallToolResult, FindSkillOutput, error) {
	if h.skillMatcher == nil {
		return nil, FindSkillOutput{
			Found:   false,
			Message: "Skills engine not available. Set DATABASE_URL and restart.",
		}, nil
	}

	result, err := h.skillMatcher.Match(skills.MatchQuery{
		TaskText:     input.TaskText,
		GraphNodeIDs: input.GraphNodeIDs,
		Language:     input.Language,
		Limit:        3,
	})
	if err != nil {
		return nil, FindSkillOutput{}, err
	}

	if result.ExplorationTriggered {
		return nil, FindSkillOutput{
			Found:              false,
			ExplorationSkipped: true,
			Message:            "Exploration mode (10% chance). Reason from scratch — if your approach works well it may be captured as a new skill.",
		}, nil
	}

	if result.BestMatch == nil {
		return nil, FindSkillOutput{
			Found:   false,
			Message: "No matching skill found. Reason from scratch. If the task succeeds, the approach may be captured as a new skill.",
		}, nil
	}

	skill, err := h.skillStore.GetByID(result.BestMatch.ID)
	if err != nil {
		return nil, FindSkillOutput{}, err
	}

	isStale := false
	for _, tag := range skill.NegativeTags {
		if tag.Context == "graph_changed" {
			isStale = true
			break
		}
	}

	return nil, FindSkillOutput{
		Found:                true,
		SkillID:              skill.ID,
		SkillName:            skill.Name,
		Version:              skill.Version,
		Instruction:          skill.Instruction,
		SuccessRate:          result.BestMatch.SuccessRate,
		Confidence:           result.BestMatch.Confidence,
		TimesApplied:         skill.TimesApplied,
		LastUpdated:          skill.CreatedAt.Format("2006-01-02"),
		RequiresVerification: true,
		VerificationPrompt:   result.BestMatch.VerificationPrompt,
		StaleWarning:         isStale,
		Message: "Skill found. IMPORTANT: You are the planning agent (premium model). " +
			"Review the skill instruction against the current code before including it in your plan. " +
			"If the skill is correct, include its steps in the plan for the execution agent. " +
			"If it is outdated, plan from scratch using the graph context instead.",
	}, nil
}

// ============================================================
// Tool: report_skill_execution
// ============================================================

type ReportSkillExecutionInput struct {
	SkillID     string `json:"skill_id"`
	Success     bool   `json:"success"`
	ErrorDetail string `json:"error_detail,omitempty"`
	TokensUsed  int    `json:"tokens_used,omitempty"`
}

type ReportSkillExecutionOutput struct {
	Message string `json:"message"`
}

func (h *Handlers) HandleReportSkillExecution(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ReportSkillExecutionInput,
) (*mcp.CallToolResult, ReportSkillExecutionOutput, error) {
	if h.skillExec == nil {
		return nil, ReportSkillExecutionOutput{Message: "Skills engine not available."}, nil
	}

	exec := skills.SkillExecution{
		SkillID:     input.SkillID,
		Success:     input.Success,
		ErrorDetail: input.ErrorDetail,
		TokensUsed:  input.TokensUsed,
		DeveloperID: "cursor-agent",
	}

	if err := h.skillExec.RecordExecution(input.SkillID, exec); err != nil {
		return nil, ReportSkillExecutionOutput{}, err
	}

	msg := "Skill execution recorded. "
	if input.Success {
		msg += "Success logged — skill confidence increased."
	} else {
		msg += "Failure logged — will be reviewed for potential improvement."
	}
	return nil, ReportSkillExecutionOutput{Message: msg}, nil
}

// ============================================================
// Tool: list_skills
// ============================================================

type ListSkillsInput struct {
	GraphNodeID string `json:"graph_node_id,omitempty"`
	Language    string `json:"language,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ListSkillsOutput struct {
	Skills  []SkillSummaryOut `json:"skills"`
	Total   int               `json:"total"`
	Message string            `json:"message,omitempty"`
}

type SkillSummaryOut struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     int     `json:"version"`
	Evolution   string  `json:"evolution"`
	Language    string  `json:"language"`
	TriggerDesc string  `json:"trigger_desc"`
	SuccessRate float64 `json:"success_rate"`
	Confidence  float64 `json:"confidence"`
	Applied     int     `json:"times_applied"`
	IsFrozen    bool    `json:"is_frozen"`
}

func (h *Handlers) HandleListSkills(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListSkillsInput,
) (*mcp.CallToolResult, ListSkillsOutput, error) {
	if h.skillStore == nil {
		return nil, ListSkillsOutput{Message: "Skills engine not available. Set DATABASE_URL and restart."}, nil
	}

	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	var skList []skills.Skill
	var err error

	if input.GraphNodeID != "" {
		skList, err = h.skillStore.GetByGraphNodes([]string{input.GraphNodeID}, input.Language, 0, limit)
	} else {
		skList, err = h.skillStore.GetByGraphNodes([]string{}, input.Language, 0, limit)
	}
	if err != nil {
		return nil, ListSkillsOutput{}, err
	}

	out := make([]SkillSummaryOut, 0, len(skList))
	for _, sk := range skList {
		sr := 0.0
		if sk.TimesApplied > 0 {
			sr = float64(sk.TimesSucceeded) / float64(sk.TimesApplied)
		}
		out = append(out, SkillSummaryOut{
			ID:          sk.ID,
			Name:        sk.Name,
			Version:     sk.Version,
			Evolution:   string(sk.Evolution),
			Language:    sk.Language,
			TriggerDesc: sk.TriggerDesc,
			SuccessRate: sr,
			Confidence:  sk.Confidence,
			Applied:     sk.TimesApplied,
			IsFrozen:    sk.IsFrozen,
		})
	}

	return nil, ListSkillsOutput{Skills: out, Total: len(out)}, nil
}

// ============================================================
// Tool: get_skill_lineage
// ============================================================

type GetSkillLineageInput struct {
	SkillID string `json:"skill_id"`
}

type GetSkillLineageOutput struct {
	Lineage []SkillVersionOut `json:"lineage"`
	Derived []SkillVersionOut `json:"derived"`
	Message string            `json:"message,omitempty"`
}

type SkillVersionOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Evolution string `json:"evolution"`
	ParentID  string `json:"parent_id,omitempty"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func (h *Handlers) HandleGetSkillLineage(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetSkillLineageInput,
) (*mcp.CallToolResult, GetSkillLineageOutput, error) {
	if h.skillStore == nil {
		return nil, GetSkillLineageOutput{Message: "Skills engine not available. Set DATABASE_URL and restart."}, nil
	}

	lineage, err := h.skillStore.GetLineage(input.SkillID)
	if err != nil {
		return nil, GetSkillLineageOutput{}, err
	}

	derived, err := h.skillStore.GetChildren(input.SkillID)
	if err != nil {
		return nil, GetSkillLineageOutput{}, err
	}

	toOut := func(sk skills.Skill) SkillVersionOut {
		pid := ""
		if sk.ParentID != nil {
			pid = *sk.ParentID
		}
		return SkillVersionOut{
			ID:        sk.ID,
			Name:      sk.Name,
			Version:   sk.Version,
			Evolution: string(sk.Evolution),
			ParentID:  pid,
			CreatedBy: sk.CreatedBy,
			CreatedAt: sk.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	lineageOut := make([]SkillVersionOut, len(lineage))
	for i, sk := range lineage {
		lineageOut[i] = toOut(sk)
	}
	derivedOut := make([]SkillVersionOut, len(derived))
	for i, sk := range derived {
		derivedOut[i] = toOut(sk)
	}

	return nil, GetSkillLineageOutput{Lineage: lineageOut, Derived: derivedOut}, nil
}
