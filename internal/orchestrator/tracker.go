package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tracker logs every LLM call to the agent_costs table.
type Tracker struct {
	pool   *pgxpool.Pool
	logCh  chan CostRecord
	stopCh chan struct{}
}

// MonthlySummary is the headline cost summary for managers.
type MonthlySummary struct {
	Month              string  `json:"month"`
	TotalCalls         int     `json:"total_calls"`
	ActualCost         float64 `json:"actual_cost"`
	WouldHaveCost      float64 `json:"would_have_cost_all_opus"`
	SavingsUSD         float64 `json:"savings_usd"`
	SavingsPercent     float64 `json:"savings_percent"`
	SkillExecutions    int     `json:"skill_executions"`
	MemoryApplies      int     `json:"memory_applies"`
	PlanExecutes       int     `json:"plan_executes"`
	FullOrchestrations int     `json:"full_orchestrations"`
	Takeovers          int     `json:"takeovers"`
	OpusCost           float64 `json:"opus_cost"`
	HaikuCost          float64 `json:"haiku_cost"`
	AvgLatencyMs       float64 `json:"avg_latency_ms"`
}

// DeveloperWeekSummary is a per-developer weekly breakdown.
type DeveloperWeekSummary struct {
	Week       string  `json:"week"`
	TotalCalls int     `json:"total_calls"`
	ActualCost float64 `json:"actual_cost"`
	SavingsUSD float64 `json:"savings_usd"`
	Takeovers  int     `json:"takeovers"`
}

// RoutingStats shows which routing modes are most cost-effective.
type RoutingStats struct {
	Mode           RoutingMode `json:"mode"`
	TotalUses      int         `json:"total_uses"`
	AvgCost        float64     `json:"avg_cost"`
	AvgLatency     float64     `json:"avg_latency_ms"`
	AvgEscalations float64     `json:"avg_escalations"`
	Takeovers      int         `json:"takeovers"`
	TotalCost      float64     `json:"total_cost"`
}

func NewTracker(databaseURL string) (*Tracker, error) {
	if databaseURL == "" {
		return &Tracker{logCh: make(chan CostRecord, 100), stopCh: make(chan struct{})}, nil
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("tracker db connect: %w", err)
	}

	t := &Tracker{
		pool:   pool,
		logCh:  make(chan CostRecord, 500),
		stopCh: make(chan struct{}),
	}
	go t.writeLoop()
	return t, nil
}

// LogCall records a single LLM call asynchronously.
func (t *Tracker) LogCall(record CostRecord) {
	select {
	case t.logCh <- record:
	default:
		log.Printf("tracker: log channel full, dropping record for task %s", record.TaskID)
	}
}

// LogTask records the complete cost of a task (derived from all sub-results).
func (t *Tracker) LogTask(taskID string, decision *RoutingDecision, results []SubTaskResult, escalations []EscalationRecord) {
	esc := len(escalations)
	takeover := false
	for _, e := range escalations {
		if e.FinalModel == Opus {
			takeover = true
		}
	}

	for _, r := range results {
		phase := "execute"
		if r.Model == Opus {
			phase = "takeover"
		}
		cost := calculateCost(r.Model, r.TokensUsed/2, r.TokensUsed/2)
		t.LogCall(CostRecord{
			TaskID:          taskID,
			DeveloperID:     "",
			Model:           r.Model,
			InputTokens:     r.TokensUsed / 2,
			OutputTokens:    r.TokensUsed / 2,
			CostUSD:         cost,
			Phase:           phase,
			RoutingMode:     decision.Mode,
			MemoryHit:       decision.MemoryHit,
			EscalationSteps: esc,
			WasTakeover:     takeover,
			LatencyMs:       r.LatencyMs,
		})
	}
}

func (t *Tracker) writeLoop() {
	for {
		select {
		case <-t.stopCh:
			return
		case rec := <-t.logCh:
			if t.pool != nil {
				t.insertRecord(rec)
			}
		}
	}
}

