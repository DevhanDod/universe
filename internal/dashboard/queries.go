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
	// Current month totals from agent_costs directly (materialized view may be stale)
	row := db.QueryRow(context.Background(), `
		SELECT
			COALESCE(COUNT(DISTINCT task_id), 0)                                                  AS total_tasks,
			COALESCE(SUM(cost_usd), 0)                                                            AS actual_cost,
			COALESCE(SUM((input_tokens * 15.0 / 1000000) + (output_tokens * 75.0 / 1000000)), 0) AS would_have,
			COALESCE(COUNT(*) FILTER (WHERE skill_id IS NOT NULL), 0)                             AS skill_uses,
			COALESCE(COUNT(*) FILTER (WHERE memory_hit), 0)                                       AS memory_hits,
			COALESCE(COUNT(*) FILTER (WHERE was_takeover), 0)                                     AS takeovers,
			COALESCE(
				COUNT(*) FILTER (WHERE model = 'haiku')::float /
				GREATEST(COUNT(*), 1) * 100, 0)                                                   AS haiku_pct
		FROM agent_costs
		WHERE created_at >= date_trunc('month', NOW())
	`)
	var wouldHave float64
	if err := row.Scan(&s.TotalTasks, &s.ActualCost, &wouldHave,
		&s.SkillUses, &s.MemoryHits, &s.Takeovers, &s.HaikuPct); err != nil {
		return s, fmt.Errorf("query monthly summary: %w", err)
	}
	s.WouldHaveCost = wouldHave
	s.SavingsUSD = wouldHave - s.ActualCost
	if wouldHave > 0 {
		s.SavingsPct = s.SavingsUSD / wouldHave * 100
	}
	return s, nil
}

func QueryMonthlyTrend(db *pgxpool.Pool) ([]MonthlyDataPoint, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(context.Background(), `
		SELECT
			to_char(date_trunc('month', created_at), 'Mon') AS month,
			COALESCE(SUM(cost_usd), 0)                      AS actual,
			COALESCE(SUM(
				(input_tokens * 15.0 / 1000000) +
				(output_tokens * 75.0 / 1000000)
			), 0)                                           AS would_have
		FROM agent_costs
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

	// Engine 5 — routing stats
	var taskCount int
	var haikuPct, costToday float64
	db.QueryRow(context.Background(), `
		SELECT COUNT(DISTINCT task_id),
			COALESCE(COUNT(*) FILTER (WHERE model='haiku')::float / GREATEST(COUNT(*), 1) * 100, 0),
			COALESCE(SUM(cost_usd), 0)
		FROM agent_costs WHERE created_at >= CURRENT_DATE
	`).Scan(&taskCount, &haikuPct, &costToday)
	engines[4].Detail = fmt.Sprintf("%.0f%% low-cost, $%.2f today", haikuPct, costToday)

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

	rows, err := db.Query(context.Background(), `
		SELECT
			task_id,
			COALESCE(MIN(developer_id), ''),
			COALESCE(MIN(task_prompt_preview), ''),
			COALESCE(MIN(routing_mode), ''),
			COALESCE(SUM(input_tokens + output_tokens), 0)          AS total_tokens,
			COALESCE(SUM(cost_usd), 0)                              AS total_cost,
			COALESCE(SUM((input_tokens*15.0/1000000)+(output_tokens*75.0/1000000)), 0) AS would_have,
			COALESCE(SUM(latency_ms), 0)                            AS total_latency,
			COALESCE(MAX(escalation_steps), 0)                      AS esc_steps,
			BOOL_OR(was_takeover)                                   AS takeover,
			BOOL_OR(memory_hit)                                     AS mem_hit,
			MIN(created_at)                                         AS first_at
		FROM agent_costs
		WHERE ($1 = '' OR developer_id = $1)
		  AND ($2 = '' OR routing_mode = $2)
		  AND created_at >= $3 AND created_at <= $4
		GROUP BY task_id
		ORDER BY MIN(created_at) DESC
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
			&t.RoutingMode, &t.TotalTokens, &t.TotalCost, &t.WouldHaveCost,
			&t.LatencyMS, &t.EscalationSteps, &t.WasTakeover, &t.MemoryHit, &t.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var total int
	db.QueryRow(context.Background(), `
		SELECT COUNT(DISTINCT task_id) FROM agent_costs
		WHERE ($1 = '' OR developer_id = $1)
		  AND ($2 = '' OR routing_mode = $2)
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
	rows, err := db.Query(context.Background(), `
		SELECT phase, model, input_tokens + output_tokens,
		       COALESCE(latency_ms, 0), cost_usd,
		       COALESCE(task_prompt_preview, ''), routing_mode, memory_hit,
		       escalation_steps, was_takeover, created_at
		FROM agent_costs WHERE task_id = $1
		ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query routing detail: %w", err)
	}
	defer rows.Close()

	var detail RoutingDetail
	detail.TaskID = taskID
	step := 0
	var totalCost, wouldHave float64
	var totalTokens, totalLatency int

	for rows.Next() {
		step++
		var phase, model, prompt, routingMode string
		var tokens, latency int
		var cost float64
		var memHit, takeover bool
		var escSteps int
		var createdAt time.Time
		if err := rows.Scan(&phase, &model, &tokens, &latency, &cost,
			&prompt, &routingMode, &memHit, &escSteps, &takeover, &createdAt); err != nil {
			return nil, err
		}
		if detail.DeveloperID == "" {
			// developer_id not stored per-row in this query — pull separately
		}
		if detail.Prompt == "" && prompt != "" {
			detail.Prompt = prompt
		}
		if detail.RoutingMode == "" {
			detail.RoutingMode = routingMode
		}
		if detail.CreatedAt.IsZero() {
			detail.CreatedAt = createdAt
		}
		totalCost += cost
		totalTokens += tokens
		totalLatency += latency
		wouldHave += float64(tokens) * 15.0 / 1000000 // rough approximation

		detail.Trace = append(detail.Trace, RoutingTraceStep{
			Step:       step,
			Action:     phase,
			Detail:     fmt.Sprintf("%s phase", phase),
			Tokens:     tokens,
			Model:      model,
			DurationMS: latency,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch developer_id separately
	db.QueryRow(context.Background(),
		`SELECT COALESCE(MIN(developer_id), '') FROM agent_costs WHERE task_id = $1`, taskID).
		Scan(&detail.DeveloperID)

	detail.TotalCost = totalCost
	detail.WouldHaveCost = wouldHave
	detail.TotalTokens = totalTokens
	detail.TotalLatencyMS = totalLatency

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
			COALESCE(COUNT(DISTINCT task_id), 0),
			COALESCE(COUNT(*) FILTER (WHERE model='haiku')::float / GREATEST(COUNT(*), 1) * 100, 0),
			COALESCE(SUM(cost_usd), 0),
			COALESCE(COUNT(*) FILTER (WHERE was_takeover), 0)
		FROM agent_costs WHERE created_at >= CURRENT_DATE`).
		Scan(&s.TasksToday, &s.HaikuPct, &s.CostToday, &s.TakeoversToday)

	modeRows, err := db.Query(context.Background(), `
		SELECT routing_mode, COUNT(DISTINCT task_id)
		FROM agent_costs WHERE created_at >= CURRENT_DATE
		GROUP BY routing_mode`)
	if err == nil {
		defer modeRows.Close()
		for modeRows.Next() {
			var mode string
			var cnt int
			if err := modeRows.Scan(&mode, &cnt); err == nil {
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
