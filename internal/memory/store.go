package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Store handles all database operations for observations.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new Store connected to PostgreSQL.
func NewStore(databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close closes the database connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// InsertObservation stores a new observation in the database.
func (s *Store) InsertObservation(obs Observation) (*Observation, error) {
	toolCallsJSON, err := json.Marshal(obs.ToolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal tool_calls: %w", err)
	}

	var vecParam interface{}
	if len(obs.Embedding) > 0 {
		v := pgvector.NewVector(obs.Embedding)
		vecParam = v
	}

	row := s.pool.QueryRow(context.Background(), `
		INSERT INTO observations
			(developer_id, repo_id, graph_node_id, category, summary, detail,
			 embedding, session_id, tool_calls, confidence, shared)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at`,
		obs.DeveloperID, obs.RepoID, obs.GraphNodeID, obs.Category,
		obs.Summary, obs.Detail, vecParam, obs.SessionID,
		toolCallsJSON, obs.Confidence, obs.Shared,
	)

	if err := row.Scan(&obs.ID, &obs.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert observation: %w", err)
	}
	return &obs, nil
}

// InsertBatch stores multiple observations in one database round-trip.
func (s *Store) InsertBatch(observations []Observation) ([]Observation, error) {
	if len(observations) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	for _, obs := range observations {
		toolCallsJSON, err := json.Marshal(obs.ToolCalls)
		if err != nil {
			return nil, fmt.Errorf("marshal tool_calls: %w", err)
		}
		var vecParam interface{}
		if len(obs.Embedding) > 0 {
			v := pgvector.NewVector(obs.Embedding)
			vecParam = v
		}
		batch.Queue(`
			INSERT INTO observations
				(developer_id, repo_id, graph_node_id, category, summary, detail,
				 embedding, session_id, tool_calls, confidence, shared)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, created_at`,
			obs.DeveloperID, obs.RepoID, obs.GraphNodeID, obs.Category,
			obs.Summary, obs.Detail, vecParam, obs.SessionID,
			toolCallsJSON, obs.Confidence, obs.Shared,
		)
	}

	br := s.pool.SendBatch(context.Background(), batch)
	defer br.Close()

	result := make([]Observation, len(observations))
	for i := range observations {
		result[i] = observations[i]
		if err := br.QueryRow().Scan(&result[i].ID, &result[i].CreatedAt); err != nil {
			return nil, fmt.Errorf("batch insert row %d: %w", i, err)
		}
	}
	return result, nil
}

// GetByID retrieves a single observation by its UUID.
func (s *Store) GetByID(id string) (*Observation, error) {
	obs, err := s.scanObservation(s.pool.QueryRow(context.Background(), `
		SELECT id, developer_id, repo_id, graph_node_id, category, summary, detail,
		       embedding, session_id, tool_calls, confidence, shared, created_at, recalled_at
		FROM observations WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return obs, nil
}

// GetByIDs retrieves multiple observations by their UUIDs.
func (s *Store) GetByIDs(ids []string) ([]Observation, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, developer_id, repo_id, graph_node_id, category, summary, detail,
		       embedding, session_id, tool_calls, confidence, shared, created_at, recalled_at
		FROM observations WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("get by ids: %w", err)
	}
	defer rows.Close()
	return s.scanObservations(rows)
}

// GetByGraphNode retrieves observations tagged to a specific graph node.
func (s *Store) GetByGraphNode(graphNodeID string, developerID string, limit int) ([]ObservationSummary, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, graph_node_id, category, summary, confidence, created_at
		FROM observations
		WHERE graph_node_id = $1
		  AND developer_id = $2
		  AND confidence > 0.1
		ORDER BY confidence DESC, created_at DESC
		LIMIT $3`, graphNodeID, developerID, limit)
	if err != nil {
		return nil, fmt.Errorf("get by graph node: %w", err)
	}
	defer rows.Close()
	return s.scanSummaries(rows, 1.0)
}

// GetByGraphNodes retrieves observations for multiple graph nodes at once.
func (s *Store) GetByGraphNodes(graphNodeIDs []string, developerID string, limit int) ([]ObservationSummary, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, graph_node_id, category, summary, confidence, created_at
		FROM observations
		WHERE graph_node_id = ANY($1)
		  AND developer_id = $2
		  AND confidence > 0.1
		ORDER BY confidence DESC, created_at DESC
		LIMIT $3`, graphNodeIDs, developerID, limit)
	if err != nil {
		return nil, fmt.Errorf("get by graph nodes: %w", err)
	}
	defer rows.Close()
	return s.scanSummaries(rows, 1.0)
}

// SearchKeyword performs full-text keyword search using PostgreSQL FTS.
func (s *Store) SearchKeyword(query string, developerID string, limit int) ([]ObservationSummary, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, graph_node_id, category, summary, confidence, created_at,
		       ts_rank(fts, plainto_tsquery('english', $1)) AS score
		FROM observations
		WHERE fts @@ plainto_tsquery('english', $1)
		  AND developer_id = $2
		  AND confidence > 0.1
		ORDER BY score DESC
		LIMIT $3`, query, developerID, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()
	return s.scanSummariesWithScore(rows)
}

// SearchSemantic performs vector similarity search using pgvector.
func (s *Store) SearchSemantic(queryEmbedding []float32, developerID string, limit int) ([]ObservationSummary, error) {
	vec := pgvector.NewVector(queryEmbedding)
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, graph_node_id, category, summary, confidence, created_at,
		       1 - (embedding <=> $1) AS score
		FROM observations
		WHERE developer_id = $2
		  AND confidence > 0.1
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $3`, vec, developerID, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	defer rows.Close()
	return s.scanSummariesWithScore(rows)
}

// TouchRecalled updates the recalled_at timestamp and boosts confidence.
func (s *Store) TouchRecalled(id string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE observations
		SET recalled_at = NOW(),
		    confidence  = LEAST(1.0, confidence + 0.1)
		WHERE id = $1`, id)
	return err
}

// UpdateConfidenceBatch updates confidence scores for multiple observations.
func (s *Store) UpdateConfidenceBatch(updates map[string]float64) error {
	if len(updates) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for id, conf := range updates {
		batch.Queue(`UPDATE observations SET confidence = $1 WHERE id = $2`, conf, id)
	}
	br := s.pool.SendBatch(context.Background(), batch)
	defer br.Close()
	for range updates {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("update confidence batch: %w", err)
		}
	}
	return nil
}

