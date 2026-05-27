#!/bin/bash
# Universe — Engine Check Script
# Verifies all 5 engines are correctly implemented and functional.
# Run from the project root after building.

DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5433/universe"

PASS=0
FAIL=0

check() {
    local desc="$1"
    local cmd="$2"
    if eval "$cmd" >/dev/null 2>&1; then
        echo "  ✅ $desc"
        PASS=$((PASS + 1))
    else
        echo "  ❌ $desc"
        FAIL=$((FAIL + 1))
    fi
}

echo "╔═══════════════════════════════════════════════╗"
echo "║  Universe — Engine Check                      ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

# ── Engine 1: Knowledge Graph ─────────────────────────────────────────────────
echo "── Engine 1: Knowledge Graph ──"
check "graph package compiles"           "go build ./internal/graph/"
check "parser package compiles"          "go build ./internal/parser/"
check "analyzer package compiles"        "go build ./internal/analyzer/"
check "extractor package compiles"       "go build ./internal/extractor/"
check "graph/graph.go exists"            "test -f internal/graph/graph.go"
check "graph tests pass"                 "CGO_ENABLED=0 go test ./internal/graph/ -count=1"

# ── Engine 2: Persistent Memory ──────────────────────────────────────────────
echo ""
echo "── Engine 2: Persistent Memory ──"
check "memory package compiles"          "go build ./internal/memory/"
check "memory/store.go exists"           "test -f internal/memory/store.go"
check "memory/retriever.go exists"       "test -f internal/memory/retriever.go"
check "memory/hooks.go exists"           "test -f internal/memory/hooks.go"
check "observations table exists"        "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0;'"
check "memory tests pass"                "DATABASE_URL=$DB_URL CGO_ENABLED=0 go test ./internal/memory/ -count=1"

# ── Engine 3: Self-Evolving Skills ────────────────────────────────────────────
echo ""
echo "── Engine 3: Self-Evolving Skills ──"
check "skills package compiles"          "go build ./internal/skills/"
check "skills/store.go exists"           "test -f internal/skills/store.go"
check "skills/matcher.go exists"         "test -f internal/skills/matcher.go"
check "skills/executor.go exists"        "test -f internal/skills/executor.go"
check "skills/evolver.go exists"         "test -f internal/skills/evolver.go"
check "skills table exists"              "psql $DB_URL -c 'SELECT 1 FROM skills LIMIT 0;'"
check "skill_executions table exists"    "psql $DB_URL -c 'SELECT 1 FROM skill_executions LIMIT 0;'"

# ── Engine 4: Compression ─────────────────────────────────────────────────────
echo ""
echo "── Engine 4: Compression ──"
check ".cursor/rules/universe-compression.mdc exists" "test -f .cursor/rules/universe-compression.mdc"
check "compression.mdc has alwaysApply: true" "grep -q 'alwaysApply: true' .cursor/rules/universe-compression.mdc"

# ── Engine 5: Plan Bridge + Workspaces ───────────────────────────────────────
echo ""
echo "── Engine 5: Plan Bridge + Workspaces ──"
check "orchestrator/types.go exists"     "test -f internal/orchestrator/types.go"
check "orchestrator/router.go exists"    "test -f internal/orchestrator/router.go"
check "orchestrator/templates.go exists" "test -f internal/orchestrator/templates.go"
check "orchestrator/tracker.go exists"   "test -f internal/orchestrator/tracker.go"
check "orchestrator/plans.go exists"     "test -f internal/orchestrator/plans.go"
check "orchestrator/workspace.go exists" "test -f internal/orchestrator/workspace.go"
check "orchestrator/setup.go exists"     "test -f internal/orchestrator/setup.go"
check "orchestrator package compiles"    "CGO_ENABLED=0 go build ./internal/orchestrator/"
check "plans table exists"               "psql $DB_URL -c 'SELECT 1 FROM plans LIMIT 0;'"
check "plan_costs table exists"          "psql $DB_URL -c 'SELECT 1 FROM plan_costs LIMIT 0;'"
check "OLD files removed: no llmclient.go" "test ! -f internal/orchestrator/llmclient.go"
check "OLD files removed: no planner.go"   "test ! -f internal/orchestrator/planner.go"
check "OLD files removed: no executor.go"  "test ! -f internal/orchestrator/executor.go"
check "OLD files removed: no verifier.go"  "test ! -f internal/orchestrator/verifier.go"
check "OLD files removed: no escalation.go" "test ! -f internal/orchestrator/escalation.go"
check "OLD files removed: no parallel.go"  "test ! -f internal/orchestrator/parallel.go"
check "orchestrator tests pass"          "DATABASE_URL=$DB_URL CGO_ENABLED=0 go test ./internal/orchestrator/ -count=1"

