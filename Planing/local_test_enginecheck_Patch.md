# Local Test + Engine Check — Alignment Patch

## Apply These Changes to local-test.md and enginecheck.md

---

# PART 1: LOCAL-TEST.md — 3 Changes

---

## Change 1: Update the test count and list (Section 1)

**REPLACE:**

```
We test 7 things:
1. The binary compiles
2. `universe init` scans a real codebase
3. Docker PostgreSQL starts and accepts connections
4. `universe db migrate` creates all tables
5. `universe status` shows real engine data
6. `universe dashboard` serves a real web page with real data
7. `universe mcp --stdio` connects to Cursor and answers real questions
```

**WITH:**

```
We test 10 things:
1. The binary compiles
2. `universe init` scans a real codebase
3. Docker PostgreSQL starts and accepts connections
4. `universe db migrate` creates all tables (including plans table)
5. `universe status` shows real engine data + model config
6. `universe setup` generates workspace files + cursor rules
7. `universe dashboard` serves a real web page with real data
8. `universe mcp --stdio` handshake succeeds with 16 tools registered
9. MCP plan tools round-trip: store → get → result → verify
10. Cursor workspace files open correctly
```

---

## Change 2: Add Test 6 (universe setup), Test 9 (plan round-trip), Test 10 (workspaces)

**ADD these three new tests between the existing tests. Insert Test 6 after Test 5 (status), renumber old Test 6/7 to 7/8.**

### New Test 6: Universe Setup

**INSERT after Test 5 (universe status):**

```bash
echo "════════════════════════════════════════════"
echo "TEST 6: Universe setup generates all config files"
echo "════════════════════════════════════════════"

# Clean previous setup files
rm -rf .universe/workspaces .cursor/rules/universe-*.mdc

# Run setup non-interactively
./universe setup --premium "claude-opus-4" --execution "claude-haiku-3.5"

# Verify workspace files
SETUP_PASS=true

if [ -f ".universe/workspaces/planner.code-workspace" ]; then
    echo "✅ planner.code-workspace created"
    # Verify it contains the correct model
    if grep -q "claude-opus-4" .universe/workspaces/planner.code-workspace; then
        echo "   ✅ Contains premium model: claude-opus-4"
    else
        echo "   ❌ Missing premium model in planner workspace"
        SETUP_PASS=false
    fi
else
    echo "❌ planner.code-workspace MISSING"
    SETUP_PASS=false
fi

if [ -f ".universe/workspaces/executor.code-workspace" ]; then
    echo "✅ executor.code-workspace created"
    if grep -q "claude-haiku-3.5" .universe/workspaces/executor.code-workspace; then
        echo "   ✅ Contains execution model: claude-haiku-3.5"
    else
        echo "   ❌ Missing execution model in executor workspace"
        SETUP_PASS=false
    fi
else
    echo "❌ executor.code-workspace MISSING"
    SETUP_PASS=false
fi

# Verify cursor rules
for rule in "universe-planner.mdc" "universe-executor.mdc" "universe-compression.mdc"; do
    if [ -f ".cursor/rules/$rule" ]; then
        echo "✅ .cursor/rules/$rule created"
    else
        echo "❌ .cursor/rules/$rule MISSING"
        SETUP_PASS=false
    fi
done

# Verify planner rule mentions the premium model
if grep -q "claude-opus-4" .cursor/rules/universe-planner.mdc; then
    echo "✅ Planner rule references premium model"
else
    echo "❌ Planner rule missing model reference"
    SETUP_PASS=false
fi

# Verify executor rule mentions the execution model
if grep -q "claude-haiku-3.5" .cursor/rules/universe-executor.mdc; then
    echo "✅ Executor rule references execution model"
else
    echo "❌ Executor rule missing model reference"
    SETUP_PASS=false
fi

# Verify compression rule has alwaysApply: true
if grep -q "alwaysApply: true" .cursor/rules/universe-compression.mdc; then
    echo "✅ Compression rule has alwaysApply: true"
else
    echo "❌ Compression rule missing alwaysApply"
    SETUP_PASS=false
fi

# Verify MCP config
if [ -f ".cursor/mcp.json" ]; then
    echo "✅ .cursor/mcp.json created"
    if grep -q "universe" .cursor/mcp.json && grep -q "mcp" .cursor/mcp.json; then
        echo "   ✅ Contains universe MCP server config"
    else
        echo "   ❌ MCP config doesn't reference universe"
        SETUP_PASS=false
    fi
else
    echo "❌ .cursor/mcp.json MISSING"
    SETUP_PASS=false
fi

# Verify config saved model preferences
./universe config get models
echo ""

if $SETUP_PASS; then
    echo "✅ PASS: Universe setup generated all files"
else
    echo "❌ FAIL: Some setup files missing or incorrect"
fi
```

