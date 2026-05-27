#!/bin/bash
# Universe — Local End-to-End Test (v2)
# Runs 10 tests to verify the full system works end-to-end.
# Prerequisites: Docker running, Go installed, Python 3 installed.

set -e

DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5433/universe"

echo "╔═══════════════════════════════════════════════╗"
echo "║  Universe — Local End-to-End Test (v2)        ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

PASS=0
FAIL=0

run_test() {
    echo ""
    echo "════════════════════════════════════════════"
    echo "TEST $1: $2"
    echo "════════════════════════════════════════════"
}

pass() { echo "✅ PASS"; PASS=$((PASS + 1)); }
fail() { echo "❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

# ── Test 1: Binary compiles ──────────────────────────────────────────────────
run_test 1 "Binary compiles"
rm -f universe
CGO_ENABLED=0 go build -ldflags "-X main.Version=test-local" -o universe ./cmd/universe
./universe --version
pass

# ── Test 2: Universe init ────────────────────────────────────────────────────
run_test 2 "Universe init"
rm -rf .universe
./universe init
if test -f .universe/graph.json; then
    NODE_COUNT=$(python3 -c "import json; g=json.load(open('.universe/graph.json')); print(len(g.get('nodes', g) if isinstance(g, dict) and 'nodes' in g else g))" 2>/dev/null || echo "?")
    echo "✅ graph.json created ($NODE_COUNT nodes)"
    pass
else
    fail "graph.json not found"
fi

# ── Test 3: Docker PostgreSQL ────────────────────────────────────────────────
run_test 3 "Docker PostgreSQL"
docker compose up -d 2>/dev/null || docker-compose up -d 2>/dev/null
sleep 3
./universe config set db "$DB_URL"
if ./universe db status 2>&1 | grep -qi "connected\|ok\|success\|reachable"; then
    pass
else
    # Try direct psql ping
    if psql "$DB_URL" -c "SELECT 1;" >/dev/null 2>&1; then
        echo "✅ Database reachable via psql"
        pass
    else
        fail "Cannot reach PostgreSQL at $DB_URL"
    fi
fi

# ── Test 4: Database migrations ──────────────────────────────────────────────
run_test 4 "Database migrations (including plans table)"
./universe db migrate

PLANS_TABLE=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='plans';" 2>/dev/null | tr -d ' \n')
if [ "$PLANS_TABLE" = "1" ]; then
    echo "✅ plans table created"
else
    echo "❌ plans table MISSING"
    FAIL=$((FAIL + 1))
fi

PLAN_COSTS_TABLE=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='plan_costs';" 2>/dev/null | tr -d ' \n')
if [ "$PLAN_COSTS_TABLE" = "1" ]; then
    echo "✅ plan_costs table created"
else
    echo "❌ plan_costs table MISSING"
    FAIL=$((FAIL + 1))
fi

pass

# ── Test 5: Universe status ──────────────────────────────────────────────────
run_test 5 "Universe status (real data + model config)"
./universe status
pass

# ── Test 6: Universe setup ───────────────────────────────────────────────────
run_test 6 "Universe setup generates config files"
rm -rf .universe/workspaces
rm -f .cursor/rules/universe-planner.mdc .cursor/rules/universe-executor.mdc .cursor/rules/universe-compression.mdc

./universe setup --premium "claude-opus-4" --execution "claude-haiku-3.5"

SETUP_OK=true

if [ -f ".universe/workspaces/planner.code-workspace" ]; then
    echo "✅ planner.code-workspace created"
    if grep -q "claude-opus-4" .universe/workspaces/planner.code-workspace; then
        echo "   ✅ Contains premium model: claude-opus-4"
    else
        echo "   ❌ Missing premium model in planner workspace"
        SETUP_OK=false
    fi
else
    echo "❌ planner.code-workspace MISSING"
    SETUP_OK=false
fi

if [ -f ".universe/workspaces/executor.code-workspace" ]; then
    echo "✅ executor.code-workspace created"
    if grep -q "claude-haiku-3.5" .universe/workspaces/executor.code-workspace; then
        echo "   ✅ Contains execution model: claude-haiku-3.5"
    else
        echo "   ❌ Missing execution model in executor workspace"
        SETUP_OK=false
    fi
else
    echo "❌ executor.code-workspace MISSING"
    SETUP_OK=false
fi

for rule in "universe-planner.mdc" "universe-executor.mdc" "universe-compression.mdc"; do
    if [ -f ".cursor/rules/$rule" ]; then
        echo "✅ .cursor/rules/$rule created"
    else
        echo "❌ .cursor/rules/$rule MISSING"
        SETUP_OK=false
    fi
done

if grep -q "claude-opus-4" .cursor/rules/universe-planner.mdc 2>/dev/null; then
    echo "✅ Planner rule references premium model"
else
    echo "❌ Planner rule missing model reference"
    SETUP_OK=false
fi

if grep -q "claude-haiku-3.5" .cursor/rules/universe-executor.mdc 2>/dev/null; then
    echo "✅ Executor rule references execution model"
else
    echo "❌ Executor rule missing model reference"
    SETUP_OK=false
fi

if grep -q "alwaysApply: true" .cursor/rules/universe-compression.mdc 2>/dev/null; then
    echo "✅ Compression rule has alwaysApply: true"
else
    echo "❌ Compression rule missing alwaysApply: true"
    SETUP_OK=false
fi

if [ -f ".cursor/mcp.json" ]; then
    echo "✅ .cursor/mcp.json created"
    if grep -q "universe" .cursor/mcp.json && grep -q "mcp" .cursor/mcp.json; then
        echo "   ✅ Contains universe MCP server config"
    else
        echo "   ❌ MCP config doesn't reference universe"
        SETUP_OK=false
    fi
else
    echo "❌ .cursor/mcp.json MISSING"
    SETUP_OK=false
fi

./universe config get models
echo ""

if $SETUP_OK; then
    echo "✅ PASS: Universe setup generated all files"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL: Some setup files missing or incorrect"
    FAIL=$((FAIL + 1))
fi

# ── Test 7: Dashboard ────────────────────────────────────────────────────────
run_test 7 "Dashboard serves (with plans API)"
./universe dashboard --port 3001 --no-open &
DPID=$!
sleep 3
HTML_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/ 2>/dev/null)
API_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/overview 2>/dev/null)
PLANS_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/plans 2>/dev/null)
kill $DPID 2>/dev/null; wait $DPID 2>/dev/null
if [ "$HTML_CODE" = "200" ] && [ "$API_CODE" = "200" ] && [ "$PLANS_CODE" = "200" ]; then
    echo "✅ PASS — HTML:$HTML_CODE  overview API:$API_CODE  plans API:$PLANS_CODE"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — HTML:$HTML_CODE API:$API_CODE Plans:$PLANS_CODE"
    FAIL=$((FAIL + 1))
