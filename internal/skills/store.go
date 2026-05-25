package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Store handles all database operations for skills and executions.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new Store connected to PostgreSQL.
func NewStore(databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to skills db: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping skills db: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// ── SKILL CRUD ────────────────────────────────────────────────────────────────

// InsertSkill stores a new skill version.
func (s *Store) InsertSkill(skill Skill) (*Skill, error) {
	testCaseJSON, _ := json.Marshal(skill.TestCase)
	negTagsJSON, _ := json.Marshal(skill.NegativeTags)
	complexJSON, _ := json.Marshal(skill.SuccessByComplexity)

	var vecParam interface{}
	if len(skill.Embedding) > 0 {
		v := pgvector.NewVector(skill.Embedding)
		vecParam = v
	}

	row := s.pool.QueryRow(context.Background(), `
		INSERT INTO skills
			(name, version, parent_id, evolution, graph_node_ids, language,
			 trigger_desc, instruction, test_case, negative_tags, embedding,
			 created_by, shared, is_active, confidence, success_by_complexity)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, created_at`,
		skill.Name, skill.Version, skill.ParentID, string(skill.Evolution),
		skill.GraphNodeIDs, nullStr(skill.Language),
		skill.TriggerDesc, skill.Instruction,
		testCaseJSON, negTagsJSON, vecParam,
		skill.CreatedBy, skill.Shared, skill.IsActive,
		skill.Confidence, complexJSON,
	)
	if err := row.Scan(&skill.ID, &skill.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert skill: %w", err)
	}
	return &skill, nil
}

// GetByID retrieves a skill by UUID.
func (s *Store) GetByID(id string) (*Skill, error) {
	return s.scanSkill(s.pool.QueryRow(context.Background(), skillSelectSQL+` WHERE id = $1`, id))
}

// GetActiveByName retrieves the active version of a named skill.
func (s *Store) GetActiveByName(name string) (*Skill, error) {
	return s.scanSkill(s.pool.QueryRow(context.Background(), skillSelectSQL+` WHERE name = $1 AND is_active = true`, name))
}

// GetByGraphNodes retrieves all active skills covering any of the given graph nodes.
func (s *Store) GetByGraphNodes(nodeIDs []string, language string, minConfidence float64, limit int) ([]Skill, error) {
	var rows pgx.Rows
	var err error
	if language == "" {
		rows, err = s.pool.Query(context.Background(), skillSelectSQL+`
			WHERE is_active = true
			  AND graph_node_ids && $1
			  AND confidence >= $2
			ORDER BY confidence DESC, times_succeeded DESC
			LIMIT $3`, nodeIDs, minConfidence, limit)
	} else {
		rows, err = s.pool.Query(context.Background(), skillSelectSQL+`
			WHERE is_active = true
			  AND graph_node_ids && $1
			  AND (language IS NULL OR language = $2)
			  AND confidence >= $3
			ORDER BY confidence DESC, times_succeeded DESC
			LIMIT $4`, nodeIDs, language, minConfidence, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("get by graph nodes: %w", err)
	}
	defer rows.Close()
	return s.scanSkills(rows)
}

// SearchKeyword performs full-text search.
func (s *Store) SearchKeyword(query string, minConfidence float64, limit int) ([]SkillSummary, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, name, version, evolution, trigger_desc, language,
		       confidence, times_applied, times_succeeded, is_frozen,
		       negative_tags, success_by_complexity,
		       ts_rank(fts, plainto_tsquery('english', $1)) AS score
		FROM skills
		WHERE fts @@ plainto_tsquery('english', $1)
		  AND is_active = true
		  AND confidence >= $2
		ORDER BY score DESC
		LIMIT $3`, query, minConfidence, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search skills: %w", err)
	}
	defer rows.Close()
	return s.scanSummaries(rows, true)
}

// SearchSemantic performs vector similarity search.
func (s *Store) SearchSemantic(queryEmbedding []float32, minConfidence float64, limit int) ([]SkillSummary, error) {
	vec := pgvector.NewVector(queryEmbedding)
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, name, version, evolution, trigger_desc, language,
		       confidence, times_applied, times_succeeded, is_frozen,
		       negative_tags, success_by_complexity,
		       1 - (embedding <=> $1) AS score
		FROM skills
		WHERE is_active = true
		  AND confidence >= $2
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $3`, vec, minConfidence, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search skills: %w", err)
	}
	defer rows.Close()
	return s.scanSummaries(rows, true)
}

// GetLineage retrieves the full evolution history of a skill via recursive CTE.
func (s *Store) GetLineage(skillID string) ([]Skill, error) {
	rows, err := s.pool.Query(context.Background(), `
		WITH RECURSIVE lineage AS (
			SELECT `+skillColumns+` FROM skills WHERE id = $1
			UNION ALL
			SELECT `+skillColumnsAliased+` FROM skills s JOIN lineage l ON s.id = l.parent_id
		)
		SELECT * FROM lineage ORDER BY version ASC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("get lineage: %w", err)
	}
	defer rows.Close()
	return s.scanSkills(rows)
}

// GetChildren retrieves all skills that evolved from a given skill.
func (s *Store) GetChildren(skillID string) ([]Skill, error) {
	rows, err := s.pool.Query(context.Background(), skillSelectSQL+` WHERE parent_id = $1`, skillID)
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	defer rows.Close()
	return s.scanSkills(rows)
}

// CountSkillsForGraphNode returns how many active skills cover a graph node.
func (s *Store) CountSkillsForGraphNode(nodeID string) (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skills WHERE graph_node_ids @> ARRAY[$1] AND is_active = true`, nodeID).Scan(&n)
	return n, err
}

// GetTotalActiveSkills returns the total number of active skills.
func (s *Store) GetTotalActiveSkills() (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skills WHERE is_active = true`).Scan(&n)
	return n, err
}

// ── SKILL UPDATES ─────────────────────────────────────────────────────────────

// DeactivateSkill marks a skill as inactive.
func (s *Store) DeactivateSkill(id string) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE skills SET is_active = false WHERE id = $1`, id)
	return err
}