// DeleteByConfidence deletes observations below the minimum confidence threshold.
func (s *Store) DeleteByConfidence(minConfidence float64) (int, error) {
	tag, err := s.pool.Exec(context.Background(),
		`DELETE FROM observations WHERE confidence < $1`, minConfidence)
	if err != nil {
		return 0, fmt.Errorf("delete by confidence: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// DeleteByID deletes a specific observation.
func (s *Store) DeleteByID(id string) error {
	_, err := s.pool.Exec(context.Background(),
		`DELETE FROM observations WHERE id = $1`, id)
	return err
}

// GetStats returns summary statistics about the memory store.
func (s *Store) GetStats() (*MemoryStats, error) {
	stats := &MemoryStats{
		ByCategory: make(map[string]int),
		ByRepo:     make(map[string]int),
	}

	row := s.pool.QueryRow(context.Background(), `
		SELECT
			COUNT(*),
			COALESCE(AVG(confidence), 0),
			MIN(created_at),
			MAX(created_at),
			COUNT(*) FILTER (WHERE recalled_at IS NOT NULL)
		FROM observations`)
	if err := row.Scan(
		&stats.TotalObservations,
		&stats.AverageConfidence,
		&stats.OldestObservation,
		&stats.NewestObservation,
		&stats.TotalRecalls,
	); err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	rows, err := s.pool.Query(context.Background(),
		`SELECT category, COUNT(*) FROM observations GROUP BY category`)
	if err != nil {
		return nil, fmt.Errorf("get stats by category: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var cnt int
		if err := rows.Scan(&cat, &cnt); err != nil {
			return nil, err
		}
		stats.ByCategory[cat] = cnt
	}

	rows2, err := s.pool.Query(context.Background(),
		`SELECT repo_id, COUNT(*) FROM observations GROUP BY repo_id`)
	if err != nil {
		return nil, fmt.Errorf("get stats by repo: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var repo string
		var cnt int
		if err := rows2.Scan(&repo, &cnt); err != nil {
			return nil, err
		}
		stats.ByRepo[repo] = cnt
	}

	return stats, nil
}

// ── internal scan helpers ─────────────────────────────────────────────────────

func (s *Store) scanObservation(row pgx.Row) (*Observation, error) {
	var obs Observation
	var vec *pgvector.Vector
	var toolCallsJSON []byte
	if err := row.Scan(
		&obs.ID, &obs.DeveloperID, &obs.RepoID, &obs.GraphNodeID,
		&obs.Category, &obs.Summary, &obs.Detail,
		&vec, &obs.SessionID, &toolCallsJSON,
		&obs.Confidence, &obs.Shared, &obs.CreatedAt, &obs.RecalledAt,
	); err != nil {
		return nil, fmt.Errorf("scan observation: %w", err)
	}
	if vec != nil {
		obs.Embedding = vec.Slice()
	}
	if len(toolCallsJSON) > 0 {
		_ = json.Unmarshal(toolCallsJSON, &obs.ToolCalls)
	}
	return &obs, nil
}

func (s *Store) scanObservations(rows pgx.Rows) ([]Observation, error) {
	var out []Observation
	for rows.Next() {
		var obs Observation
		var vec *pgvector.Vector
		var toolCallsJSON []byte
		if err := rows.Scan(
			&obs.ID, &obs.DeveloperID, &obs.RepoID, &obs.GraphNodeID,
			&obs.Category, &obs.Summary, &obs.Detail,
			&vec, &obs.SessionID, &toolCallsJSON,
			&obs.Confidence, &obs.Shared, &obs.CreatedAt, &obs.RecalledAt,
		); err != nil {
			return nil, fmt.Errorf("scan observation row: %w", err)
		}
		if vec != nil {
			obs.Embedding = vec.Slice()
		}
		if len(toolCallsJSON) > 0 {
			_ = json.Unmarshal(toolCallsJSON, &obs.ToolCalls)
		}
		out = append(out, obs)
	}
	return out, rows.Err()
}

func (s *Store) scanSummaries(rows pgx.Rows, defaultScore float64) ([]ObservationSummary, error) {
	var out []ObservationSummary
	for rows.Next() {
		var sum ObservationSummary
		if err := rows.Scan(
			&sum.ID, &sum.GraphNodeID, &sum.Category,
			&sum.Summary, &sum.Confidence, &sum.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan summary row: %w", err)
		}
		sum.Score = defaultScore
		out = append(out, sum)
	}
	return out, rows.Err()
}

func (s *Store) scanSummariesWithScore(rows pgx.Rows) ([]ObservationSummary, error) {
	var out []ObservationSummary
	for rows.Next() {
		var sum ObservationSummary
		if err := rows.Scan(
			&sum.ID, &sum.GraphNodeID, &sum.Category,
			&sum.Summary, &sum.Confidence, &sum.CreatedAt, &sum.Score,
		); err != nil {
			return nil, fmt.Errorf("scan summary+score row: %w", err)
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// scanObservationForDecay is a lightweight scan used by decay.go.
type decayRow struct {
	ID         string
	Confidence float64
	RecalledAt *time.Time
	CreatedAt  time.Time
}

func (s *Store) queryDecayRows(minConfidence float64) ([]decayRow, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, confidence, recalled_at, created_at
		FROM observations
		WHERE confidence > $1
		  AND (recalled_at IS NULL OR recalled_at < NOW() - interval '1 day')`, minConfidence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []decayRow
	for rows.Next() {
		var r decayRow
		if err := rows.Scan(&r.ID, &r.Confidence, &r.RecalledAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) countObservations() (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM observations`).Scan(&n)
	return n, err
}