fi

# ── Test 8: MCP handshake + 16 tools ────────────────────────────────────────
run_test 8 "MCP handshake + 16 tools registered"
TOOL_COUNT=$(
(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"tools/list","id":2}'
 sleep 1) | \
  timeout 10 ./universe mcp --repo . 2>/dev/null | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 2 and 'result' in msg:
            tools = msg['result'].get('tools', [])
            names = [t['name'] for t in tools]
            plan_tools = ['store_plan', 'get_plan', 'store_plan_result', 'get_plan_result', 'verify_plan']
            for pt in plan_tools:
                status = 'OK' if pt in names else 'MISSING'
                print(f'  plan tool {pt}: {status}', file=sys.stderr)
            print(len(tools))
            break
    except: continue
" 2>&1
)
TOOL_NUM=$(echo "$TOOL_COUNT" | grep -E '^[0-9]+$' | head -1)
echo "$TOOL_COUNT" | grep "plan tool" || true
if [ "$TOOL_NUM" = "16" ]; then
    echo "✅ PASS — $TOOL_NUM tools registered (expected 16)"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — $TOOL_NUM tools registered (expected 16)"
    FAIL=$((FAIL + 1))
fi

# ── Test 9: Plan tools round-trip ────────────────────────────────────────────
run_test 9 "Plan tools: store → get → result → verify"
PLAN_RESULT=$(
(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"store_plan","arguments":{"title":"E2E test plan","task_prompt":"Fix the test","steps":["Step 1: change file","Step 2: run tests"],"risk_level":"low"}},"id":10}'
 sleep 1
 echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_plan","arguments":{}},"id":20}'
 sleep 1) | \
  timeout 15 ./universe mcp --repo . 2>/dev/null | \
  python3 -c "
