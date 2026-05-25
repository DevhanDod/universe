-- ============================================================
-- Engine 5: Tiered Agent Orchestration — Cost Tracking
-- Migration: 004_cost_tracking.sql
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_costs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id          TEXT NOT NULL,
    developer_id     TEXT NOT NULL,
    model            TEXT NOT NULL CHECK (model IN ('opus', 'haiku')),
    input_tokens     INT NOT NULL,
    output_tokens    INT NOT NULL,
    cost_usd         FLOAT NOT NULL,
    phase            TEXT NOT NULL CHECK (phase IN (
                         'plan', 'execute', 'verify', 'escalate', 'takeover', 'direct'
                     )),
    routing_mode     TEXT NOT NULL,
    skill_id         UUID,
    memory_hit       BOOLEAN NOT NULL DEFAULT false,
    escalation_steps INT NOT NULL DEFAULT 0,
    was_takeover     BOOLEAN NOT NULL DEFAULT false,
    latency_ms       INT,
    repo_id          TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_costs_developer ON agent_costs(developer_id);
CREATE INDEX idx_costs_model     ON agent_costs(model);
CREATE INDEX idx_costs_routing   ON agent_costs(routing_mode);
CREATE INDEX idx_costs_created   ON agent_costs(created_at);
CREATE INDEX idx_costs_task      ON agent_costs(task_id);

-- ============================================================
-- Materialized views for dashboard queries
-- ============================================================

CREATE MATERIALIZED VIEW monthly_cost_summary AS
SELECT
    date_trunc('month', created_at)                        AS month,
    COUNT(*)                                               AS total_calls,
    SUM(cost_usd)                                          AS actual_cost,
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    )                                                      AS would_have_cost_all_opus,
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    ) - SUM(cost_usd)                                      AS savings_usd,
    COUNT(*) FILTER (WHERE routing_mode = 'skill_execute') AS skill_executions,
    COUNT(*) FILTER (WHERE routing_mode = 'memory_apply')  AS memory_applies,
    COUNT(*) FILTER (WHERE routing_mode = 'plan_execute')  AS plan_executes,
    COUNT(*) FILTER (WHERE routing_mode = 'full_orchestration') AS full_orchestrations,
    COUNT(*) FILTER (WHERE routing_mode = 'single_opus')   AS single_opus,
    COUNT(*) FILTER (WHERE was_takeover)                   AS takeovers,
    COUNT(*) FILTER (WHERE memory_hit)                     AS memory_hits,
    COUNT(*) FILTER (WHERE skill_id IS NOT NULL)           AS skill_uses,
    SUM(cost_usd) FILTER (WHERE model = 'opus')            AS opus_cost,
    SUM(cost_usd) FILTER (WHERE model = 'haiku')           AS haiku_cost,
    AVG(latency_ms)                                        AS avg_latency_ms
FROM agent_costs
GROUP BY 1;

CREATE MATERIALIZED VIEW developer_cost_summary AS
SELECT
    developer_id,
    date_trunc('week', created_at)                         AS week,
    COUNT(*)                                               AS total_calls,
    SUM(cost_usd)                                          AS actual_cost,
    SUM(
        (input_tokens * 15.0 / 1000000) +
        (output_tokens * 75.0 / 1000000)
    ) - SUM(cost_usd)                                      AS savings_usd,
    COUNT(*) FILTER (WHERE was_takeover)                   AS takeovers
FROM agent_costs
GROUP BY 1, 2;

CREATE MATERIALIZED VIEW routing_effectiveness AS
SELECT
    routing_mode,
    COUNT(*)                                               AS total_uses,
    AVG(cost_usd)                                          AS avg_cost,
    AVG(latency_ms)                                        AS avg_latency,
    AVG(escalation_steps)                                  AS avg_escalations,
    COUNT(*) FILTER (WHERE was_takeover)                   AS takeovers,
    SUM(cost_usd)                                          AS total_cost
FROM agent_costs
GROUP BY 1;