// FreezeSkill marks a skill as frozen.
func (s *Store) FreezeSkill(id string) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE skills SET is_frozen = true WHERE id = $1`, id)
	return err
}

// UnfreezeSkill unfreezes a skill and resets daily attempts.
func (s *Store) UnfreezeSkill(id string) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE skills SET is_frozen = false, evolution_attempts_today = 0 WHERE id = $1`, id)
	return err
}

// IncrementEvolutionAttempts adds 1 to evolution_attempts_today.
func (s *Store) IncrementEvolutionAttempts(id string) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE skills SET evolution_attempts_today = evolution_attempts_today + 1 WHERE id = $1`, id)
	return err
}

// ResetDailyEvolutionAttempts resets all skills' daily counters to 0.
func (s *Store) ResetDailyEvolutionAttempts() error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE skills SET evolution_attempts_today = 0`)
	return err
}

// UpdateMetrics updates quality metrics after an execution.
func (s *Store) UpdateMetrics(id string, success bool, complexity string, tokensSaved float64) error {
	if success {
		_, err := s.pool.Exec(context.Background(), `
			UPDATE skills SET
				times_applied     = times_applied + 1,
				times_succeeded   = times_succeeded + 1,
				avg_tokens_saved  = (avg_tokens_saved * times_applied + $2) / (times_applied + 1),
				confidence        = LEAST($3, confidence + $4),
				last_applied_at   = NOW(),
				success_by_complexity = CASE
					WHEN $5 = 'simple' THEN jsonb_set(jsonb_set(success_by_complexity,
						'{simple,applied}', ((success_by_complexity->'simple'->>'applied')::int + 1)::text::jsonb),
						'{simple,succeeded}', ((success_by_complexity->'simple'->>'succeeded')::int + 1)::text::jsonb)
					WHEN $5 = 'complex' THEN jsonb_set(jsonb_set(success_by_complexity,
						'{complex,applied}', ((success_by_complexity->'complex'->>'applied')::int + 1)::text::jsonb),
						'{complex,succeeded}', ((success_by_complexity->'complex'->>'succeeded')::int + 1)::text::jsonb)
					ELSE success_by_complexity
				END
			WHERE id = $1`,
			id, tokensSaved, 1.0, 0.1, complexity)
		return err
	}
	_, err := s.pool.Exec(context.Background(), `
		UPDATE skills SET
			times_applied   = times_applied + 1,
			times_failed    = times_failed + 1,
			last_applied_at = NOW(),
			success_by_complexity = CASE
				WHEN $2 = 'simple' THEN jsonb_set(success_by_complexity,
					'{simple,applied}', ((success_by_complexity->'simple'->>'applied')::int + 1)::text::jsonb)
				WHEN $2 = 'complex' THEN jsonb_set(success_by_complexity,
					'{complex,applied}', ((success_by_complexity->'complex'->>'applied')::int + 1)::text::jsonb)
				ELSE success_by_complexity
			END
		WHERE id = $1`, id, complexity)
	return err
}

