package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Universe/universe/internal/graph"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Overview ──────────────────────────────────────────────────────────────────

func QueryMonthlySummary(db *pgxpool.Pool) (MonthlySummary, error) {
	var s MonthlySummary
	if db == nil {
		return s, nil
	}
	// Current month totals from plan_costs (materialized view may be stale)
	row := db.QueryRow(context.Background(), `
		SELECT
			COALESCE(COUNT(*), 0)                                                                  AS total_tasks,
			COALESCE(SUM(estimated_total_cost), 0)                                                AS actual_cost,
			COALESCE(SUM(estimated_all_premium_cost), 0)                                          AS would_have,
			COALESCE(COUNT(*) FILTER (WHERE skill_used), 0)                                       AS skill_uses,
			COALESCE(COUNT(*) FILTER (WHERE memory_hit), 0)                                       AS memory_hits,
			COALESCE(
				COUNT(*) FILTER (WHERE executor_model ILIKE '%haiku%')::float /
				GREATEST(COUNT(*), 1) * 100, 0)                                                   AS haiku_pct
		FROM plan_costs
		WHERE created_at >= date_trunc('month', NOW())
	`)
	var wouldHave float64
	if err := row.Scan(&s.TotalTasks, &s.ActualCost, &wouldHave,
		&s.SkillUses, &s.MemoryHits, &s.HaikuPct); err != nil {
		return s, fmt.Errorf("query monthly summary: %w", err)
	}
	s.WouldHaveCost = wouldHave
	s.SavingsUSD = wouldHave - s.ActualCost
	if wouldHave > 0 {
		s.SavingsPct = s.SavingsUSD / wouldHave * 100
	}
	s.Takeovers = 0 // concept removed in plan-based architecture
	return s, nil
}