### New Test 9: MCP Plan Tools Round-Trip

**INSERT after Test 8 (MCP handshake):**

```bash
echo "════════════════════════════════════════════"
echo "TEST 9: MCP plan tools round-trip"
echo "════════════════════════════════════════════"

# This test sends a sequence of MCP tool calls through stdin
# and verifies each step of the plan lifecycle:
#   store_plan → get_plan → store_plan_result → get_plan_result → verify_plan

# Create a temp file with the JSON-RPC messages
cat > /tmp/universe_plan_test.jsonl << 'JSONL'
{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"store_plan","arguments":{"title":"Test plan","task_prompt":"Fix the test","steps":["Step 1: change file.go line 10","Step 2: run tests"],"risk_level":"low"}},"id":10}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_plan","arguments":{}},"id":20}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"store_plan_result","arguments":{"plan_id":"PLAN_ID_PLACEHOLDER","success":true,"summary":"Changed file.go, tests pass","files_changed":["file.go"],"tests_passed":true}},"id":30}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_plan_result","arguments":{"plan_id":"PLAN_ID_PLACEHOLDER"}},"id":40}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"verify_plan","arguments":{"plan_id":"PLAN_ID_PLACEHOLDER","approved":true,"note":"Looks good"}},"id":50}
JSONL

# Run the test — send all messages, capture all responses
RESPONSES=$(cat /tmp/universe_plan_test.jsonl | \
    timeout 15 ./universe mcp --stdio 2>/dev/null)

PLAN_PASS=true

# Check store_plan response (id=10) — should return a plan_id
STORE_RESP=$(echo "$RESPONSES" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 10 and 'result' in msg:
            content = msg['result'].get('content', [])
            for c in content:
                if c.get('type') == 'text':
                    data = json.loads(c['text'])
                    plan_id = data.get('plan_id', '')
                    if plan_id:
                        print(f'PASS:{plan_id}')
                    else:
                        print('FAIL:no plan_id')
                    break
            break
    except: continue
print('FAIL:no response')
" 2>/dev/null | head -1)

if [[ "$STORE_RESP" == PASS:* ]]; then
    PLAN_ID="${STORE_RESP#PASS:}"
    echo "✅ store_plan returned plan_id: ${PLAN_ID:0:8}..."
else
    echo "❌ store_plan failed: $STORE_RESP"
    PLAN_PASS=false
fi

# Check get_plan response (id=20) — should return steps
GET_RESP=$(echo "$RESPONSES" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 20 and 'result' in msg:
            content = msg['result'].get('content', [])
            for c in content:
                if c.get('type') == 'text':
                    data = json.loads(c['text'])
                    if data.get('found') and data.get('steps'):
                        print(f'PASS:{len(data[\"steps\"])} steps')
                    else:
                        print('FAIL:no plan found')
                    break
            break
    except: continue
print('FAIL:no response')
" 2>/dev/null | head -1)

if [[ "$GET_RESP" == PASS:* ]]; then
    echo "✅ get_plan returned: $GET_RESP"
else
    echo "❌ get_plan failed: $GET_RESP"
    PLAN_PASS=false
fi

echo ""
if $PLAN_PASS; then
    echo "✅ PASS: Plan tools round-trip works"
else
    echo "❌ FAIL: Plan tools round-trip has issues"
    echo "   Note: store_plan_result, get_plan_result, and verify_plan"
    echo "   require a real plan_id from store_plan. If store_plan failed,"
    echo "   the subsequent tests can't run. Fix store_plan first."
fi

# Clean up test plan from database
if [ -n "$PLAN_ID" ]; then
    psql "$DB_URL" -c "DELETE FROM plans WHERE id = '$PLAN_ID';" 2>/dev/null
fi
rm -f /tmp/universe_plan_test.jsonl
```

### New Test 10: Workspace Files Open

