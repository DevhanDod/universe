-- ============================================================
-- Engine 2: Graph-Aware Persistent Memory
-- Migration: 002_memory_tables.sql
-- ============================================================

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS observations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    developer_id    TEXT NOT NULL,
    repo_id         TEXT NOT NULL,
    graph_node_id   TEXT NOT NULL,
    category        TEXT NOT NULL CHECK (category IN ('fix', 'pattern', 'decision', 'failure', 'convention')),
    summary         TEXT NOT NULL,
    detail          TEXT,
    embedding       vector(1536),
    session_id      TEXT,
    tool_calls      JSONB,
    confidence      FLOAT NOT NULL DEFAULT 1.0,
    shared          BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    recalled_at     TIMESTAMPTZ
);

CREATE INDEX idx_obs_graph_node ON observations(graph_node_id);
CREATE INDEX idx_obs_developer  ON observations(developer_id);
CREATE INDEX idx_obs_category   ON observations(category);
CREATE INDEX idx_obs_session    ON observations(session_id);
CREATE INDEX idx_obs_repo       ON observations(repo_id);

CREATE INDEX idx_obs_embedding ON observations
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

ALTER TABLE observations ADD COLUMN IF NOT EXISTS fts tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', summary || ' ' || COALESCE(detail, ''))
    ) STORED;

CREATE INDEX idx_obs_fts   ON observations USING gin(fts);
CREATE INDEX idx_obs_decay ON observations(confidence, created_at);
CREATE INDEX idx_obs_shared ON observations(shared) WHERE shared = true;
