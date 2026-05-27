package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PlanStore handles all database operations for the plans table.
// This is the core of the two-agent bridge.
type PlanStore struct {
	pool *pgxpool.Pool
}

// NewPlanStore creates a new PlanStore.
func NewPlanStore(databaseURL string) (*PlanStore, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &PlanStore{pool: pool}, nil
}

// Close closes the connection pool.
func (ps *PlanStore) Close() error {
	ps.pool.Close()
	return nil
}

const selectPlanColumns = `
	id, developer_id, title, task_prompt, steps,
	COALESCE(files_to_change, ARRAY[]::TEXT[]),
	COALESCE(skill_used::text, ''), skill_verified,
	COALESCE(graph_context, ''),
	COALESCE(affected_nodes, ARRAY[]::TEXT[]),
	cross_repo, COALESCE(risk_level, ''),
	COALESCE(planner_model, ''), COALESCE(executor_model, ''),
	status, result_success,
	COALESCE(result_summary, ''),
	COALESCE(result_files, ARRAY[]::TEXT[]),
	result_tests, COALESCE(result_error, ''),
	verified, COALESCE(verification_note, ''),
	created_at, executed_at, verified_at`

type scannable interface {
	Scan(dest ...any) error
}

func scanPlan(row scannable) (*Plan, error) {
	var p Plan
	var stepsJSON []byte
	var status string
	err := row.Scan(
		&p.ID, &p.DeveloperID, &p.Title, &p.TaskPrompt, &stepsJSON,
		&p.FilesToChange, &p.SkillUsed, &p.SkillVerified,
		&p.GraphContext, &p.AffectedNodes, &p.CrossRepo, &p.RiskLevel,
		&p.PlannerModel, &p.ExecutorModel,
		&status, &p.ResultSuccess,
		&p.ResultSummary, &p.ResultFiles, &p.ResultTests, &p.ResultError,
		&p.Verified, &p.VerificationNote,
		&p.CreatedAt, &p.ExecutedAt, &p.VerifiedAt,
	)
	if err != nil {
		return nil, err
	}
	p.Status = PlanStatus(status)
	_ = json.Unmarshal(stepsJSON, &p.Steps)
	return &p, nil
}

// StorePlan saves a new plan created by the planner agent.
func (ps *PlanStore) StorePlan(plan Plan) (*Plan, error) {
	stepsJSON, err := json.Marshal(plan.Steps)
	if err != nil {
		return nil, fmt.Errorf("marshal steps: %w", err)
	}

	var skillUsed *string
	if plan.SkillUsed != "" {
		skillUsed = &plan.SkillUsed
	}

	var id string
	var createdAt time.Time
	err = ps.pool.QueryRow(context.Background(), `
		INSERT INTO plans (
			developer_id, title, task_prompt, steps, files_to_change,
			skill_used, skill_verified, graph_context, affected_nodes,
			cross_repo, risk_level, planner_model, executor_model
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, created_at`,
		plan.DeveloperID, plan.Title, plan.TaskPrompt, stepsJSON,
		plan.FilesToChange, skillUsed, plan.SkillVerified,
		plan.GraphContext, plan.AffectedNodes, plan.CrossRepo,
		plan.RiskLevel, plan.PlannerModel, plan.ExecutorModel,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("store plan: %w", err)
	}

	plan.ID = id
	plan.CreatedAt = createdAt
	plan.Status = PlanPending
	return &plan, nil
}