```bash
echo "════════════════════════════════════════════"
echo "TEST 10: Workspace files are valid JSON"
echo "════════════════════════════════════════════"

WS_PASS=true

# Verify planner workspace is valid JSON
if python3 -c "import json; json.load(open('.universe/workspaces/planner.code-workspace'))" 2>/dev/null; then
    echo "✅ planner.code-workspace is valid JSON"
else
    echo "❌ planner.code-workspace is not valid JSON"
    WS_PASS=false
fi

# Verify executor workspace is valid JSON
if python3 -c "import json; json.load(open('.universe/workspaces/executor.code-workspace'))" 2>/dev/null; then
    echo "✅ executor.code-workspace is valid JSON"
else
    echo "❌ executor.code-workspace is not valid JSON"
    WS_PASS=false
fi

# Verify both point to the project folder
for ws in planner executor; do
    FOLDERS=$(python3 -c "
import json
data = json.load(open('.universe/workspaces/${ws}.code-workspace'))
folders = data.get('folders', [])
if folders:
    print(folders[0].get('path', 'NONE'))
else:
    print('NONE')
" 2>/dev/null)
    if [ "$FOLDERS" = "../.." ]; then
        echo "✅ ${ws}.code-workspace points to project root"
    else
        echo "❌ ${ws}.code-workspace has wrong path: $FOLDERS"
        WS_PASS=false
    fi
done

# Verify planner has premium model, executor has cheap model
PLANNER_MODEL=$(python3 -c "
import json
data = json.load(open('.universe/workspaces/planner.code-workspace'))
print(data.get('settings', {}).get('ai.model', 'NONE'))
" 2>/dev/null)
EXECUTOR_MODEL=$(python3 -c "
import json
data = json.load(open('.universe/workspaces/executor.code-workspace'))
print(data.get('settings', {}).get('ai.model', 'NONE'))
" 2>/dev/null)

echo "   Planner model: $PLANNER_MODEL"
echo "   Executor model: $EXECUTOR_MODEL"

if [ "$PLANNER_MODEL" != "$EXECUTOR_MODEL" ] && [ "$PLANNER_MODEL" != "NONE" ]; then
    echo "✅ Different models configured for planner and executor"
else
    echo "❌ Models are the same or missing"
    WS_PASS=false
fi

echo ""
if $WS_PASS; then
    echo "✅ PASS: Workspace files are valid and correctly configured"
else
    echo "❌ FAIL: Workspace file issues"
fi
```

---

## Change 3: Update Test 7 (MCP handshake) — tool count to 16

**FIND in the existing Test 7b (MCP tool listing):**

```python
print(f'✅ {len(tools)} tools registered:')
```

**ADD a tool count check after the listing:**

```python
# After listing all tools, verify the count
expected = 16
if len(tools) == expected:
    print(f'\n✅ Tool count correct: {len(tools)} (expected {expected})')
else:
    print(f'\n❌ Tool count wrong: {len(tools)} (expected {expected})')
    print(f'   Expected tools:')
    print(f'   Engine 1: get_dependencies, get_impact_analysis, search_graph (3)')
    print(f'   Engine 2: recall_memory, get_observation_details, store_observation (3)')
    print(f'   Engine 3: find_skill, report_skill_execution, list_skills, get_skill_lineage (4)')
    print(f'   Engine 5: store_plan, get_plan, store_plan_result, get_plan_result, verify_plan, get_cost_summary (6)')
    print(f'   Total: 16')
```

**Also verify the specific plan tools are registered. ADD after tool listing:**

```python
# Verify plan tools specifically exist
plan_tools = ['store_plan', 'get_plan', 'store_plan_result', 'get_plan_result', 'verify_plan']
tool_names = [t['name'] for t in tools]
for pt in plan_tools:
    if pt in tool_names:
        print(f'   ✅ {pt} registered')
    else:
        print(f'   ❌ {pt} MISSING')
```

---

## Update the full test script (Section 10)

**REPLACE the test-local.sh script with this updated version:**

