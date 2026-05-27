package orchestrator

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tracker logs plan costs and provides dashboard data.
// Cost estimates are based on the developer's configured model pricing.
type Tracker struct {
	pool *pgxpool.Pool
}

// NewTracker creates a new Tracker.
func NewTracker(databaseURL string) (*Tracker, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Tracker{pool: pool}, nil
}

// Close closes the connection pool.
func (t *Tracker) Close() {
	t.pool.Close()
}

// LogPlanCost records the estimated cost of a plan execution.
func (t *Tracker) LogPlanCost(cost PlanCost) error {
	var planID *string
	if cost.PlanID != "" {
		planID = &cost.PlanID
	}
	_, err := t.pool.Exec(context.Background(), `
		INSERT INTO plan_costs (
			plan_id, developer_id, planner_model, executor_model,
			estimated_planner_tokens, estimated_executor_tokens,
			estimated_planner_cost, estimated_executor_cost,
			estimated_total_cost, estimated_all_premium_cost,
			estimated_savings, skill_used, memory_hit, routing_recommendation
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		planID, cost.DeveloperID, cost.PlannerModel, cost.ExecutorModel,
		cost.EstimatedPlannerTokens, cost.EstimatedExecutorTokens,
		cost.EstimatedPlannerCost, cost.EstimatedExecutorCost,
		cost.EstimatedTotalCost, cost.EstimatedAllPremiumCost,
		cost.EstimatedSavings, cost.SkillUsed, cost.MemoryHit,
		cost.RoutingRecommendation,
	)
	return err
}

// MonthlySummary is the headline cost summary.
type MonthlySummary struct {
	Month          string  `json:"month"`
	TotalPlans     int     `json:"total_plans"`
	ActualCost     float64 `json:"actual_cost"`
	WouldHaveCost  float64 `json:"would_have_cost"`
	Savings        float64 `json:"savings"`
	SavingsPercent float64 `json:"savings_percent"`
	SkillUses      int     `json:"skill_uses"`
	MemoryHits     int     `json:"memory_hits"`
	AvgCostPerPlan float64 `json:"avg_cost_per_plan"`
}

// GetMonthlySummary returns per-month cost summaries for the past 6 months.
func (t *Tracker) GetMonthlySummary() ([]MonthlySummary, error) {
	rows, err := t.pool.Query(context.Background(), `
		SELECT
			to_char(date_trunc('month', created_at), 'YYYY-MM') AS month,
			COUNT(*) AS total_plans,
			COALESCE(SUM(estimated_total_cost), 0) AS actual_cost,
			COALESCE(SUM(estimated_all_premium_cost), 0) AS would_have_cost,
			COALESCE(SUM(estimated_savings), 0) AS savings,
			COUNT(*) FILTER (WHERE skill_used) AS skill_uses,
			COUNT(*) FILTER (WHERE memory_hit) AS memory_hits,
			COALESCE(AVG(estimated_total_cost), 0) AS avg_cost_per_plan
		FROM plan_costs
		WHERE created_at >= NOW() - INTERVAL '6 months'
		GROUP BY date_trunc('month', created_at)
		ORDER BY date_trunc('month', created_at)`)
	if err != nil {
		return nil, fmt.Errorf("get monthly summary: %w", err)
	}
	defer rows.Close()

	var summaries []MonthlySummary
	for rows.Next() {
		var s MonthlySummary
		if err := rows.Scan(&s.Month, &s.TotalPlans, &s.ActualCost,
			&s.WouldHaveCost, &s.Savings, &s.SkillUses, &s.MemoryHits,
			&s.AvgCostPerPlan); err != nil {
			return nil, err
		}
		if s.WouldHaveCost > 0 {
			s.SavingsPercent = s.Savings / s.WouldHaveCost * 100
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// DeveloperSummary is a per-developer weekly breakdown.
type DeveloperSummary struct {
	Week       string  `json:"week"`
	TotalPlans int     `json:"total_plans"`
	ActualCost float64 `json:"actual_cost"`
	Savings    float64 `json:"savings"`
}

// GetDeveloperSummary returns weekly cost summaries for a developer.
func (t *Tracker) GetDeveloperSummary(developerID string) ([]DeveloperSummary, error) {
	rows, err := t.pool.Query(context.Background(), `
		SELECT
			to_char(date_trunc('week', created_at), 'YYYY-MM-DD') AS week,
			COUNT(*) AS total_plans,
			COALESCE(SUM(estimated_total_cost), 0) AS actual_cost,
			COALESCE(SUM(estimated_savings), 0) AS savings
		FROM plan_costs
		WHERE developer_id = $1
		  AND created_at >= NOW() - INTERVAL '12 weeks'
		GROUP BY date_trunc('week', created_at)
		ORDER BY date_trunc('week', created_at)`, developerID)
	if err != nil {
		return nil, fmt.Errorf("get developer summary: %w", err)
	}
	defer rows.Close()

	var summaries []DeveloperSummary
	for rows.Next() {
		var s DeveloperSummary
		if err := rows.Scan(&s.Week, &s.TotalPlans, &s.ActualCost, &s.Savings); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// RefreshViews refreshes the materialized views.
func (t *Tracker) RefreshViews() error {
	ctx := context.Background()
	if _, err := t.pool.Exec(ctx, "REFRESH MATERIALIZED VIEW monthly_cost_summary"); err != nil {
		return fmt.Errorf("refresh monthly_cost_summary: %w", err)
	}
	_, err := t.pool.Exec(ctx, "REFRESH MATERIALIZED VIEW developer_cost_summary")
	return err
}