// GetLatestPlan retrieves the most recent pending plan for a developer
// and atomically marks it as executing.
func (ps *PlanStore) GetLatestPlan(developerID string) (*Plan, error) {
	row := ps.pool.QueryRow(context.Background(), `
		WITH selected AS (
			SELECT id FROM plans
			WHERE developer_id = $1 AND status = 'pending'
			ORDER BY created_at DESC LIMIT 1
		),
		updated AS (
			UPDATE plans SET status = 'executing'
			WHERE id = (SELECT id FROM selected)
			RETURNING `+selectPlanColumns+`
		)
		SELECT * FROM updated`, developerID)

	p, err := scanPlan(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// GetPlanByID retrieves a specific plan by UUID.
func (ps *PlanStore) GetPlanByID(id string) (*Plan, error) {
	row := ps.pool.QueryRow(context.Background(),
		`SELECT `+selectPlanColumns+` FROM plans WHERE id = $1`, id)
	p, err := scanPlan(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// StorePlanResult saves the executor's result for a plan.
func (ps *PlanStore) StorePlanResult(planID string, success bool, summary string, files []string, testsPassed bool, errorDetail string) error {
	status := PlanCompleted
	if !success {
		status = PlanFailed
	}
	_, err := ps.pool.Exec(context.Background(), `
		UPDATE plans SET
			status = $2,
			result_success = $3,
			result_summary = $4,
			result_files = $5,
			result_tests = $6,
			result_error = $7,
			executed_at = NOW()
		WHERE id = $1`,
		planID, string(status), success, summary, files, testsPassed, errorDetail,
	)
	return err
}

// GetPlanResult retrieves the result of a plan for the planner to verify.
func (ps *PlanStore) GetPlanResult(planID string) (*Plan, error) {
	return ps.GetPlanByID(planID)
}

// GetLatestCompletedPlan retrieves the most recent plan with a result
// (status = completed or failed) that has not yet been verified.
func (ps *PlanStore) GetLatestCompletedPlan(developerID string) (*Plan, error) {
	row := ps.pool.QueryRow(context.Background(), `
		SELECT `+selectPlanColumns+` FROM plans
		WHERE developer_id = $1
		  AND status IN ('completed', 'failed')
		  AND verified IS NULL
		ORDER BY executed_at DESC
		LIMIT 1`, developerID)
	p, err := scanPlan(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// VerifyPlan marks a plan as verified or rejected by the planner.
func (ps *PlanStore) VerifyPlan(planID string, approved bool, note string) error {
	status := PlanVerified
	if !approved {
		status = PlanRejected
	}
	_, err := ps.pool.Exec(context.Background(), `
		UPDATE plans SET
			status = $2,
			verified = $3,
			verification_note = $4,
			verified_at = NOW()
		WHERE id = $1`,
		planID, string(status), approved, note,
	)
	return err
}

// ListPlans returns paginated plan summaries for a developer.
func (ps *PlanStore) ListPlans(developerID string, limit int, offset int) ([]PlanSummary, int, error) {
	rows, err := ps.pool.Query(context.Background(), `
		SELECT id, title, status, jsonb_array_length(steps) AS step_count,
			skill_used IS NOT NULL AS skill_used, cross_repo,
			COALESCE(planner_model, ''), COALESCE(executor_model, ''),
			created_at
		FROM plans
		WHERE developer_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		developerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var summaries []PlanSummary
	for rows.Next() {
		var ps PlanSummary
		var status string
		if err := rows.Scan(&ps.ID, &ps.Title, &status, &ps.StepCount,
			&ps.SkillUsed, &ps.CrossRepo, &ps.PlannerModel, &ps.ExecutorModel,
			&ps.CreatedAt); err != nil {
			return nil, 0, err
		}
		ps.Status = PlanStatus(status)
		summaries = append(summaries, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	ps.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM plans WHERE developer_id = $1`, developerID).
		Scan(&total)

	return summaries, total, nil
}

// PlanStats aggregates plan statistics.
type PlanStats struct {
	TotalPlans      int     `json:"total_plans"`
	Completed       int     `json:"completed"`
	Failed          int     `json:"failed"`
	Verified        int     `json:"verified"`
	Rejected        int     `json:"rejected"`
	Pending         int     `json:"pending"`
	SkillUsedCount  int     `json:"skill_used_count"`
	CrossRepoCount  int     `json:"cross_repo_count"`
	AvgStepsPerPlan float64 `json:"avg_steps_per_plan"`
}

// GetPlanStats returns aggregate statistics for a developer.
func (ps *PlanStore) GetPlanStats(developerID string) (*PlanStats, error) {
	var s PlanStats
	err := ps.pool.QueryRow(context.Background(), `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'completed'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'verified'),
			COUNT(*) FILTER (WHERE status = 'rejected'),
			COUNT(*) FILTER (WHERE status IN ('pending','executing')),
			COUNT(*) FILTER (WHERE skill_used IS NOT NULL),
			COUNT(*) FILTER (WHERE cross_repo),
			COALESCE(AVG(jsonb_array_length(steps)), 0)
		FROM plans WHERE developer_id = $1`, developerID).
		Scan(&s.TotalPlans, &s.Completed, &s.Failed, &s.Verified, &s.Rejected,
			&s.Pending, &s.SkillUsedCount, &s.CrossRepoCount, &s.AvgStepsPerPlan)
	if err != nil {
		return nil, fmt.Errorf("get plan stats: %w", err)
	}
	return &s, nil
}