// AddNegativeTag adds a "don't use when" tag to a skill.
func (s *Store) AddNegativeTag(id string, tag NegativeTag) error {
	tagJSON, err := json.Marshal(tag)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`UPDATE skills SET negative_tags = negative_tags || $2::jsonb WHERE id = $1`,
		id, fmt.Sprintf("[%s]", tagJSON))
	return err
}

// MarkGraphNodesStale flags all skills covering the given graph nodes as stale.
func (s *Store) MarkGraphNodesStale(changedNodeIDs []string) error {
	tag := NegativeTag{
		Context: "graph_changed",
		Reason:  "code changed in the graph",
		AddedAt: time.Now().UTC().Format(time.RFC3339),
	}
	tagJSON, _ := json.Marshal(tag)
	_, err := s.pool.Exec(context.Background(), `
		UPDATE skills
		SET negative_tags = negative_tags || $2::jsonb
		WHERE graph_node_ids && $1
		  AND is_active = true`,
		changedNodeIDs, fmt.Sprintf("[%s]", tagJSON))
	return err
}

// ── EXECUTION LOG ─────────────────────────────────────────────────────────────

// InsertExecution logs a skill application.
func (s *Store) InsertExecution(exec SkillExecution) (*SkillExecution, error) {
	var skillID *string
	if exec.SkillID != "" {
		skillID = &exec.SkillID
	}
	row := s.pool.QueryRow(context.Background(), `
		INSERT INTO skill_executions
			(skill_id, developer_id, session_id, success, tokens_used,
			 error_detail, complexity, graph_node_ids, task_prompt, task_output)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, executed_at`,
		skillID, exec.DeveloperID, exec.SessionID, exec.Success, exec.TokensUsed,
		nullStr(exec.ErrorDetail), nullStr(exec.Complexity), exec.GraphNodeIDs,
		nullStr(exec.TaskPrompt), nullStr(exec.TaskOutput),
	)
	if err := row.Scan(&exec.ID, &exec.ExecutedAt); err != nil {
		return nil, fmt.Errorf("insert execution: %w", err)
	}
	return &exec, nil
}

// GetRecentExecutions retrieves recent executions for a skill.
func (s *Store) GetRecentExecutions(skillID string, limit int) ([]SkillExecution, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, COALESCE(skill_id::text,''), developer_id, COALESCE(session_id,''),
		       success, COALESCE(tokens_used,0), COALESCE(error_detail,''),
		       COALESCE(complexity,''), COALESCE(graph_node_ids,'{}'),
		       COALESCE(task_prompt,''), COALESCE(task_output,''), executed_at
		FROM skill_executions
		WHERE skill_id = $1
		ORDER BY executed_at DESC
		LIMIT $2`, skillID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanExecutions(rows)
}

// GetConsecutiveFailures returns the number of consecutive failures from the most recent.
func (s *Store) GetConsecutiveFailures(skillID string) (int, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT success FROM skill_executions
		WHERE skill_id = $1
		ORDER BY executed_at DESC
		LIMIT 20`, skillID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var ok bool
		if err := rows.Scan(&ok); err != nil {
			return count, err
		}
		if !ok {
			count++
		} else {
			break
		}
	}
	return count, rows.Err()
}

