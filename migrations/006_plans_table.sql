-- ============================================================
-- Engine 5 UPDATED: Plan Bridge + Cost Tracking
-- Migration 006: plans table + plan_costs (replaces agent_costs)
-- ============================================================

-- The plans table is the bridge between the planner agent and executor agent.
-- Opus writes a plan, stores it here. Haiku reads it, executes, stores result.
CREATE TABLE IF NOT EXISTS plans (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Who created the plan
    developer_id      TEXT NOT NULL,

    -- What the developer asked
    title             TEXT NOT NULL,
    task_prompt       TEXT NOT NULL,

    -- The plan itself — step by step instructions for the executor
    steps             JSONB NOT NULL,        -- ["step 1 text", "step 2 text", ...]

    -- Files the executor should touch
    files_to_change   TEXT[],

    -- Was a skill used in creating this plan?
    skill_used        UUID REFERENCES skills(id),
    skill_verified    BOOLEAN DEFAULT false,

    -- Graph context summary from the planner
    graph_context     TEXT,

    -- Blast radius info
    affected_nodes    TEXT[],
    cross_repo        BOOLEAN DEFAULT false,
    risk_level        TEXT,                  -- "low", "medium", "high"

    -- Which models were used (whatever the developer configured)
    planner_model     TEXT,
    executor_model    TEXT,

    -- Plan lifecycle status
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'executing', 'completed', 'failed', 'verified', 'rejected')),

    -- Executor result (filled by store_plan_result)
    result_success    BOOLEAN,
    result_summary    TEXT,
    result_files      TEXT[],
    result_tests      BOOLEAN,
    result_error      TEXT,

    -- Verification (filled when planner verifies)
    verified          BOOLEAN,
    verification_note TEXT,

    -- Timestamps
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    executed_at       TIMESTAMPTZ,
    verified_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_plans_developer ON plans(developer_id);
CREATE INDEX IF NOT EXISTS idx_plans_status ON plans(status);
CREATE INDEX IF NOT EXISTS idx_plans_created ON plans(created_at);
CREATE INDEX IF NOT EXISTS idx_plans_skill ON plans(skill_used);

-- ============================================================
-- Replace agent_costs with plan_costs
-- ============================================================

DROP TABLE IF EXISTS agent_costs CASCADE;
DROP MATERIALIZED VIEW IF EXISTS monthly_cost_summary;
DROP MATERIALIZED VIEW IF EXISTS developer_cost_summary;

CREATE TABLE IF NOT EXISTS plan_costs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id         UUID REFERENCES plans(id),
    developer_id    TEXT NOT NULL,

    -- Model info (from developer's config, self-reported)
    planner_model   TEXT,
    executor_model  TEXT,

    -- Cost estimates (calculated from config model pricing)
    estimated_planner_tokens  INT,
    estimated_executor_tokens INT,
    estimated_planner_cost    FLOAT,
    estimated_executor_cost   FLOAT,
    estimated_total_cost      FLOAT,

    -- What would it have cost if everything was on the premium model?
    estimated_all_premium_cost FLOAT,

    -- Savings
    estimated_savings          FLOAT,

    -- Was a skill used? (reduces tokens because agent follows recipe)
    skill_used      BOOLEAN DEFAULT false,

    -- Was memory helpful? (reduces tokens because agent skips investigation)
    memory_hit      BOOLEAN DEFAULT false,

    -- Routing mode (what the recommendation engine suggested)
    routing_recommendation TEXT,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plan_costs_developer ON plan_costs(developer_id);
CREATE INDEX IF NOT EXISTS idx_plan_costs_created ON plan_costs(created_at);
CREATE INDEX IF NOT EXISTS idx_plan_costs_plan ON plan_costs(plan_id);

-- Updated materialized views for dashboard
CREATE MATERIALIZED VIEW monthly_cost_summary AS
SELECT
    date_trunc('month', created_at) AS month,
    COUNT(*) AS total_plans,
    SUM(estimated_total_cost) AS actual_cost,
    SUM(estimated_all_premium_cost) AS would_have_cost,
    SUM(estimated_savings) AS savings,
    COUNT(*) FILTER (WHERE skill_used) AS skill_uses,
    COUNT(*) FILTER (WHERE memory_hit) AS memory_hits,
    AVG(estimated_total_cost) AS avg_cost_per_plan
FROM plan_costs
GROUP BY 1;

CREATE MATERIALIZED VIEW developer_cost_summary AS
SELECT
    developer_id,
    date_trunc('week', created_at) AS week,
    COUNT(*) AS total_plans,
    SUM(estimated_total_cost) AS actual_cost,
    SUM(estimated_savings) AS savings
FROM plan_costs
GROUP BY 1, 2;