func QueryMonthlyTrend(db *pgxpool.Pool) ([]MonthlyDataPoint, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(context.Background(), `
		SELECT
			to_char(date_trunc('month', created_at), 'Mon') AS month,
			COALESCE(SUM(estimated_total_cost), 0)          AS actual,
			COALESCE(SUM(estimated_all_premium_cost), 0)    AS would_have
		FROM plan_costs
		WHERE created_at >= NOW() - INTERVAL '6 months'
		GROUP BY date_trunc('month', created_at)
		ORDER BY date_trunc('month', created_at)
	`)
	if err != nil {
		return nil, fmt.Errorf("query monthly trend: %w", err)
	}
	defer rows.Close()

	var out []MonthlyDataPoint
	for rows.Next() {
		var p MonthlyDataPoint
		if err := rows.Scan(&p.Month, &p.Actual, &p.WouldHave); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func QueryEngineStats(db *pgxpool.Pool, g *graph.Graph) ([]EngineStatus, error) {
	engines := []EngineStatus{
		{Number: 1, Name: "Knowledge Graph", Status: "active"},
		{Number: 2, Name: "Persistent Memory", Status: "active"},
		{Number: 3, Name: "Evolving Skills", Status: "active"},
		{Number: 4, Name: "Compression", Status: "active"},
		{Number: 5, Name: "Orchestrator", Status: "active"},
	}

	// Engine 1 — graph stats (from in-memory graph)
	if g != nil {
		engines[0].Detail = fmt.Sprintf("%d nodes, %d edges", len(g.Nodes), len(g.Edges))
	} else {
		engines[0].Status = "disabled"
		engines[0].Detail = "no graph loaded"
	}

	if db == nil {
		for i := 1; i < len(engines); i++ {
			engines[i].Status = "disabled"
			engines[i].Detail = "no database"
		}
		return engines, nil
	}

	// Engine 2 — memory stats
	var obsCount int
	var recallRate float64
	db.QueryRow(context.Background(), `
		SELECT
			COUNT(*),
			COALESCE(COUNT(*) FILTER (WHERE recalled_at IS NOT NULL)::float / GREATEST(COUNT(*), 1), 0)
		FROM observations`).Scan(&obsCount, &recallRate)
	engines[1].Detail = fmt.Sprintf("%d observations, %.0f%% recall rate", obsCount, recallRate*100)

	// Engine 3 — skills stats
	var activeSkills, frozenSkills int
	db.QueryRow(context.Background(), `
		SELECT
			COUNT(*) FILTER (WHERE is_active AND NOT is_frozen),
			COUNT(*) FILTER (WHERE is_frozen)
		FROM skills`).Scan(&activeSkills, &frozenSkills)
	engines[2].Detail = fmt.Sprintf("%d active, %d frozen", activeSkills, frozenSkills)
	if frozenSkills > 0 {
		engines[2].Status = "degraded"
	}

	// Engine 4 — compression stats
	var samplesCount int
	var avgReduction float64
	db.QueryRow(context.Background(), `
		SELECT COUNT(*),
			COALESCE(AVG(1.0 - after_tokens::float / GREATEST(before_tokens, 1)) * 100, 0)
		FROM compression_samples WHERE created_at >= NOW() - INTERVAL '24h'
	`).Scan(&samplesCount, &avgReduction)
	engines[3].Detail = fmt.Sprintf("compact mode, %.0f%% reduction", avgReduction)

	// Engine 5 — plan bridge stats
	var planCount int
	var haikuPct, costToday float64
	db.QueryRow(context.Background(), `
		SELECT COUNT(*),
			COALESCE(COUNT(*) FILTER (WHERE executor_model ILIKE '%haiku%')::float / GREATEST(COUNT(*), 1) * 100, 0),
			COALESCE(SUM(estimated_total_cost), 0)
		FROM plan_costs WHERE created_at >= CURRENT_DATE
	`).Scan(&planCount, &haikuPct, &costToday)
	engines[4].Detail = fmt.Sprintf("%d plans today, $%.2f estimated, %.0f%% low-cost", planCount, costToday, haikuPct)

	return engines, nil
}

// ── Memory ────────────────────────────────────────────────────────────────────

func QueryObservations(db *pgxpool.Pool, f ObservationFilters) (*ObservationListResponse, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	from := f.From
	to := f.To
	if from.IsZero() {
		from = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if to.IsZero() {
		to = time.Now().Add(24 * time.Hour)
	}

	rows, err := db.Query(context.Background(), `
		SELECT id, developer_id, repo_id, graph_node_id, category,
		       summary, confidence, shared, created_at, recalled_at
		FROM observations
		WHERE ($1 = '' OR developer_id = $1)
		  AND ($2 = '' OR category = $2)
		  AND ($3 = '' OR graph_node_id = $3)
		  AND ($4 = '' OR repo_id = $4)
		  AND created_at >= $5 AND created_at <= $6
		ORDER BY created_at DESC
		LIMIT $7 OFFSET $8`,
		f.DeveloperID, f.Category, f.GraphNodeID, f.RepoID, from, to, f.Limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query observations: %w", err)
	}
	defer rows.Close()

	var obs []ObservationRow
	for rows.Next() {
		var o ObservationRow
		if err := rows.Scan(&o.ID, &o.DeveloperID, &o.RepoID, &o.GraphNodeID,
			&o.Category, &o.Summary, &o.Confidence, &o.Shared, &o.CreatedAt, &o.RecalledAt); err != nil {
			return nil, err
		}
		obs = append(obs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var total int
	db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM observations
		WHERE ($1 = '' OR developer_id = $1)
		  AND ($2 = '' OR category = $2)
		  AND ($3 = '' OR graph_node_id = $3)
		  AND ($4 = '' OR repo_id = $4)
		  AND created_at >= $5 AND created_at <= $6`,
		f.DeveloperID, f.Category, f.GraphNodeID, f.RepoID, from, to).Scan(&total)

	applied := map[string]string{}
	if f.DeveloperID != "" {
		applied["developer"] = f.DeveloperID
	}
	if f.Category != "" {
		applied["category"] = f.Category
	}
	if f.GraphNodeID != "" {
		applied["graph_node"] = f.GraphNodeID
	}

	if obs == nil {
		obs = []ObservationRow{}
	}
	return &ObservationListResponse{
		Observations:   obs,
		Total:          total,
		Page:           f.Page,
		Limit:          f.Limit,
		FiltersApplied: applied,
	}, nil
}

func QueryObservationDetail(db *pgxpool.Pool, id string) (*ObservationDetail, error) {
	var o ObservationDetail
	var toolCallsJSON []byte
	err := db.QueryRow(context.Background(), `
		SELECT id, developer_id, repo_id, graph_node_id, category,
		       summary, detail, confidence, shared, created_at, recalled_at,
		       COALESCE(session_id, ''), COALESCE(tool_calls::text, '[]')
		FROM observations WHERE id = $1`, id).
		Scan(&o.ID, &o.DeveloperID, &o.RepoID, &o.GraphNodeID,
			&o.Category, &o.Summary, &o.Detail, &o.Confidence, &o.Shared,
			&o.CreatedAt, &o.RecalledAt, &o.SessionID, &toolCallsJSON)
	if err != nil {
		return nil, fmt.Errorf("query observation detail: %w", err)
	}
	_ = json.Unmarshal(toolCallsJSON, &o.ToolCalls)
	return &o, nil
}

// ── Skills ────────────────────────────────────────────────────────────────────

func QuerySkills(db *pgxpool.Pool, f SkillFilters) (*SkillListResponse, error) {
	if db == nil {
		return &SkillListResponse{Skills: []SkillRow{}, Stats: SkillStats{ByEvolution: map[string]int{}}}, nil
	}

	sortExpr := "created_at DESC"
	switch f.Sort {
	case "success_rate":
		sortExpr = "times_succeeded::float / GREATEST(times_applied, 1) DESC, created_at DESC"
	case "confidence":
		sortExpr = "confidence DESC, created_at DESC"
	case "applied":
		sortExpr = "times_applied DESC, created_at DESC"
	}

	rows, err := db.Query(context.Background(), fmt.Sprintf(`
		SELECT id, name, version, evolution, COALESCE(language, ''), trigger_desc,
		       graph_node_ids,
		       confidence,
		       CASE WHEN times_applied > 0
		         THEN times_succeeded::float / times_applied
		         ELSE 0 END AS success_rate,
		       times_applied, times_succeeded,
		       is_frozen, created_at, COALESCE(created_by, '')
		FROM skills
		WHERE is_active = true
		  AND ($1 = '' OR language = $1)
		ORDER BY %s`, sortExpr), f.Language)
	if err != nil {
		return nil, fmt.Errorf("query skills: %w", err)
	}
	defer rows.Close()

	var skills []SkillRow
	for rows.Next() {
		var s SkillRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Version, &s.Evolution, &s.Language,
			&s.TriggerDesc, &s.GraphNodeIDs, &s.Confidence, &s.SuccessRate,
			&s.TimesApplied, &s.TimesSucceeded, &s.IsFrozen, &s.CreatedAt, &s.CreatedBy); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Stats
	var totalActive, totalFrozen int
	var avgSuccessRate float64
	db.QueryRow(context.Background(), `
		SELECT
			COUNT(*) FILTER (WHERE is_active AND NOT is_frozen),
			COUNT(*) FILTER (WHERE is_frozen AND is_active),
			COALESCE(AVG(CASE WHEN times_applied > 0 THEN times_succeeded::float/times_applied ELSE 0 END), 0)
		FROM skills WHERE is_active = true`).
		Scan(&totalActive, &totalFrozen, &avgSuccessRate)

	byEvolution := map[string]int{}
	evoRows, _ := db.Query(context.Background(),
		`SELECT evolution, COUNT(*) FROM skills WHERE is_active = true GROUP BY evolution`)
	if evoRows != nil {
		defer evoRows.Close()
		for evoRows.Next() {
			var evo string
			var cnt int
			if err := evoRows.Scan(&evo, &cnt); err == nil {
				byEvolution[evo] = cnt
			}
		}
	}

	if skills == nil {
		skills = []SkillRow{}
	}
	return &SkillListResponse{
		Skills: skills,
		Stats: SkillStats{
			TotalActive:    totalActive,
			TotalFrozen:    totalFrozen,
			ByEvolution:    byEvolution,
			AvgSuccessRate: avgSuccessRate,
		},
	}, nil
}

func QuerySkillDetail(db *pgxpool.Pool, id string) (*SkillDetail, error) {
	var s SkillDetail
	var negTagsJSON, complexJSON []byte
	var testCaseJSON []byte
	err := db.QueryRow(context.Background(), `
		SELECT id, name, version, COALESCE(parent_id::text, ''), evolution,
		       COALESCE(language, ''), trigger_desc, instruction,
		       graph_node_ids, confidence,
		       CASE WHEN times_applied > 0 THEN times_succeeded::float/times_applied ELSE 0 END,
		       times_applied, times_succeeded, is_frozen,
		       created_at, COALESCE(created_by, ''),
		       COALESCE(negative_tags::text, '[]'),
		       COALESCE(success_by_complexity::text, '{}'),
		       COALESCE(test_case::text, 'null')
		FROM skills WHERE id = $1`, id).
		Scan(&s.ID, &s.Name, &s.Version, new(string), &s.Evolution,
			&s.Language, &s.TriggerDesc, &s.Instruction,
			&s.GraphNodeIDs, &s.Confidence, &s.SuccessRate,
			&s.TimesApplied, &s.TimesSucceeded, &s.IsFrozen,
			&s.CreatedAt, &s.CreatedBy, &negTagsJSON, &complexJSON, &testCaseJSON)
	if err != nil {
		return nil, fmt.Errorf("query skill detail: %w", err)
	}
	_ = json.Unmarshal(negTagsJSON, &s.NegativeTags)
	_ = json.Unmarshal(complexJSON, &s.SuccessByComplexity)
	_ = json.Unmarshal(testCaseJSON, &s.TestCase)
	return &s, nil
}

func QuerySkillLineage(db *pgxpool.Pool, id string) (*SkillLineageResponse, error) {
	// Walk up to root, then collect all versions of that skill lineage
	rows, err := db.Query(context.Background(), `
		WITH RECURSIVE lineage AS (
			SELECT id, name, version, evolution, parent_id, created_by, created_at
			FROM skills WHERE id = $1
			UNION ALL
			SELECT s.id, s.name, s.version, s.evolution, s.parent_id, s.created_by, s.created_at
			FROM skills s JOIN lineage l ON s.id = l.parent_id
		)
		SELECT id, name, version, evolution, parent_id, created_by, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM lineage ORDER BY version ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("query skill lineage: %w", err)
	}
	defer rows.Close()

	var lineage []SkillLineageEntry
	for rows.Next() {
		var e SkillLineageEntry
		var parentID *string
		if err := rows.Scan(&e.ID, &e.Name, &e.Version, &e.Evolution,
			&parentID, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ParentID = parentID
		lineage = append(lineage, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Derived branches
	ids := make([]string, 0, len(lineage))
	for _, e := range lineage {
		ids = append(ids, e.ID)
	}
	var derived []SkillLineageEntry
	if len(ids) > 0 {
		dRows, err := db.Query(context.Background(), `
			SELECT id, name, version, evolution, parent_id, created_by, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
			FROM skills
			WHERE parent_id = ANY($1) AND evolution = 'derived'
			ORDER BY created_at ASC`, ids)
		if err == nil {
			defer dRows.Close()
			for dRows.Next() {
				var e SkillLineageEntry
				var parentID *string
				if err := dRows.Scan(&e.ID, &e.Name, &e.Version, &e.Evolution,
					&parentID, &e.CreatedBy, &e.CreatedAt); err == nil {
					e.ParentID = parentID
					derived = append(derived, e)
				}
			}
		}
	}

	if lineage == nil {
		lineage = []SkillLineageEntry{}
	}
	if derived == nil {
		derived = []SkillLineageEntry{}
	}
	return &SkillLineageResponse{Lineage: lineage, Derived: derived}, nil
}

// ── Compression ───────────────────────────────────────────────────────────────

func QueryCompressionSamples(db *pgxpool.Pool, limit int) (*CompressionSamplesResponse, error) {
	if limit <= 0 {
		limit = 10
	}
	if db == nil {
		return &CompressionSamplesResponse{Samples: []CompressionSample{}}, nil
	}

	rows, err := db.Query(context.Background(), `
		SELECT task_id, created_at, compression_level,
		       before_tokens, after_tokens,
		       ROUND((1.0 - after_tokens::float / GREATEST(before_tokens, 1)) * 100, 1) AS savings_pct,
		       before_preview, after_preview, COALESCE(graph_shorthand, '')
		FROM compression_samples
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return &CompressionSamplesResponse{Samples: []CompressionSample{}}, nil
	}
	defer rows.Close()

	var samples []CompressionSample
	for rows.Next() {
		var s CompressionSample
		if err := rows.Scan(&s.TaskID, &s.Timestamp, &s.Level,
			&s.BeforeTokens, &s.AfterTokens, &s.SavingsPct,
			&s.BeforePreview, &s.AfterPreview, &s.GraphShorthand); err != nil {
			return nil, err
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var stats CompressionStats
	db.QueryRow(context.Background(), `
		SELECT
			COALESCE(AVG(1.0 - after_tokens::float / GREATEST(before_tokens, 1)) * 100, 0),
			COALESCE(SUM(before_tokens - after_tokens), 0)
		FROM compression_samples WHERE created_at >= CURRENT_DATE`).
		Scan(&stats.AvgOutputReduction, &stats.TotalTokensSavedToday)
	stats.ActiveLevel = "compact"

	if samples == nil {
		samples = []CompressionSample{}
	}
	return &CompressionSamplesResponse{Samples: samples, Stats: stats}, nil
}

// ── Routing ───────────────────────────────────────────────────────────────────

func QueryRoutingTasks(db *pgxpool.Pool, f RoutingFilters) (*RoutingListResponse, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	from := f.From
	to := f.To
	if from.IsZero() {
		from = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if to.IsZero() {
		to = time.Now().Add(24 * time.Hour)
	}

	if db == nil {
		return &RoutingListResponse{Tasks: []RoutingTaskRow{}, Stats: RoutingStats{ByRoutingMode: map[string]int{}}}, nil
	}

	// Join plans with plan_costs to show cost info alongside plan status
	rows, err := db.Query(context.Background(), `
		SELECT
			p.id                                                 AS task_id,
			COALESCE(p.developer_id, '')                        AS developer_id,
			COALESCE(p.title, '')                               AS prompt_preview,
			p.status                                            AS routing_mode,
			COALESCE(pc.estimated_total_cost, 0)               AS total_cost,
			COALESCE(pc.estimated_all_premium_cost, 0)         AS would_have_cost,
			COALESCE(pc.memory_hit, false)                     AS memory_hit,
			p.created_at
		FROM plans p
		LEFT JOIN LATERAL (
			SELECT estimated_total_cost, estimated_all_premium_cost, memory_hit
			FROM plan_costs WHERE plan_id = p.id ORDER BY created_at DESC LIMIT 1
		) pc ON true
		WHERE ($1 = '' OR p.developer_id = $1)
		  AND ($2 = '' OR p.status = $2)
		  AND p.created_at >= $3 AND p.created_at <= $4
		ORDER BY p.created_at DESC
		LIMIT $5 OFFSET $6`,
		f.DeveloperID, f.RoutingMode, from, to, f.Limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query routing tasks: %w", err)
	}
	defer rows.Close()

	var tasks []RoutingTaskRow
	for rows.Next() {
		var t RoutingTaskRow
		if err := rows.Scan(&t.TaskID, &t.DeveloperID, &t.PromptPreview,
			&t.RoutingMode, &t.TotalCost, &t.WouldHaveCost,
			&t.MemoryHit, &t.CreatedAt); err != nil {
			return nil, err
		}
		// Fields not tracked in plan-based architecture
		t.TotalTokens = 0
		t.LatencyMS = 0
		t.EscalationSteps = 0
		t.WasTakeover = false
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var total int
	db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM plans
		WHERE ($1 = '' OR developer_id = $1)
		  AND ($2 = '' OR status = $2)
		  AND created_at >= $3 AND created_at <= $4`,
		f.DeveloperID, f.RoutingMode, from, to).Scan(&total)

	stats, _ := QueryRoutingStats(db)

	if tasks == nil {
		tasks = []RoutingTaskRow{}
	}
	return &RoutingListResponse{
		Tasks: tasks,
		Total: total,
		Page:  f.Page,
		Stats: stats,
	}, nil
}

func QueryRoutingDetail(db *pgxpool.Pool, taskID string) (*RoutingDetail, error) {
	if db == nil {
		return &RoutingDetail{TaskID: taskID, Trace: []RoutingTraceStep{}}, nil
	}

	var detail RoutingDetail
	detail.TaskID = taskID

	// Load plan details
	var steps []byte
	var status string
	err := db.QueryRow(context.Background(), `
		SELECT developer_id, task_prompt, status, steps,
		       COALESCE(planner_model, ''), COALESCE(executor_model, ''),
		       created_at
		FROM plans WHERE id = $1`, taskID).
		Scan(&detail.DeveloperID, &detail.Prompt, &status, &steps,
			&detail.RoutingMode, new(string), &detail.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("query plan detail: %w", err)
	}
	detail.RoutingMode = status

	// Build trace from plan steps
	var planSteps []string
	if err := json.Unmarshal(steps, &planSteps); err == nil {
		for i, step := range planSteps {
			detail.Trace = append(detail.Trace, RoutingTraceStep{
				Step:   i + 1,
				Action: "plan_step",
				Detail: step,
			})
		}
	}

	// Load cost info
	db.QueryRow(context.Background(), `
		SELECT COALESCE(estimated_total_cost, 0), COALESCE(estimated_all_premium_cost, 0)
		FROM plan_costs WHERE plan_id = $1 ORDER BY created_at DESC LIMIT 1`, taskID).
		Scan(&detail.TotalCost, &detail.WouldHaveCost)

	if detail.Trace == nil {
		detail.Trace = []RoutingTraceStep{}
	}
	return &detail, nil
}

func QueryRoutingStats(db *pgxpool.Pool) (RoutingStats, error) {
	var s RoutingStats
	s.ByRoutingMode = map[string]int{}
	if db == nil {
		return s, nil
	}

	db.QueryRow(context.Background(), `
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(COUNT(*) FILTER (WHERE executor_model ILIKE '%haiku%')::float / GREATEST(COUNT(*), 1) * 100, 0),
			COALESCE(SUM(estimated_total_cost), 0)
		FROM plan_costs WHERE created_at >= CURRENT_DATE`).
		Scan(&s.TasksToday, &s.HaikuPct, &s.CostToday)

	// ByRoutingMode maps to plan status counts
	statusRows, err := db.Query(context.Background(), `
		SELECT status, COUNT(*) FROM plans WHERE created_at >= CURRENT_DATE GROUP BY status`)
	if err == nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var mode string
			var cnt int
			if err := statusRows.Scan(&mode, &cnt); err == nil {
				s.ByRoutingMode[mode] = cnt
			}
		}
	}
	return s, nil
}

// ── Graph ─────────────────────────────────────────────────────────────────────

func QueryGraphNodesWithBadges(db *pgxpool.Pool, g *graph.Graph) (*GraphNodesResponse, error) {
	if g == nil {
		return &GraphNodesResponse{Nodes: []GraphNodeRow{}}, nil
	}

	// Collect all node IDs for bulk badge count query
	nodeIDs := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}

	memoryCounts := map[string]int{}
	skillCounts := map[string]int{}

	if db != nil && len(nodeIDs) > 0 {
		mRows, err := db.Query(context.Background(),
			`SELECT graph_node_id, COUNT(*) FROM observations WHERE graph_node_id = ANY($1) GROUP BY graph_node_id`,
			nodeIDs)
		if err == nil {
			defer mRows.Close()
			for mRows.Next() {
				var id string
				var cnt int
				if err := mRows.Scan(&id, &cnt); err == nil {
					memoryCounts[id] = cnt
				}
			}
		}

		sRows, err := db.Query(context.Background(),
			`SELECT unnest(graph_node_ids) AS nid, COUNT(*) FROM skills WHERE is_active = true GROUP BY nid HAVING unnest(graph_node_ids) = ANY($1)`,
			nodeIDs)
		if err == nil {
			defer sRows.Close()
			for sRows.Next() {
				var id string
				var cnt int
				if err := sRows.Scan(&id, &cnt); err == nil {
					skillCounts[id] = cnt
				}
			}
		}
	}

	nodes := make([]GraphNodeRow, 0, len(g.Nodes))
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		nodes = append(nodes, GraphNodeRow{
			ID:          id,
			Name:        n.Name,
			Kind:        string(n.Type),
			Package:     n.Package,
			File:        n.FilePath,
			MemoryCount: memoryCounts[id],
			SkillCount:  skillCounts[id],
		})
	}

	return &GraphNodesResponse{Nodes: nodes}, nil
}

func QueryGraphNodeDetail(db *pgxpool.Pool, g *graph.Graph, nodeID string) (*GraphNodeDetailResponse, error) {
	var detail GraphNodeDetailResponse

	if g != nil {
		n := g.GetNode(nodeID)
		if n != nil {
			detail.Node = GraphNodeRow{
				ID:      nodeID,
				Name:    n.Name,
				Kind:    string(n.Type),
				Package: n.Package,
				File:    n.FilePath,
			}
		}
		for _, dep := range g.GetDependents(nodeID) {
			if dep != nil {
				detail.Callers = append(detail.Callers, dep.ID)
			}
		}
		for _, dep := range g.GetDependencies(nodeID) {
			if dep != nil {
				detail.Callees = append(detail.Callees, dep.ID)
			}
		}
	}

	if db != nil {
		mRows, _ := db.Query(context.Background(), `
			SELECT id, developer_id, repo_id, graph_node_id, category,
			       summary, confidence, shared, created_at, recalled_at
			FROM observations WHERE graph_node_id = $1
			ORDER BY created_at DESC LIMIT 10`, nodeID)
		if mRows != nil {
			defer mRows.Close()
			for mRows.Next() {
				var o ObservationRow
				if err := mRows.Scan(&o.ID, &o.DeveloperID, &o.RepoID, &o.GraphNodeID,
					&o.Category, &o.Summary, &o.Confidence, &o.Shared, &o.CreatedAt, &o.RecalledAt); err == nil {
					detail.Memories = append(detail.Memories, o)
				}
			}
		}

		sRows, _ := db.Query(context.Background(), `
			SELECT id, name, version, evolution, COALESCE(language, ''), trigger_desc,
			       graph_node_ids, confidence,
			       CASE WHEN times_applied > 0 THEN times_succeeded::float/times_applied ELSE 0 END,
			       times_applied, times_succeeded, is_frozen, created_at, COALESCE(created_by, '')
			FROM skills WHERE $1 = ANY(graph_node_ids) AND is_active = true
			ORDER BY confidence DESC LIMIT 10`, nodeID)
		if sRows != nil {
			defer sRows.Close()
			for sRows.Next() {
				var s SkillRow
				if err := sRows.Scan(&s.ID, &s.Name, &s.Version, &s.Evolution, &s.Language,
					&s.TriggerDesc, &s.GraphNodeIDs, &s.Confidence, &s.SuccessRate,
					&s.TimesApplied, &s.TimesSucceeded, &s.IsFrozen, &s.CreatedAt, &s.CreatedBy); err == nil {
					detail.Skills = append(detail.Skills, s)
				}
			}
		}
	}

	if detail.Callers == nil {
		detail.Callers = []string{}
	}
	if detail.Callees == nil {
		detail.Callees = []string{}
	}
	if detail.Memories == nil {
		detail.Memories = []ObservationRow{}
	}
	if detail.Skills == nil {
		detail.Skills = []SkillRow{}
	}
	if detail.RecentRoutes == nil {
		detail.RecentRoutes = []RoutingTaskRow{}
	}
	return &detail, nil
}


// ── Plans ─────────────────────────────────────────────────────────────────────

func QueryPlansList(db *pgxpool.Pool, f PlanFilters) (*PlanListResponse, error) {
	if db == nil {
		return &PlanListResponse{Plans: []PlanRow{}, Stats: PlanStatsResponse{}}, nil
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	offset := (f.Page - 1) * f.Limit

	args := []interface{}{}
	where := "WHERE 1=1"
	if f.DeveloperID != "" {
		args = append(args, f.DeveloperID)
		where += fmt.Sprintf(" AND developer_id = $%d", len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}

	args = append(args, f.Limit, offset)
	limitArg := len(args) - 1
	offsetArg := len(args)

	rows, err := db.Query(context.Background(), fmt.Sprintf(`
		SELECT id, developer_id, title, status,
		       jsonb_array_length(steps),
		       skill_used IS NOT NULL, skill_verified, cross_repo,
		       COALESCE(risk_level,''), COALESCE(planner_model,''), COALESCE(executor_model,''),
		       result_success, result_tests, created_at, executed_at, verified_at
		FROM plans %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, limitArg, offsetArg), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []PlanRow
	for rows.Next() {
		var p PlanRow
		if err := rows.Scan(
			&p.ID, &p.DeveloperID, &p.Title, &p.Status,
			&p.StepCount, &p.SkillUsed, &p.SkillVerified, &p.CrossRepo,
			&p.RiskLevel, &p.PlannerModel, &p.ExecutorModel,
			&p.ResultSuccess, &p.ResultTests,
			&p.CreatedAt, &p.ExecutedAt, &p.VerifiedAt,
		); err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	if plans == nil {
		plans = []PlanRow{}
	}

	var total int
	countArgs := args[:len(args)-2]
	db.QueryRow(context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM plans %s", where), countArgs...).Scan(&total)

	stats, _ := QueryPlanStats(db, f.DeveloperID)
	if stats == nil {
		stats = &PlanStatsResponse{}
	}

	return &PlanListResponse{
		Plans: plans, Total: total, Page: f.Page, Limit: f.Limit, Stats: *stats,
	}, nil
}

func QueryPlanDetail(db *pgxpool.Pool, id string) (*PlanDetail, error) {
	if db == nil {
		return nil, fmt.Errorf("no database")
	}

	var d PlanDetail
	var stepsJSON []byte
	var filesToChangeJSON []byte
	var affectedNodesJSON []byte
	var status string

	err := db.QueryRow(context.Background(), `
		SELECT p.id, p.developer_id, p.title, p.status,
		       jsonb_array_length(p.steps),
		       p.skill_used IS NOT NULL, p.skill_verified, p.cross_repo,
		       COALESCE(p.risk_level,''), COALESCE(p.planner_model,''), COALESCE(p.executor_model,''),
		       p.result_success, p.result_tests, p.created_at, p.executed_at, p.verified_at,
		       p.task_prompt, p.steps,
		       to_json(COALESCE(p.files_to_change, ARRAY[]::TEXT[])),
		       COALESCE(p.graph_context,''),
		       to_json(COALESCE(p.affected_nodes, ARRAY[]::TEXT[])),
		       COALESCE(p.result_summary,''),
		       to_json(COALESCE(p.result_files, ARRAY[]::TEXT[])),
		       COALESCE(p.result_error,''), COALESCE(p.verification_note,''),
		       COALESCE(pc.estimated_total_cost, 0),
		       COALESCE(pc.estimated_all_premium_cost, 0),
		       COALESCE(pc.estimated_savings, 0)
		FROM plans p
		LEFT JOIN plan_costs pc ON pc.plan_id = p.id
		WHERE p.id = $1
		LIMIT 1`, id).Scan(
		&d.ID, &d.DeveloperID, &d.Title, &status,
		&d.StepCount, &d.SkillUsed, &d.SkillVerified, &d.CrossRepo,
		&d.RiskLevel, &d.PlannerModel, &d.ExecutorModel,
		&d.ResultSuccess, &d.ResultTests, &d.CreatedAt, &d.ExecutedAt, &d.VerifiedAt,
		&d.TaskPrompt, &stepsJSON, &filesToChangeJSON,
		&d.GraphContext, &affectedNodesJSON,
		&d.ResultSummary, &filesToChangeJSON, // reuse for result files below
		&d.ResultError, &d.VerificationNote,
		&d.EstimatedCost, &d.AllPremiumCost, &d.EstimatedSavings,
	)
	if err != nil {
		return nil, err
	}
	d.Status = status
	json.Unmarshal(stepsJSON, &d.Steps)
	json.Unmarshal(filesToChangeJSON, &d.FilesToChange)
	json.Unmarshal(affectedNodesJSON, &d.AffectedNodes)
	if d.Steps == nil {
		d.Steps = []string{}
	}
	if d.FilesToChange == nil {
		d.FilesToChange = []string{}
	}
	if d.AffectedNodes == nil {
		d.AffectedNodes = []string{}
	}
	return &d, nil
}

func QueryPlanStats(db *pgxpool.Pool, developerID string) (*PlanStatsResponse, error) {
	if db == nil {
		return &PlanStatsResponse{}, nil
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	if developerID != "" {
		args = append(args, developerID)
		where = "WHERE developer_id = $1"
	}

	var s PlanStatsResponse
	err := db.QueryRow(context.Background(), fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'completed'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'verified'),
			COUNT(*) FILTER (WHERE status = 'rejected'),
			COUNT(*) FILTER (WHERE status IN ('pending','executing')),
			COALESCE(AVG(jsonb_array_length(steps)), 0),
			COUNT(*) FILTER (WHERE skill_used IS NOT NULL),
			COUNT(*) FILTER (WHERE cross_repo)
		FROM plans %s`, where), args...).
		Scan(&s.TotalPlans, &s.Completed, &s.Failed, &s.Verified, &s.Rejected, &s.Pending,
			&s.AvgStepsPerPlan, &s.SkillUsedCount, &s.CrossRepoCount)
	if err != nil {
		return nil, err
	}

	denom := s.TotalPlans
	if denom > 0 {
		s.CompletionRate = float64(s.Completed+s.Verified) / float64(denom) * 100
		s.VerificationRate = float64(s.Verified) / float64(denom) * 100
	}
	return &s, nil
}