```bash
#!/bin/bash
set -e

DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

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

# ── Test 1: Compile ──
run_test 1 "Binary compiles"
rm -f universe
cd dashboard && npm install --silent && npm run build 2>/dev/null && cd ..
go build -ldflags "-X main.Version=test-local" -o universe ./cmd/universe
./universe --version
echo "✅ PASS"; PASS=$((PASS + 1))

# ── Test 2: Init ──
run_test 2 "Universe init"
rm -rf .universe
./universe init
test -f .universe/graph.json && echo "✅ PASS" && PASS=$((PASS + 1)) || { echo "❌ FAIL"; FAIL=$((FAIL + 1)); }

# ── Test 3: Docker DB ──
run_test 3 "Docker PostgreSQL"
docker compose up -d
sleep 3
./universe config set db "$DB_URL"
echo "✅ PASS"; PASS=$((PASS + 1))

# ── Test 4: Migrations ──
run_test 4 "Database migrations (including plans table)"
./universe db migrate

# Verify plans table specifically
PLANS_TABLE=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='plans';" 2>/dev/null | tr -d ' ')
if [ "$PLANS_TABLE" = "1" ]; then
    echo "✅ plans table created"
else
    echo "❌ plans table MISSING"
    FAIL=$((FAIL + 1))
fi

PLAN_COSTS_TABLE=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='plan_costs';" 2>/dev/null | tr -d ' ')
if [ "$PLAN_COSTS_TABLE" = "1" ]; then
    echo "✅ plan_costs table created"
else
    echo "❌ plan_costs table MISSING"
    FAIL=$((FAIL + 1))
fi

./universe db status
echo "✅ PASS"; PASS=$((PASS + 1))

# ── Test 5: Status ──
run_test 5 "Universe status (real data + model config)"
./universe status
echo "✅ PASS"; PASS=$((PASS + 1))

# ── Test 6: Setup ──
run_test 6 "Universe setup generates config files"
rm -rf .universe/workspaces .cursor/rules/universe-*.mdc
./universe setup --premium "claude-opus-4" --execution "claude-haiku-3.5"
SETUP_OK=true
test -f .universe/workspaces/planner.code-workspace || SETUP_OK=false
test -f .universe/workspaces/executor.code-workspace || SETUP_OK=false
test -f .cursor/rules/universe-planner.mdc || SETUP_OK=false
test -f .cursor/rules/universe-executor.mdc || SETUP_OK=false
test -f .cursor/rules/universe-compression.mdc || SETUP_OK=false
test -f .cursor/mcp.json || SETUP_OK=false
if $SETUP_OK; then
    echo "✅ PASS — all 6 config files generated"; PASS=$((PASS + 1))
else
    echo "❌ FAIL — some config files missing"; FAIL=$((FAIL + 1))
fi

# ── Test 7: Dashboard ──
run_test 7 "Dashboard serves (with plans API)"
./universe dashboard --port 3001 --no-open &
DPID=$!
sleep 3
HTML_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/)
API_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/overview)
PLANS_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/plans)
kill $DPID 2>/dev/null; wait $DPID 2>/dev/null
if [ "$HTML_CODE" = "200" ] && [ "$API_CODE" = "200" ] && [ "$PLANS_CODE" = "200" ]; then
    echo "✅ PASS — HTML, overview API, and plans API all serve"; PASS=$((PASS + 1))
else
    echo "❌ FAIL — HTML:$HTML_CODE API:$API_CODE Plans:$PLANS_CODE"; FAIL=$((FAIL + 1))
fi

# ── Test 8: MCP handshake + tool count ──
run_test 8 "MCP handshake + 16 tools"
TOOL_COUNT=$(
(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"tools/list","id":2}'
 sleep 1) | \
  timeout 10 ./universe mcp --stdio 2>/dev/null | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 2 and 'result' in msg:
            tools = msg['result'].get('tools', [])
            print(len(tools))
            break
    except: continue
" 2>/dev/null
)
if [ "$TOOL_COUNT" = "16" ]; then
    echo "✅ PASS — $TOOL_COUNT tools registered (expected 16)"; PASS=$((PASS + 1))
else
    echo "❌ FAIL — $TOOL_COUNT tools registered (expected 16)"; FAIL=$((FAIL + 1))
fi

# ── Test 9: Plan tools round-trip ──
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
  timeout 15 ./universe mcp --stdio 2>/dev/null | \
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
    echo "✅ PASS — store_plan + get_plan round-trip works"; PASS=$((PASS + 1))
else
    echo "❌ FAIL — plan round-trip: $PLAN_RESULT"; FAIL=$((FAIL + 1))
fi

# Clean up test plans
psql "$DB_URL" -c "DELETE FROM plans WHERE title = 'E2E test plan';" 2>/dev/null

# ── Test 10: Workspace files valid ──
run_test 10 "Workspace files are valid and correctly configured"
WS_OK=true
python3 -c "import json; json.load(open('.universe/workspaces/planner.code-workspace'))" 2>/dev/null || WS_OK=false
python3 -c "import json; json.load(open('.universe/workspaces/executor.code-workspace'))" 2>/dev/null || WS_OK=false
# Verify different models
PM=$(python3 -c "import json; print(json.load(open('.universe/workspaces/planner.code-workspace')).get('settings',{}).get('ai.model',''))" 2>/dev/null)
EM=$(python3 -c "import json; print(json.load(open('.universe/workspaces/executor.code-workspace')).get('settings',{}).get('ai.model',''))" 2>/dev/null)
[ "$PM" != "$EM" ] && [ -n "$PM" ] || WS_OK=false

if $WS_OK; then
    echo "✅ PASS — valid JSON, planner=$PM, executor=$EM"; PASS=$((PASS + 1))
else
    echo "❌ FAIL — workspace files invalid"; FAIL=$((FAIL + 1))
fi

# ── Summary ──
echo ""
echo "╔═══════════════════════════════════════════════╗"
printf "║  Results: %d passed, %d failed                   ║\n" $PASS $FAIL
echo "╚═══════════════════════════════════════════════╝"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "🎉 ALL 10 TESTS PASSED"
    echo ""
    echo "The Universe binary is ready for npm packaging."
    echo ""
    echo "Next steps:"
    echo "  1. Follow npm-setup.md to publish"
    echo "  2. npm install -g @atlas/universe"
    echo "  3. Test with Cursor:"
    echo "     universe start → plan in 🧠 → execute in ⚡ → verify in 🧠"
else
    echo "⚠️  $FAIL tests failed — fix before publishing"
fi
```

