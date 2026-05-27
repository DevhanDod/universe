package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ============================================================
// Tool: get_cost_summary
// ============================================================

type GetCostSummaryInput struct {
	Period      string `json:"period,omitempty"`
	DeveloperID string `json:"developer_id,omitempty"`
}

type GetCostSummaryOutput struct {
	ActualCost     float64 `json:"actual_cost"`
	WouldHaveCost  float64 `json:"would_have_cost"`
	SavingsUSD     float64 `json:"savings_usd"`
	SavingsPercent float64 `json:"savings_percent"`
	TotalPlans     int     `json:"total_plans"`
	SkillUses      int     `json:"skill_uses"`
	MemoryHits     int     `json:"memory_hits"`
	AvgCostPerPlan float64 `json:"avg_cost_per_plan"`
	PremiumModel   string  `json:"premium_model,omitempty"`
	ExecutionModel string  `json:"execution_model,omitempty"`
	Message        string  `json:"message,omitempty"`
}

func (h *Handlers) HandleGetCostSummary(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetCostSummaryInput,
) (*mcp.CallToolResult, GetCostSummaryOutput, error) {
	if h.tracker == nil {
		return nil, GetCostSummaryOutput{
			Message: "Cost tracker not available. Connect a database: universe config set db postgres://...",
		}, nil
	}

	summaries, err := h.tracker.GetMonthlySummary()
	if err != nil {
		return nil, GetCostSummaryOutput{}, err
	}

	if len(summaries) == 0 {
		return nil, GetCostSummaryOutput{
			Message: "No cost data yet. Run tasks through the plan bridge first.",
		}, nil
	}

	s := summaries[len(summaries)-1]
	pct := 0.0
	if s.WouldHaveCost > 0 {
		pct = s.Savings / s.WouldHaveCost * 100
	}

	premiumModel := ""
	executionModel := ""
	if h.orchConfig != nil {
		premiumModel = h.orchConfig.PremiumModel.Name
		executionModel = h.orchConfig.ExecutionModel.Name
	}

	return nil, GetCostSummaryOutput{
		ActualCost:     s.ActualCost,
		WouldHaveCost:  s.WouldHaveCost,
		SavingsUSD:     s.Savings,
		SavingsPercent: pct,
		TotalPlans:     s.TotalPlans,
		SkillUses:      s.SkillUses,
		MemoryHits:     s.MemoryHits,
		AvgCostPerPlan: s.AvgCostPerPlan,
		PremiumModel:   premiumModel,
		ExecutionModel: executionModel,
		Message:        fmt.Sprintf("This month: %d plans, $%.4f actual vs $%.4f without routing (%.1f%% saved).", s.TotalPlans, s.ActualCost, s.WouldHaveCost, pct),
	}, nil
}