func (t *Tracker) insertRecord(r CostRecord) {
	skillID := interface{}(nil)
	if r.SkillID != "" {
		skillID = r.SkillID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := t.pool.Exec(ctx, `
		INSERT INTO agent_costs
		  (task_id, developer_id, model, input_tokens, output_tokens, cost_usd,
		   phase, routing_mode, skill_id, memory_hit, escalation_steps,
		   was_takeover, latency_ms, repo_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		r.TaskID, r.DeveloperID, string(r.Model),
		r.InputTokens, r.OutputTokens, r.CostUSD,
		r.Phase, string(r.RoutingMode),
		skillID, r.MemoryHit, r.EscalationSteps,
		r.WasTakeover, r.LatencyMs, r.RepoID,
	)
	if err != nil {
		log.Printf("tracker: insert cost record: %v", err)
	}
}

// GetMonthlySummary reads from the monthly_cost_summary materialized view.
func (t *Tracker) GetMonthlySummary() ([]MonthlySummary, error) {
	if t.pool == nil {
		return nil, nil
	}
	rows, err := t.pool.Query(context.Background(), `
		SELECT
			to_char(month, 'YYYY-MM'),
			total_calls, actual_cost, would_have_cost_all_opus,
			savings_usd,
			CASE WHEN would_have_cost_all_opus > 0
				THEN (savings_usd / would_have_cost_all_opus * 100) ELSE 0 END,
			skill_executions, memory_applies, plan_executes,
			full_orchestrations, takeovers,
			opus_cost, haiku_cost, avg_latency_ms
		FROM monthly_cost_summary
		ORDER BY month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MonthlySummary
	for rows.Next() {
		var m MonthlySummary
		if err := rows.Scan(
			&m.Month, &m.TotalCalls, &m.ActualCost, &m.WouldHaveCost,
			&m.SavingsUSD, &m.SavingsPercent,
			&m.SkillExecutions, &m.MemoryApplies, &m.PlanExecutes,
			&m.FullOrchestrations, &m.Takeovers,
			&m.OpusCost, &m.HaikuCost, &m.AvgLatencyMs,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetDeveloperSummary returns per-developer weekly cost breakdown.
func (t *Tracker) GetDeveloperSummary(developerID string) ([]DeveloperWeekSummary, error) {
	if t.pool == nil {
		return nil, nil
	}
	rows, err := t.pool.Query(context.Background(), `
		SELECT to_char(week,'YYYY-MM-DD'), total_calls, actual_cost, savings_usd, takeovers
		FROM developer_cost_summary
		WHERE developer_id = $1
		ORDER BY week DESC`, developerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeveloperWeekSummary
	for rows.Next() {
		var s DeveloperWeekSummary
		if err := rows.Scan(&s.Week, &s.TotalCalls, &s.ActualCost, &s.SavingsUSD, &s.Takeovers); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetRoutingEffectiveness returns routing mode cost stats.
func (t *Tracker) GetRoutingEffectiveness() ([]RoutingStats, error) {
	if t.pool == nil {
		return nil, nil
	}
	rows, err := t.pool.Query(context.Background(), `
		SELECT routing_mode, total_uses, avg_cost, avg_latency, avg_escalations, takeovers, total_cost
		FROM routing_effectiveness
		ORDER BY total_cost DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RoutingStats
	for rows.Next() {
		var s RoutingStats
		var mode string
		if err := rows.Scan(&mode, &s.TotalUses, &s.AvgCost, &s.AvgLatency,
			&s.AvgEscalations, &s.Takeovers, &s.TotalCost); err != nil {
			return nil, err
		}
		s.Mode = RoutingMode(mode)
		out = append(out, s)
	}
	return out, rows.Err()
}

// RefreshViews refreshes the materialized views for the dashboard.
func (t *Tracker) RefreshViews() error {
	if t.pool == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, view := range []string{"monthly_cost_summary", "developer_cost_summary", "routing_effectiveness"} {
		if _, err := t.pool.Exec(ctx, "REFRESH MATERIALIZED VIEW "+view); err != nil {
			return fmt.Errorf("refresh %s: %w", view, err)
		}
	}
	return nil
}

// Stop flushes pending writes and closes the DB pool.
func (t *Tracker) Stop() {
	close(t.stopCh)
	// drain remaining records
	for {
		select {
		case rec := <-t.logCh:
			if t.pool != nil {
				t.insertRecord(rec)
			}
		default:
			if t.pool != nil {
				t.pool.Close()
			}
			return
		}
	}
}

func calculateCost(model ModelTier, inputTokens, outputTokens int) float64 {
	switch model {
	case Opus:
		return (float64(inputTokens)*15.0 + float64(outputTokens)*75.0) / 1_000_000
	case Haiku:
		return (float64(inputTokens)*0.25 + float64(outputTokens)*1.25) / 1_000_000
	default:
		return 0
	}
}
