-- ============================================================
-- Engine 3: Self-Evolving Skill Engine
-- Migration: 003_skills_tables.sql
-- ============================================================

CREATE TABLE IF NOT EXISTS skills (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    TEXT NOT NULL,
    version                 INT NOT NULL DEFAULT 1,
    parent_id               UUID REFERENCES skills(id),
    evolution               TEXT NOT NULL CHECK (evolution IN ('captured', 'fix', 'derived', 'manual')),
    graph_node_ids          TEXT[] NOT NULL,
    language                TEXT,
    trigger_desc            TEXT NOT NULL,
    instruction             TEXT NOT NULL,
    test_case               JSONB,
    negative_tags           JSONB DEFAULT '[]'::jsonb,
    embedding               vector(1536),
    created_by              TEXT NOT NULL,
    shared                  BOOLEAN NOT NULL DEFAULT true,
    is_active               BOOLEAN NOT NULL DEFAULT true,
    is_frozen               BOOLEAN NOT NULL DEFAULT false,
    evolution_attempts_today INT NOT NULL DEFAULT 0,
    times_applied           INT NOT NULL DEFAULT 0,
    times_succeeded         INT NOT NULL DEFAULT 0,
    times_failed            INT NOT NULL DEFAULT 0,
    success_by_complexity   JSONB DEFAULT '{"simple":{"applied":0,"succeeded":0},"complex":{"applied":0,"succeeded":0}}'::jsonb,
    avg_tokens_saved        FLOAT DEFAULT 0,
    confidence              FLOAT NOT NULL DEFAULT 0.5,
    last_applied_at         TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(name, version)
);

CREATE TABLE IF NOT EXISTS skill_executions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID REFERENCES skills(id),
    developer_id    TEXT NOT NULL,
    session_id      TEXT,
    success         BOOLEAN NOT NULL,
    tokens_used     INT,
    error_detail    TEXT,
    complexity      TEXT CHECK (complexity IN ('simple', 'complex')),
    graph_node_ids  TEXT[],
    task_prompt     TEXT,
    task_output     TEXT,
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_skills_graph_nodes ON skills USING gin(graph_node_ids);
CREATE INDEX idx_skills_name        ON skills(name);
CREATE INDEX idx_skills_active      ON skills(is_active) WHERE is_active = true;
CREATE INDEX idx_skills_language    ON skills(language);
CREATE INDEX idx_skills_parent      ON skills(parent_id);
CREATE INDEX idx_skills_evolution   ON skills(evolution);

CREATE INDEX idx_skills_embedding ON skills
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 50);

ALTER TABLE skills ADD COLUMN fts tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', trigger_desc || ' ' || instruction)
    ) STORED;
CREATE INDEX idx_skills_fts        ON skills USING gin(fts);
CREATE INDEX idx_skills_confidence ON skills(confidence, last_applied_at);
CREATE INDEX idx_skills_frozen     ON skills(is_frozen) WHERE is_frozen = true;

CREATE INDEX idx_executions_skill     ON skill_executions(skill_id);
CREATE INDEX idx_executions_developer ON skill_executions(developer_id);
CREATE INDEX idx_executions_session   ON skill_executions(session_id);
CREATE INDEX idx_executions_success   ON skill_executions(success);
