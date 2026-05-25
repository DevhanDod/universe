package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================
// Tool: get_cost_summary
// ============================================================

type GetCostSummaryInput struct {
	Period      string `json:"period,omitempty" jsonschema:"description=Time period: week month all (default month)"`
	DeveloperID string `json:"developer_id,omitempty" jsonschema:"description=Filter by developer. Empty for team-wide."`
}

type GetCostSummaryOutput struct {
	ActualCost       float64        `json:"actual_cost"`
	WouldHaveCost    float64        `json:"would_have_cost"`
	SavingsUSD       float64        `json:"savings_usd"`
	SavingsPercent   float64        `json:"savings_percent"`
	TotalTasks       int            `json:"total_tasks"`
	SkillUses        int            `json:"skill_uses"`
	MemoryHits       int            `json:"memory_hits"`
	Takeovers        int            `json:"takeovers"`
	RoutingBreakdown map[string]int `json:"routing_breakdown"`
	Message          string         `json:"message,omitempty"`
}

func (h *Handlers) HandleGetCostSummary(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetCostSummaryInput,
) (*mcp.CallToolResult, GetCostSummaryOutput, error) {
	if h.orchestrator == nil {
		return nil, GetCostSummaryOutput{
			Message: "Orchestrator not available. Cost tracking requires DATABASE_URL.",
		}, nil
	}

	summaries, err := h.orchestrator.Tracker().GetMonthlySummary()
	if err != nil {
		return nil, GetCostSummaryOutput{}, err
	}

	if len(summaries) == 0 {
		return nil, GetCostSummaryOutput{
			Message: "No cost data yet. Run tasks through the orchestrator first.",
		}, nil
	}

	// Use the most recent month
	s := summaries[len(summaries)-1]
	pct := 0.0
	if s.WouldHaveCost > 0 {
		pct = (s.SavingsUSD / s.WouldHaveCost) * 100
	}

	return nil, GetCostSummaryOutput{
		ActualCost:     s.ActualCost,
		WouldHaveCost:  s.WouldHaveCost,
		SavingsUSD:     s.SavingsUSD,
		SavingsPercent: pct,
		TotalTasks:     s.TotalCalls,
		SkillUses:      s.SkillExecutions,
		MemoryHits:     s.MemoryApplies,
		Takeovers:      s.Takeovers,
		RoutingBreakdown: map[string]int{
			"skill_execute":      s.SkillExecutions,
			"memory_apply":       s.MemoryApplies,
			"plan_execute":       s.PlanExecutes,
			"full_orchestration": s.FullOrchestrations,
			"single_opus":        s.TotalCalls - s.SkillExecutions - s.MemoryApplies - s.PlanExecutes - s.FullOrchestrations,
		},
	}, nil
}