# Engine 5 functional checks
echo ""
echo "── Engine 5 (Updated): Plans CRUD ──"
psql "$DB_URL" -c "
  INSERT INTO plans (developer_id, title, task_prompt, steps, status)
  VALUES ('engine-check', 'Engine check test', 'Test', '[\"step1\"]'::jsonb, 'pending')
  RETURNING id, title;" >/dev/null 2>&1 && echo "  ✅ Plan insert works" && PASS=$((PASS+1)) || { echo "  ❌ Plan insert failed"; FAIL=$((FAIL+1)); }

psql "$DB_URL" -c "SELECT id, title, status FROM plans WHERE developer_id = 'engine-check';" >/dev/null 2>&1 && \
    echo "  ✅ Plan select works" && PASS=$((PASS+1)) || { echo "  ❌ Plan select failed"; FAIL=$((FAIL+1)); }

psql "$DB_URL" -c "
  UPDATE plans SET status = 'completed', result_success = true, result_summary = 'engine-check-test'
  WHERE developer_id = 'engine-check';" >/dev/null 2>&1 && \
    echo "  ✅ Plan update works" && PASS=$((PASS+1)) || { echo "  ❌ Plan update failed"; FAIL=$((FAIL+1)); }

psql "$DB_URL" -c "DELETE FROM plans WHERE developer_id = 'engine-check';" >/dev/null 2>&1
echo "  Cleaned up test data"

# ── MCP Server ────────────────────────────────────────────────────────────────
echo ""
echo "── MCP Server ──"
check "mcpserver/server.go exists"       "test -f internal/mcpserver/server.go"
check "mcpserver/tools_graph.go exists"  "test -f internal/mcpserver/tools_graph.go"
check "mcpserver/tools_memory.go exists" "test -f internal/mcpserver/tools_memory.go"
check "mcpserver/tools_skills.go exists" "test -f internal/mcpserver/tools_skills.go"
check "mcpserver/tools_plans.go exists"  "test -f internal/mcpserver/tools_plans.go"
check "mcpserver package compiles"       "CGO_ENABLED=0 go build ./internal/mcpserver/"
check "mcpserver tests pass"             "DATABASE_URL=$DB_URL CGO_ENABLED=0 go test ./internal/mcpserver/ -count=1"

# MCP tool count check
echo ""
echo "── MCP Tool Count ──"
TOOL_COUNT=$(
(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"check","version":"1.0"}},"id":1}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"tools/list","id":2}'
 sleep 1) | \
  timeout 10 go run ./cmd/universe mcp --repo . 2>/dev/null | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 2 and 'result' in msg:
            print(len(msg['result'].get('tools', [])))
            break
    except: continue
" 2>/dev/null
)

if [ "$TOOL_COUNT" = "16" ]; then
    echo "  ✅ MCP registers $TOOL_COUNT tools (expected 16)"
    PASS=$((PASS + 1))
else
    echo "  ❌ MCP registers $TOOL_COUNT tools (expected 16)"
    echo "     Expected: 3 graph + 3 memory + 4 skills + 6 orchestrator = 16"
    FAIL=$((FAIL + 1))
fi

# ── Cross-Engine Integration Checks ──────────────────────────────────────────
echo ""
echo "── Cross-Engine Integration ──"
check "Full binary compiles"             "CGO_ENABLED=0 go build ./cmd/universe/"
check "All tables present" "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0; SELECT 1 FROM skills LIMIT 0; SELECT 1 FROM skill_executions LIMIT 0; SELECT 1 FROM plans LIMIT 0; SELECT 1 FROM plan_costs LIMIT 0;'"
check "Dashboard package compiles"       "CGO_ENABLED=0 go build ./internal/dashboard/"
check "CLI setup_cmd compiles"           "CGO_ENABLED=0 go vet ./cmd/universe/"
check "Workspace cmd exists"             "test -f cmd/universe/workspace_cmd.go"
check "Setup cmd exists"                 "test -f cmd/universe/setup_cmd.go"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "╔═══════════════════════════════════════════════╗"
printf  "║  Results: %-3d passed, %-3d failed              ║\n" $PASS $FAIL
echo "╚═══════════════════════════════════════════════╝"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "ALL CHECKS PASSED"
    echo ""
    echo "Next steps:"
    echo "  1. Run test-local.sh for full end-to-end verification"
    echo "  2. Set up npm distribution (npm-setup.md)"
    echo "  3. make release V=0.1.0"
    echo "  4. npm install -g @atlas/universe && universe setup"
else
    echo "WARNING: $FAIL checks failed"
    exit 1
fi