// GetRecentFailureRate returns the failure rate over the last N executions.
func (s *Store) GetRecentFailureRate(skillID string, lastN int) (float64, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT success FROM skill_executions
		WHERE skill_id = $1
		ORDER BY executed_at DESC
		LIMIT $2`, skillID, lastN)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total, failed := 0, 0
	for rows.Next() {
		var ok bool
		if err := rows.Scan(&ok); err != nil {
			return 0, err
		}
		total++
		if !ok {
			failed++
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(failed) / float64(total), rows.Err()
}

// GetSuccessfulSessionsWithoutSkill retrieves successful executions with no skill matched.
func (s *Store) GetSuccessfulSessionsWithoutSkill(limit int) ([]SkillExecution, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, COALESCE(skill_id::text,''), developer_id, COALESCE(session_id,''),
		       success, COALESCE(tokens_used,0), COALESCE(error_detail,''),
		       COALESCE(complexity,''), COALESCE(graph_node_ids,'{}'),
		       COALESCE(task_prompt,''), COALESCE(task_output,''), executed_at
		FROM skill_executions
		WHERE skill_id IS NULL
		  AND success = true
		  AND task_prompt IS NOT NULL
		ORDER BY executed_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanExecutions(rows)
}

// ── PRUNING ───────────────────────────────────────────────────────────────────

// PruneUnusedSkills deletes active skills with 0 applications after N days.
func (s *Store) PruneUnusedSkills(afterDays int) (int, error) {
	tag, err := s.pool.Exec(context.Background(), `
		DELETE FROM skills
		WHERE is_active = true
		  AND times_applied = 0
		  AND created_at < NOW() - make_interval(days => $1)`, afterDays)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ── STATS ─────────────────────────────────────────────────────────────────────

// GetStats returns summary statistics about the skill store.
func (s *Store) GetStats() (*SkillStats, error) {
	stats := &SkillStats{
		ByEvolution: make(map[string]int),
		ByLanguage:  make(map[string]int),
	}

	row := s.pool.QueryRow(context.Background(), `
		SELECT
			COUNT(*) FILTER (WHERE is_active = true),
			COUNT(*) FILTER (WHERE is_frozen = true),
			COALESCE(AVG(confidence) FILTER (WHERE is_active = true), 0),
			COALESCE(AVG(CASE WHEN times_applied > 0 THEN times_succeeded::float/times_applied ELSE NULL END), 0)
		FROM skills`)
	if err := row.Scan(&stats.TotalActive, &stats.TotalFrozen, &stats.AvgConfidence, &stats.AvgSuccessRate); err != nil {
		return nil, fmt.Errorf("get skill stats: %w", err)
	}

	rows, _ := s.pool.Query(context.Background(), `SELECT evolution, COUNT(*) FROM skills WHERE is_active=true GROUP BY evolution`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var k string
			var v int
			rows.Scan(&k, &v)
			stats.ByEvolution[k] = v
		}
	}

	rows2, _ := s.pool.Query(context.Background(), `SELECT COALESCE(language,'unknown'), COUNT(*) FROM skills WHERE is_active=true GROUP BY language`)
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var k string
			var v int
			rows2.Scan(&k, &v)
			stats.ByLanguage[k] = v
		}
	}

	s.pool.QueryRow(context.Background(), `SELECT COUNT(*), COALESCE(SUM(tokens_used),0) FROM skill_executions`).
		Scan(&stats.TotalExecutions, &stats.TotalTokensSaved)

	return stats, nil
}

// ── scan helpers ──────────────────────────────────────────────────────────────

const skillColumns = `id, name, version, parent_id, evolution, graph_node_ids, language,
	trigger_desc, instruction, test_case, negative_tags, embedding,
	created_by, shared, is_active, is_frozen, evolution_attempts_today,
	times_applied, times_succeeded, times_failed,
	success_by_complexity, avg_tokens_saved, confidence, last_applied_at, created_at`

const skillColumnsAliased = `s.id, s.name, s.version, s.parent_id, s.evolution, s.graph_node_ids, s.language,
	s.trigger_desc, s.instruction, s.test_case, s.negative_tags, s.embedding,
	s.created_by, s.shared, s.is_active, s.is_frozen, s.evolution_attempts_today,
	s.times_applied, s.times_succeeded, s.times_failed,
	s.success_by_complexity, s.avg_tokens_saved, s.confidence, s.last_applied_at, s.created_at`

const skillSelectSQL = `SELECT ` + skillColumns + ` FROM skills `

func (s *Store) scanSkill(row pgx.Row) (*Skill, error) {
	var sk Skill
	var vec *pgvector.Vector
	var testCaseJSON, negTagsJSON, complexJSON []byte
	var language *string
	if err := row.Scan(
		&sk.ID, &sk.Name, &sk.Version, &sk.ParentID, &sk.Evolution,
		&sk.GraphNodeIDs, &language,
		&sk.TriggerDesc, &sk.Instruction, &testCaseJSON, &negTagsJSON, &vec,
		&sk.CreatedBy, &sk.Shared, &sk.IsActive, &sk.IsFrozen, &sk.EvolutionAttemptsToday,
		&sk.TimesApplied, &sk.TimesSucceeded, &sk.TimesFailed,
		&complexJSON, &sk.AvgTokensSaved, &sk.Confidence, &sk.LastAppliedAt, &sk.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	if language != nil {
		sk.Language = *language
	}
	if vec != nil {
		sk.Embedding = vec.Slice()
	}
	if len(testCaseJSON) > 0 && string(testCaseJSON) != "null" {
		json.Unmarshal(testCaseJSON, &sk.TestCase)
	}
	if len(negTagsJSON) > 0 {
		json.Unmarshal(negTagsJSON, &sk.NegativeTags)
	}
	if len(complexJSON) > 0 {
		json.Unmarshal(complexJSON, &sk.SuccessByComplexity)
	}
	return &sk, nil
}

func (s *Store) scanSkills(rows pgx.Rows) ([]Skill, error) {
	var out []Skill
	for rows.Next() {
		var sk Skill
		var vec *pgvector.Vector
		var testCaseJSON, negTagsJSON, complexJSON []byte
		var language *string
		if err := rows.Scan(
			&sk.ID, &sk.Name, &sk.Version, &sk.ParentID, &sk.Evolution,
			&sk.GraphNodeIDs, &language,
			&sk.TriggerDesc, &sk.Instruction, &testCaseJSON, &negTagsJSON, &vec,
			&sk.CreatedBy, &sk.Shared, &sk.IsActive, &sk.IsFrozen, &sk.EvolutionAttemptsToday,
			&sk.TimesApplied, &sk.TimesSucceeded, &sk.TimesFailed,
			&complexJSON, &sk.AvgTokensSaved, &sk.Confidence, &sk.LastAppliedAt, &sk.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan skill row: %w", err)
		}
		if language != nil {
			sk.Language = *language
		}
		if vec != nil {
			sk.Embedding = vec.Slice()
		}
		if len(testCaseJSON) > 0 && string(testCaseJSON) != "null" {
			json.Unmarshal(testCaseJSON, &sk.TestCase)
		}
		if len(negTagsJSON) > 0 {
			json.Unmarshal(negTagsJSON, &sk.NegativeTags)
		}
		if len(complexJSON) > 0 {
			json.Unmarshal(complexJSON, &sk.SuccessByComplexity)
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) scanSummaries(rows pgx.Rows, hasScore bool) ([]SkillSummary, error) {
	var out []SkillSummary
	for rows.Next() {
		var sum SkillSummary
		var applied, succeeded int
		var score float64
		var language *string
		var negTagsJSON, complexJSON []byte
		if hasScore {
			if err := rows.Scan(
				&sum.ID, &sum.Name, &sum.Version, &sum.Evolution,
				&sum.TriggerDesc, &language, &sum.Confidence,
				&applied, &succeeded, &sum.IsFrozen,
				&negTagsJSON, &complexJSON, &score,
			); err != nil {
				return nil, err
			}
			sum.SearchScore = score
		} else {
			if err := rows.Scan(
				&sum.ID, &sum.Name, &sum.Version, &sum.Evolution,
				&sum.TriggerDesc, &language, &sum.Confidence,
				&applied, &succeeded, &sum.IsFrozen,
				&negTagsJSON, &complexJSON,
			); err != nil {
				return nil, err
			}
		}
		if language != nil {
			sum.Language = *language
		}
		sum.TimesApplied = applied
		if applied > 0 {
			sum.SuccessRate = float64(succeeded) / float64(applied)
		}
		if len(negTagsJSON) > 0 {
			json.Unmarshal(negTagsJSON, &sum.NegativeTags)
		}
		if len(complexJSON) > 0 {
			json.Unmarshal(complexJSON, &sum.SuccessByComplexity)
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func (s *Store) scanExecutions(rows pgx.Rows) ([]SkillExecution, error) {
	var out []SkillExecution
	for rows.Next() {
		var ex SkillExecution
		if err := rows.Scan(
			&ex.ID, &ex.SkillID, &ex.DeveloperID, &ex.SessionID,
			&ex.Success, &ex.TokensUsed, &ex.ErrorDetail,
			&ex.Complexity, &ex.GraphNodeIDs,
			&ex.TaskPrompt, &ex.TaskOutput, &ex.ExecutedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