---

## Update the "What Passing All Tests Means" table (Section 11)

**REPLACE with:**

```
| Test | What it proves |
|------|---------------|
| 1. Binary compiles | All engines, MCP server, dashboard, CLI compile together |
| 2. Init works | Tree-sitter parsing + graph building functional |
| 3. Docker DB works | PostgreSQL + pgvector running |
| 4. Migrations work | All tables created including plans + plan_costs |
| 5. Status shows real data | Every engine queries DB + shows model config |
| 6. Setup generates files | Workspace files + cursor rules + MCP config all created correctly |
| 7. Dashboard serves | React app embedded, API returns JSON including plans endpoint |
| 8. MCP 16 tools | All 16 tools registered (3 graph + 3 memory + 4 skills + 6 orchestrator) |
| 9. Plan round-trip | store_plan → get_plan works end-to-end through MCP |
| 10. Workspaces valid | JSON valid, different models for planner vs executor |
```

---

## Update "If Something Fails" table (Section 12)

**ADD these rows:**

```
| Setup generates no files | orchestrator.RunSetup not called or path wrong | Check cmd/universe/setup.go calls RunSetup with correct projectDir |
| Plans table missing | Migration file doesn't include plans table | Check migrations/004_plans_table.sql exists and has CREATE TABLE plans |
| Tool count is not 16 | Tools not registered in server.go | Check internal/mcpserver/server.go — all mcp.AddTool calls present |
| store_plan fails | PlanStore not initialized or DB not connected | Check mcp.go initializes PlanStore when DB URL is available |
| get_plan returns empty | No pending plan or developer_id mismatch | Check getDeveloperID returns the correct value |
| Workspace models are same | Setup used same model for both | Check universe setup --premium and --execution are different |
```

---

---

# PART 2: ENGINECHECK.md — Add Plans and MCP Checks

---

## Add after the Engine 5 section (before "Cross-Engine Integration Checks")

**INSERT this new section:**

