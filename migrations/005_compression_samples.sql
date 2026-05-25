-- Migration: 005_compression_samples.sql
-- Compression sample logging for the dashboard Compression view.

CREATE TABLE IF NOT EXISTS compression_samples (
    id                UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           TEXT    NOT NULL,
    developer_id      TEXT    NOT NULL,
    compression_level TEXT    NOT NULL,   -- 'full', 'compact', 'normal'
    before_tokens     INT     NOT NULL,
    after_tokens      INT     NOT NULL,
    before_preview    TEXT    NOT NULL,   -- first 200 chars of the uncompressed prompt
    after_preview     TEXT    NOT NULL,   -- first 200 chars of the compressed prompt
    graph_shorthand   TEXT,               -- generated shorthand (if any)
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_comp_samples_created    ON compression_samples(created_at);
CREATE INDEX idx_comp_samples_developer  ON compression_samples(developer_id);

-- Add task prompt preview to agent_costs for routing trace display.
ALTER TABLE agent_costs ADD COLUMN IF NOT EXISTS task_prompt_preview TEXT;
