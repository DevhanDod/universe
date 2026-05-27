# Step 9 — Local End-to-End Testing

## Full System Verification Before npm Publish

**Purpose:** Verify the entire Universe system works end-to-end — from `go build` to Cursor connecting and using real engine data. This is the final gate before publishing to npm.  
**Estimated time:** 30-45 minutes  
**Dependencies:** All engines built, MCP server built, dashboard built, CLI wired.  

---

## 1. What We're Testing

This is NOT unit testing (that's `enginecheck.md`). This is the full developer experience test: "I am a developer who just built this binary. Does everything actually work?"

We test 7 things:
1. The binary compiles
2. `universe init` scans a real codebase
3. Docker PostgreSQL starts and accepts connections
4. `universe db migrate` creates all tables
5. `universe status` shows real engine data
6. `universe dashboard` serves a real web page with real data
7. `universe mcp --stdio` connects to Cursor and answers real questions

---

## 2. Test Environment Setup

```bash
# Make sure you're in the universe project root
cd /path/to/universe

# Make sure Docker is installed and running
docker --version
docker info > /dev/null 2>&1 || echo "ERROR: Docker is not running"

# Make sure Go is installed
go version

# Make sure Node.js is installed (for dashboard build)
node --version
npm --version
```

---

## 3. Test 1: Binary Compiles

```bash
echo "════════════════════════════════════════════"
echo "TEST 1: Binary compiles"
echo "════════════════════════════════════════════"

# Clean previous builds
rm -f universe

# Build the dashboard first (React → static files)
cd dashboard
npm install --silent
npm run build
cd ..

# Build the Go binary
go build -ldflags "-X main.Version=test-local" -o universe ./cmd/universe

# Verify
if [ -f universe ]; then
    echo "✅ Binary compiled: $(ls -lh universe | awk '{print $5}')"
    ./universe --version
else
    echo "❌ FAILED: Binary not found"
    exit 1
fi
```

**Expected output:**
```
✅ Binary compiled: 15M (approximate size, varies)
universe vtest-local
```

**If it fails:**
- Compilation errors → fix the Go code, run `go vet ./...` for hints
- Missing dependencies → run `go mod tidy`
- Dashboard static files missing → run `cd dashboard && npm run build`

---

## 4. Test 2: Universe Init

```bash
echo "════════════════════════════════════════════"
echo "TEST 2: Universe init scans codebase"
echo "════════════════════════════════════════════"

# Clean previous scan
rm -rf .universe

# Scan the universe project itself (scan our own code)
./universe init

# Verify the graph was created
if [ -f .universe/graph.json ]; then
    echo "✅ Graph created at .universe/graph.json"
    # Check it's valid JSON with content
    NODES=$(cat .universe/graph.json | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('nodes',[])))" 2>/dev/null || echo "0")
    echo "   Nodes found: $NODES"
    if [ "$NODES" -gt 0 ]; then
        echo "✅ Graph has real data"
    else
        echo "⚠️  Graph is empty — check parser"
    fi
else
    echo "❌ FAILED: .universe/graph.json not created"
    exit 1
fi
```

**Expected output:**
```
🔍 Scanning codebase...
   Path: /path/to/universe
   Mode: local (SQLite)

   Scanning files... found 35 source files
   Parsing with tree-sitter... 47 functions, 8 types, 6 packages
   Building knowledge graph... 58 nodes, 94 edges
   Stored locally: .universe/graph.json

✅ Graph ready!
```

**If it fails:**
- "no source files found" → check you're in a directory with Go files
- Parser errors → tree-sitter bindings may not be configured correctly
- Permission errors → check you can write to the current directory

---

## 5. Test 3: Docker PostgreSQL

```bash
echo "════════════════════════════════════════════"
echo "TEST 3: Docker PostgreSQL starts"
echo "════════════════════════════════════════════"

# Start PostgreSQL (if already running, this is a no-op)
docker compose up -d

# Wait for it to be healthy
echo "   Waiting for PostgreSQL to be ready..."
for i in $(seq 1 30); do
    if docker compose exec universe-db pg_isready -U universe_admin > /dev/null 2>&1; then
        echo "✅ PostgreSQL is ready (took ${i}s)"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ FAILED: PostgreSQL didn't start in 30 seconds"
        docker compose logs universe-db
        exit 1
    fi
    sleep 1
done

# Configure the connection
./universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe

# Verify connection
./universe db status
```

**Expected output:**
```
✅ PostgreSQL is ready (took 3s)
✅ Database URL saved
🔍 Testing database connection...
✅ Connection successful!
   pgvector: v0.7.0
```

**If it fails:**
- "connection refused" → Docker isn't running, or port 5432 is in use
- "password authentication failed" → check the docker-compose.yml credentials match
- "pgvector not installed" → the Docker image should include it (pgvector/pgvector:pg16)

---

## 6. Test 4: Database Migrations

```bash
echo "════════════════════════════════════════════"
echo "TEST 4: Database migrations"
echo "════════════════════════════════════════════"

./universe db migrate

# Verify all tables exist
./universe db status
```

**Expected output:**
```
🔧 Running database migrations...
   Running: 001_universe_schema.sql
   ✅ 001_universe_schema.sql applied

✅ Migrations complete!

🔍 Testing database connection...
✅ Connection successful!
   pgvector:             v0.7.0
   observations          ✅ 0 rows
   skills                ✅ 3 rows
   skill_executions      ✅ 0 rows
   agent_costs           ✅ 0 rows

   Seed skills: 3
```

**Key checks:**
- All 4 tables created (observations, skills, skill_executions, agent_costs)
- 3 seed skills present (type-mismatch-fix, add-unit-test, fix-import-cycle)
- pgvector extension installed

**If it fails:**
- "table already exists" → migrations already ran (this is fine, they use IF NOT EXISTS)
- "CREATE EXTENSION" error → the Docker image may not include pgvector, use `pgvector/pgvector:pg16`

---

## 7. Test 5: Universe Status (Real Data)

```bash
echo "════════════════════════════════════════════"
echo "TEST 5: Universe status with real data"
echo "════════════════════════════════════════════"

./universe status
```

**Expected output:**
```
🌌 Universe Status
═══════════════════════════════════════════════
  Engine 1 (Knowledge Graph): ✅ Active
    58 nodes, 94 edges
  Engine 2 (Persistent Memory): ✅ Active
    0 observations, 0% recall rate, 0 shared
  Engine 3 (Evolving Skills): ✅ Active
    3 active, 0 frozen, avg 0% success
  Engine 4 (Compression): ✅ Active
    compact mode, graph shorthand enabled
  Engine 5 (Orchestrator): ✅ Active
    cost tracking ready (no tasks today)
═══════════════════════════════════════════════
  Mode:     team (PostgreSQL)
  Database: postgres://universe_admin:***@localhost:5432/universe
  Platform: linux/amd64
  Version:  test-local
```

**Key checks:**
- Engine 1 shows real node/edge counts from the graph
- Engine 2 shows 0 observations (empty but connected)
- Engine 3 shows 3 active skills (the seed skills)
- Engine 4 shows active (it's always active, no DB needed)
- Engine 5 shows active with cost tracking ready
- Mode shows "team (PostgreSQL)" not "local"
- Password is masked in the URL

**If it fails:**
- Engine 1 "Unavailable" → run `universe init` first
- Engines 2-5 "Unavailable" → database not configured, run `universe config set db ...`
- Engine 2 "Error" → migration didn't run, run `universe db migrate`

---

## 8. Test 6: Dashboard

```bash
echo "════════════════════════════════════════════"
echo "TEST 6: Dashboard serves and shows data"
echo "════════════════════════════════════════════"

# Start dashboard in background
./universe dashboard --port 3001 --no-open &
DASHBOARD_PID=$!
sleep 2

# Test that the server responds
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/)
if [ "$HTTP_CODE" = "200" ]; then
    echo "✅ Dashboard is serving at http://localhost:3001"
else
    echo "❌ FAILED: Dashboard returned HTTP $HTTP_CODE"
    kill $DASHBOARD_PID 2>/dev/null
    exit 1
fi

# Test that it serves HTML (the React app)
CONTENT_TYPE=$(curl -s -I http://localhost:3001/ | grep -i content-type | head -1)
echo "   $CONTENT_TYPE"

# Test API endpoints
echo ""
echo "   Testing API endpoints:"

API_ENDPOINTS=(
    "/api/overview"
    "/api/memory?limit=5"
    "/api/skills"
    "/api/routing?limit=5"
    "/api/graph/nodes"
)

ALL_PASS=true
for endpoint in "${API_ENDPOINTS[@]}"; do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:3001${endpoint}")
    if [ "$CODE" = "200" ]; then
        echo "   ✅ GET $endpoint → 200"
    else
        echo "   ❌ GET $endpoint → $CODE"
        ALL_PASS=false
    fi
done

# Test that API returns JSON, not HTML
OVERVIEW=$(curl -s http://localhost:3001/api/overview)
if echo "$OVERVIEW" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
    echo "   ✅ /api/overview returns valid JSON"
else
    echo "   ❌ /api/overview does not return valid JSON"
    ALL_PASS=false
fi

# Test SPA routing (non-API paths should return index.html)
MEMORY_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/memory)
if [ "$MEMORY_CODE" = "200" ]; then
    echo "   ✅ SPA routing works (/memory returns 200)"
else
    echo "   ❌ SPA routing broken (/memory returned $MEMORY_CODE)"
    ALL_PASS=false
fi

# Cleanup
kill $DASHBOARD_PID 2>/dev/null
wait $DASHBOARD_PID 2>/dev/null

if $ALL_PASS; then
    echo ""
    echo "✅ Dashboard: all checks passed"
else
    echo ""
    echo "❌ Dashboard: some checks failed"
fi
```

**Expected output:**
```
✅ Dashboard is serving at http://localhost:3001
   Content-Type: text/html; charset=utf-8

   Testing API endpoints:
   ✅ GET /api/overview → 200
   ✅ GET /api/memory?limit=5 → 200
   ✅ GET /api/skills → 200
   ✅ GET /api/routing?limit=5 → 200
   ✅ GET /api/graph/nodes → 200
   ✅ /api/overview returns valid JSON
   ✅ SPA routing works (/memory returns 200)

✅ Dashboard: all checks passed
```

**Also open in browser and verify visually:**
- http://localhost:3001 — dark themed dashboard with sidebar
- Click through all 6 tabs — no JavaScript errors in console
- Skills page shows 3 seed skills
- Memory page shows empty state (no observations yet)
- Overview shows all 5 engines as active

---

## 9. Test 7: MCP Server + Cursor Connection

This is the most important test — it proves Cursor can actually talk to Universe.

### 9.1 Quick MCP handshake test (no Cursor needed)

```bash
echo "════════════════════════════════════════════"
echo "TEST 7a: MCP server handshake"
echo "════════════════════════════════════════════"

# Send an MCP initialize request via stdin and check the response
echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}' | \
  timeout 5 ./universe mcp --stdio 2>/dev/null | head -1 | \
  python3 -c "
import sys, json
try:
    resp = json.loads(sys.stdin.readline())
    if 'result' in resp:
        info = resp['result'].get('serverInfo', {})
        tools = resp['result'].get('capabilities', {}).get('tools', {})
        print(f'✅ MCP handshake successful')
        print(f'   Server: {info.get(\"name\", \"unknown\")} v{info.get(\"version\", \"unknown\")}')
        print(f'   Protocol: {resp[\"result\"].get(\"protocolVersion\", \"unknown\")}')
    else:
        print('❌ MCP handshake failed — no result in response')
        print(f'   Response: {json.dumps(resp)[:200]}')
except Exception as e:
    print(f'❌ MCP handshake error: {e}')
" 2>/dev/null || echo "❌ MCP server didn't respond (timeout or crash)"
```

**Expected output:**
```
✅ MCP handshake successful
   Server: universe vtest-local
   Protocol: 2024-11-05
```

### 9.2 Test tool listing

```bash
echo "════════════════════════════════════════════"
echo "TEST 7b: MCP tool listing"
echo "════════════════════════════════════════════"

# Send initialize + tools/list
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
            print(f'✅ {len(tools)} tools registered:')
            for t in tools:
                print(f'   - {t[\"name\"]}: {t.get(\"description\",\"\")[:60]}')
            break
    except:
        continue
" 2>/dev/null || echo "❌ Failed to get tool list"
```

**Expected output:**
```
✅ 10 tools registered:
   - get_dependencies: Get all functions that call or are called by a given fu...
   - get_impact_analysis: Analyze what will break if a given function is changed...
   - search_graph: Search the knowledge graph by function name, type name...
   - recall_memory: Search past observations from the memory store. Return...
   - get_observation_details: Get full details for specific observations by their ID...
   - store_observation: Manually store an observation — a pattern, decision, c...
   - find_skill: Search for a matching skill recipe for the current tas...
   - report_skill_execution: Report whether a skill application succeeded or failed...
   - list_skills: List all active skills, optionally filtered by languag...
   - get_skill_lineage: Get the full evolution history of a skill — all versio...
```

### 9.3 Test a real tool call

```bash
echo "════════════════════════════════════════════"
echo "TEST 7c: MCP tool call — list_skills"
echo "════════════════════════════════════════════"

(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
 sleep 0.5
 echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"list_skills","arguments":{}},"id":3}'
 sleep 2) | \
  timeout 10 ./universe mcp --stdio 2>/dev/null | \
  python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if msg.get('id') == 3 and 'result' in msg:
            content = msg['result'].get('content', [])
            for c in content:
                if c.get('type') == 'text':
                    data = json.loads(c['text'])
                    skills = data.get('skills', [])
                    print(f'✅ list_skills returned {len(skills)} skills:')
                    for s in skills:
                        print(f'   - {s[\"name\"]} v{s[\"version\"]} ({s[\"evolution\"]})')
                    break
            break
    except:
        continue
" 2>/dev/null || echo "❌ Tool call failed"
```

**Expected output:**
```
✅ list_skills returned 3 skills:
   - type-mismatch-fix v1 (manual)
   - add-unit-test v1 (manual)
   - fix-import-cycle v1 (manual)
```

### 9.4 Full Cursor test (manual)

After all automated tests pass, do this manually:

```bash
# 1. Create the Cursor config
mkdir -p ~/.cursor
cat > ~/.cursor/mcp.json << EOF
{
  "mcpServers": {
    "universe": {
      "command": "$(pwd)/universe",
      "args": ["mcp", "--stdio"],
      "env": {
        "UNIVERSE_DB_URL": "postgres://universe_admin:universe_secret_2024@localhost:5432/universe"
      }
    }
  }
}
EOF

echo "Cursor config written to ~/.cursor/mcp.json"
echo "Now:"
echo "  1. Close Cursor completely"
echo "  2. Reopen Cursor"
echo "  3. Open this project"
echo "  4. In chat, ask: 'What tools do you have access to?'"
echo "  5. Agent should list Universe tools (get_dependencies, recall_memory, etc.)"
echo "  6. Ask: 'List all available skills'"
echo "  7. Agent should call list_skills and show the 3 seed skills"
```

---

## 10. Full Test Script

Save this as `test-local.sh` and run it:

```bash
#!/bin/bash
set -e

echo "╔═══════════════════════════════════════════════╗"
echo "║  Universe — Local End-to-End Test             ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

PASS=0
FAIL=0

run_test() {
    echo ""
    echo "════════════════════════════════════════════"
    echo "TEST: $1"
    echo "════════════════════════════════════════════"
}

# ── Test 1: Compile ──
run_test "Binary compiles"
rm -f universe
cd dashboard && npm install --silent && npm run build 2>/dev/null && cd ..
go build -ldflags "-X main.Version=test-local" -o universe ./cmd/universe
./universe --version
echo "✅ PASS"
PASS=$((PASS + 1))

# ── Test 2: Init ──
run_test "Universe init"
rm -rf .universe
./universe init
test -f .universe/graph.json && echo "✅ PASS" || { echo "❌ FAIL"; FAIL=$((FAIL + 1)); }
PASS=$((PASS + 1))

# ── Test 3: Docker DB ──
run_test "Docker PostgreSQL"
docker compose up -d
sleep 3
./universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe
echo "✅ PASS"
PASS=$((PASS + 1))

# ── Test 4: Migrations ──
run_test "Database migrations"
./universe db migrate
./universe db status
echo "✅ PASS"
PASS=$((PASS + 1))

# ── Test 5: Status ──
run_test "Universe status (real data)"
./universe status
echo "✅ PASS"
PASS=$((PASS + 1))

# ── Test 6: Dashboard ──
run_test "Dashboard serves"
./universe dashboard --port 3001 --no-open &
DPID=$!
sleep 3
CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/)
API_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/overview)
kill $DPID 2>/dev/null; wait $DPID 2>/dev/null
if [ "$CODE" = "200" ] && [ "$API_CODE" = "200" ]; then
    echo "✅ PASS — dashboard serves HTML and API"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — HTML: $CODE, API: $API_CODE"
    FAIL=$((FAIL + 1))
fi

# ── Test 7: MCP handshake ──
run_test "MCP server handshake"
RESULT=$(echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}' | \
  timeout 5 ./universe mcp --stdio 2>/dev/null | head -1)
if echo "$RESULT" | python3 -c "import sys,json; d=json.loads(sys.stdin.read()); assert 'result' in d" 2>/dev/null; then
    echo "✅ PASS — MCP handshake successful"
    PASS=$((PASS + 1))
else
    echo "❌ FAIL — MCP handshake failed"
    FAIL=$((FAIL + 1))
fi

# ── Summary ──
echo ""
echo "╔═══════════════════════════════════════════════╗"
echo "║  Results: $PASS passed, $FAIL failed              ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "🎉 ALL TESTS PASSED"
    echo ""
    echo "The Universe binary is ready for npm packaging."
    echo ""
    echo "Next steps:"
    echo "  1. Follow npm-setup.md to create npm account and publish"
    echo "  2. Test: npm install -g @atlas/universe"
    echo "  3. Test Cursor connection (see Test 7.4 above)"
    echo ""
    echo "Manual Cursor test:"
    echo "  1. Create ~/.cursor/mcp.json (see test-local.sh output)"
    echo "  2. Restart Cursor"
    echo "  3. Ask: 'List all available skills'"
    echo "  4. Agent should call list_skills tool and show 3 seed skills"
else
    echo "⚠️  $FAIL tests failed — fix before publishing"
fi
```

```bash
chmod +x test-local.sh
./test-local.sh
```

---

## 11. What Passing All Tests Means

When all 7 tests pass, you have proven:

| Test | What it proves |
|------|---------------|
| Binary compiles | All engines, MCP server, dashboard, CLI compile together without errors |
| Init works | Tree-sitter parsing is functional, graph builds correctly |
| Docker DB works | PostgreSQL + pgvector is running and accessible |
| Migrations work | All tables for all 5 engines are created, seed data inserted |
| Status shows real data | Every engine can connect to the database and query it |
| Dashboard serves | React app is embedded, Go serves it, API returns JSON from real DB |
| MCP handshake works | The MCP protocol is implemented correctly, Cursor can discover tools |

**After this, the system is ready for npm packaging.** The next and final step is `npm-setup.md`.

---

## 12. If Something Fails — Quick Fixes

| Failure | Most likely cause | Fix |
|---------|-------------------|-----|
| Binary won't compile | Import cycle or missing dependency | `go vet ./...` then `go mod tidy` |
| Init finds 0 files | Wrong directory or parser not handling the language | Check `internal/parser/registry.go` maps the right extensions |
| Docker won't start | Port 5432 in use | `lsof -i :5432` to find what's using it, stop it or change docker-compose port |
| Migration fails | pgvector extension missing | Use `pgvector/pgvector:pg16` Docker image, not plain `postgres:16` |
| Status shows "Unavailable" | DB URL not configured | Run `universe config set db postgres://...` |
| Dashboard returns 404 | Static files not embedded | Rebuild: `cd dashboard && npm run build && cd .. && go build ./cmd/universe` |
| API returns HTML instead of JSON | Route not matching, falls through to SPA handler | Check `internal/dashboard/server.go` route order — API routes must be registered before the catch-all |
| MCP handshake times out | Binary crashes on startup | Run `./universe mcp --stdio` manually and check stderr for errors |
| MCP tools not found | Tools not registered in server.go | Check `internal/mcpserver/server.go` — all `mcp.AddTool` calls present |