```bash
# ============================================================
# ENGINE 5 (UPDATED): Plans + Workspaces
# ============================================================

# Check 5.NEW.1: Plans table exists
echo "── Engine 5 (Updated): Plans + Workspaces ──"

DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

psql "$DB_URL" -c "SELECT COUNT(*) FROM plans;" 2>/dev/null && echo "✅ plans table exists" || echo "❌ plans table MISSING"
psql "$DB_URL" -c "SELECT COUNT(*) FROM plan_costs;" 2>/dev/null && echo "✅ plan_costs table exists" || echo "❌ plan_costs table MISSING"

# Check 5.NEW.2: Plans CRUD works
psql "$DB_URL" -c "
  INSERT INTO plans (developer_id, title, task_prompt, steps, status)
  VALUES ('test-dev', 'Engine check test', 'Test', '[\"step1\"]'::jsonb, 'pending')
  RETURNING id, title;" && echo "✅ Plan insert works" || echo "❌ Plan insert failed"

psql "$DB_URL" -c "
  SELECT id, title, status FROM plans WHERE developer_id = 'test-dev';" && echo "✅ Plan select works" || echo "❌ Plan select failed"

psql "$DB_URL" -c "
  UPDATE plans SET status = 'completed', result_success = true, result_summary = 'test'
  WHERE developer_id = 'test-dev';" && echo "✅ Plan update works" || echo "❌ Plan update failed"

psql "$DB_URL" -c "DELETE FROM plans WHERE developer_id = 'test-dev';"
echo "  Cleaned up test data"

# Check 5.NEW.3: Plan store functions exist
go doc ./internal/orchestrator/ NewPlanStore 2>/dev/null && echo "✅ NewPlanStore" || echo "❌ NewPlanStore missing"
go doc ./internal/orchestrator/ StorePlan 2>/dev/null && echo "✅ StorePlan" || echo "❌ StorePlan missing"
go doc ./internal/orchestrator/ GetLatestPlan 2>/dev/null && echo "✅ GetLatestPlan" || echo "❌ GetLatestPlan missing"
go doc ./internal/orchestrator/ StorePlanResult 2>/dev/null && echo "✅ StorePlanResult" || echo "❌ StorePlanResult missing"
go doc ./internal/orchestrator/ VerifyPlan 2>/dev/null && echo "✅ VerifyPlan" || echo "❌ VerifyPlan missing"

# Check 5.NEW.4: Workspace generator exists
go doc ./internal/orchestrator/ GenerateWorkspaces 2>/dev/null && echo "✅ GenerateWorkspaces" || echo "❌ GenerateWorkspaces missing"
go doc ./internal/orchestrator/ OpenPlannerWorkspace 2>/dev/null && echo "✅ OpenPlannerWorkspace" || echo "❌ OpenPlannerWorkspace missing"
go doc ./internal/orchestrator/ OpenExecutorWorkspace 2>/dev/null && echo "✅ OpenExecutorWorkspace" || echo "❌ OpenExecutorWorkspace missing"

# Check 5.NEW.5: Setup generator exists
go doc ./internal/orchestrator/ RunSetup 2>/dev/null && echo "✅ RunSetup" || echo "❌ RunSetup missing"

# Check 5.NEW.6: Router is recommendation-based (not LLM-calling)
go doc ./internal/orchestrator/ Recommend 2>/dev/null && echo "✅ Recommend method exists" || echo "❌ Recommend missing"
# Verify the old LLM-calling functions are GONE
go doc ./internal/orchestrator/ LLMClient 2>/dev/null && echo "❌ LLMClient still exists (should be removed)" || echo "✅ LLMClient removed"
go doc ./internal/orchestrator/ ExecuteSubTask 2>/dev/null && echo "❌ ExecuteSubTask still exists (should be removed)" || echo "✅ ExecuteSubTask removed"
```

---

## Update the Cross-Engine checks

**REPLACE the "All tables present" check:**

```bash
# BEFORE:
check "All tables present" "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0; SELECT 1 FROM skills LIMIT 0; SELECT 1 FROM skill_executions LIMIT 0; SELECT 1 FROM agent_costs LIMIT 0;'"

# AFTER:
check "All tables present" "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0; SELECT 1 FROM skills LIMIT 0; SELECT 1 FROM skill_executions LIMIT 0; SELECT 1 FROM plans LIMIT 0; SELECT 1 FROM plan_costs LIMIT 0;'"
```

---

## Update the Engine 5 file checks in the Final Summary Script

**REPLACE the Engine 5 section in the `universe-engine-check.sh` script:**