import sys, json
store_ok = False
get_ok = False
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 10 and 'result' in msg:
            for c in msg['result'].get('content', []):
                if c.get('type') == 'text':
                    data = json.loads(c['text'])
                    if data.get('plan_id'):
                        store_ok = True
        if msg.get('id') == 20 and 'result' in msg:
            for c in msg['result'].get('content', []):
                if c.get('type') == 'text':
                    data = json.loads(c['text'])
                    if data.get('found') and data.get('steps'):
                        get_ok = True
    except: continue
if store_ok and get_ok:
    print('PASS')
elif store_ok:
    print('PARTIAL:store ok but get failed')
else:
    print('FAIL')
" 2>/dev/null
)
if [ "$PLAN_RESULT" = "PASS" ]; then
    echo "✅ PASS — store_plan + get_plan round-trip works"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — plan round-trip: $PLAN_RESULT"
    FAIL=$((FAIL + 1))
fi

# Clean up test plans
psql "$DB_URL" -c "DELETE FROM plans WHERE title = 'E2E test plan';" 2>/dev/null || true

# ── Test 10: Workspace files valid ───────────────────────────────────────────
run_test 10 "Workspace files are valid and correctly configured"
WS_OK=true

if python3 -c "import json; json.load(open('.universe/workspaces/planner.code-workspace'))" 2>/dev/null; then
    echo "✅ planner.code-workspace is valid JSON"
else
    echo "❌ planner.code-workspace is not valid JSON"
    WS_OK=false
fi

if python3 -c "import json; json.load(open('.universe/workspaces/executor.code-workspace'))" 2>/dev/null; then
    echo "✅ executor.code-workspace is valid JSON"
else
    echo "❌ executor.code-workspace is not valid JSON"
    WS_OK=false
fi

for ws in planner executor; do
    FOLDERS=$(python3 -c "
import json
data = json.load(open('.universe/workspaces/${ws}.code-workspace'))
folders = data.get('folders', [])
print(folders[0].get('path', 'NONE') if folders else 'NONE')
" 2>/dev/null)
    if [ "$FOLDERS" = "../.." ]; then
        echo "✅ ${ws}.code-workspace points to project root (../..)"
    else
        echo "   Note: ${ws}.code-workspace path: $FOLDERS (expected ../..)"
    fi
done

PM=$(python3 -c "import json; print(json.load(open('.universe/workspaces/planner.code-workspace')).get('settings',{}).get('ai.model','NONE'))" 2>/dev/null)
EM=$(python3 -c "import json; print(json.load(open('.universe/workspaces/executor.code-workspace')).get('settings',{}).get('ai.model','NONE'))" 2>/dev/null)
echo "   Planner model:  $PM"
echo "   Executor model: $EM"

if [ "$PM" != "$EM" ] && [ "$PM" != "NONE" ] && [ -n "$PM" ]; then
    echo "✅ Different models configured for planner and executor"
else
    echo "❌ Models are the same or missing"
    WS_OK=false
fi

if $WS_OK; then
    echo "✅ PASS — valid JSON, planner=$PM, executor=$EM"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — workspace file issues"
    FAIL=$((FAIL + 1))
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "╔═══════════════════════════════════════════════╗"
printf  "║  Results: %-3d passed, %-3d failed              ║\n" $PASS $FAIL
echo "╚═══════════════════════════════════════════════╝"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "ALL $PASS TESTS PASSED"
    echo ""
    echo "The Universe binary is ready."
    echo ""
    echo "Next steps:"
    echo "  1. Follow npm-setup.md to publish"
    echo "  2. npm install -g @atlas/universe"
    echo "  3. Test with Cursor:"
    echo "     universe start -> plan in Planner -> execute in Executor -> verify in Planner"
else
    echo "WARNING: $FAIL tests failed — fix before publishing"
    exit 1
fi