```bash
# BEFORE:
echo ""
echo "── Engine 5: Orchestrator ──"
check "orchestrator/types.go exists" "test -f internal/orchestrator/types.go"
check "orchestrator/router.go exists" "test -f internal/orchestrator/router.go"
check "orchestrator/planner.go exists" "test -f internal/orchestrator/planner.go"
check "orchestrator/executor.go exists" "test -f internal/orchestrator/executor.go"
check "orchestrator/verifier.go exists" "test -f internal/orchestrator/verifier.go"
check "orchestrator/escalation.go exists" "test -f internal/orchestrator/escalation.go"
check "orchestrator/parallel.go exists" "test -f internal/orchestrator/parallel.go"
check "orchestrator/llmclient.go exists" "test -f internal/orchestrator/llmclient.go"
check "orchestrator/tracker.go exists" "test -f internal/orchestrator/tracker.go"
check "orchestrator/templates.go exists" "test -f internal/orchestrator/templates.go"
check "orchestrator package compiles" "go build ./internal/orchestrator/"
check "agent_costs table exists" "psql $DB_URL -c 'SELECT 1 FROM agent_costs LIMIT 0;'"
check "orchestrator tests pass" "DATABASE_URL=$DB_URL go test ./internal/orchestrator/ -count=1"

# AFTER:
echo ""
echo "── Engine 5: Plan Bridge + Workspaces ──"
check "orchestrator/types.go exists" "test -f internal/orchestrator/types.go"
check "orchestrator/router.go exists" "test -f internal/orchestrator/router.go"
check "orchestrator/templates.go exists" "test -f internal/orchestrator/templates.go"
check "orchestrator/tracker.go exists" "test -f internal/orchestrator/tracker.go"
check "orchestrator/plans.go exists" "test -f internal/orchestrator/plans.go"
check "orchestrator/workspace.go exists" "test -f internal/orchestrator/workspace.go"
check "orchestrator/setup.go exists" "test -f internal/orchestrator/setup.go"
check "orchestrator package compiles" "go build ./internal/orchestrator/"
check "plans table exists" "psql $DB_URL -c 'SELECT 1 FROM plans LIMIT 0;'"
check "plan_costs table exists" "psql $DB_URL -c 'SELECT 1 FROM plan_costs LIMIT 0;'"
check "OLD files removed: no llmclient.go" "test ! -f internal/orchestrator/llmclient.go"
check "OLD files removed: no planner.go" "test ! -f internal/orchestrator/planner.go"
check "OLD files removed: no executor.go" "test ! -f internal/orchestrator/executor.go"
check "OLD files removed: no verifier.go" "test ! -f internal/orchestrator/verifier.go"
check "OLD files removed: no escalation.go" "test ! -f internal/orchestrator/escalation.go"
check "OLD files removed: no parallel.go" "test ! -f internal/orchestrator/parallel.go"
check "orchestrator tests pass" "DATABASE_URL=$DB_URL go test ./internal/orchestrator/ -count=1"
```

---

## Add MCP tool check section to the Final Summary Script

**ADD after the cross-engine checks, before the results:**

```bash
echo ""
echo "── MCP Server ──"
check "mcpserver/server.go exists" "test -f internal/mcpserver/server.go"
check "mcpserver/tools_graph.go exists" "test -f internal/mcpserver/tools_graph.go"
check "mcpserver/tools_memory.go exists" "test -f internal/mcpserver/tools_memory.go"
check "mcpserver/tools_skills.go exists" "test -f internal/mcpserver/tools_skills.go"
check "mcpserver/tools_plans.go exists" "test -f internal/mcpserver/tools_plans.go"
check "mcpserver package compiles" "go build ./internal/mcpserver/"
check "mcpserver tests pass" "DATABASE_URL=$DB_URL go test ./internal/mcpserver/ -count=1"

# Quick MCP handshake + tool count check
TOOL_COUNT=$(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
{"jsonrpc":"2.0","method":"tools/list","id":2}' | \
  timeout 10 go run ./cmd/universe mcp --stdio 2>/dev/null | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 2 and 'result' in msg:
            print(len(msg['result'].get('tools', [])))
            break
    except: continue
" 2>/dev/null)

check "MCP registers 16 tools" "test '$TOOL_COUNT' = '16'"
```

---

## Update the "Next steps" message at the end of enginecheck.sh

**REPLACE:**

```bash
echo "Next steps:"
echo "  1. Build the MCP server (universe mcp --stdio)"
echo "  2. Wire the CLI commands (cobra)"
echo "  3. Build the dashboard (dashboard.md)"
echo "  4. Set up npm distribution (npm-setup.md)"
```

**WITH:**

```bash
echo "Next steps:"
echo "  1. Run local-test.sh for full end-to-end verification"
echo "  2. Set up npm distribution (npm-setup.md)"
echo "  3. make release V=0.1.0"
echo "  4. npm install -g @atlas/universe && universe setup"
```
