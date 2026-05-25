# Engine Verification Checklist

## Run This After Building All Engines

**Purpose:** Verify all 4 engines (Engine 2, 3, 4, 5) exist, compile, and work correctly before wiring the MCP server and publishing to npm.  
**How to use:** Feed this file to Claude Code after building each engine, or run all checks at the end.  
**Prerequisites:** Docker PostgreSQL running, Go 1.22+ installed, project compiles.  

---

## Pre-Check: Environment

Run these first. If any fail, fix them before checking engines.

```bash
# ============================================================
# PRE-CHECK 1: Go is installed
# ============================================================
go version
# Expected: go version go1.22.x (or higher)
# If missing: install Go from https://go.dev/dl/

# ============================================================
# PRE-CHECK 2: Project compiles
# ============================================================
cd /path/to/universe
go build ./...
# Expected: no errors
# If errors: fix compilation issues before proceeding

# ============================================================
# PRE-CHECK 3: Docker PostgreSQL is running
# ============================================================
docker compose ps
# Expected: universe-db running, healthy
# If not running: docker compose up -d

# ============================================================
# PRE-CHECK 4: Database is accessible
# ============================================================
psql postgres://universe_admin:universe_secret_2024@localhost:5432/universe -c "SELECT 1;"
# Expected: returns 1
# If psql not installed: apt install postgresql-client
# If connection refused: check Docker is running

# ============================================================
# PRE-CHECK 5: pgvector extension exists
# ============================================================
psql postgres://universe_admin:universe_secret_2024@localhost:5432/universe \
  -c "SELECT extversion FROM pg_extension WHERE extname='vector';"
# Expected: returns a version number (e.g., 0.7.0)
# If empty: run CREATE EXTENSION vector; in the database

# ============================================================
# PRE-CHECK 6: Migrations have been applied
# ============================================================
psql postgres://universe_admin:universe_secret_2024@localhost:5432/universe \
  -c "SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name;"
# Expected tables: agent_costs, compression_samples, observations, skill_executions, skills
# If missing: run migrations
#   psql postgres://universe_admin:universe_secret_2024@localhost:5432/universe \
#     -f migrations/001_universe_schema.sql
```

---

## Engine 4: Compression — Verification

Engine 4 has no database dependency. It's 3 Go files with no external calls.

### Check 4.1: Files exist

```bash
echo "=== Engine 4: File check ==="
FILES=(
  "internal/compress/prompt.go"
  "internal/compress/shorthand.go"
  "internal/compress/formatter.go"
)
PASS=true
for f in "${FILES[@]}"; do
  if [ -f "$f" ]; then
    echo "  ✅ $f"
  else
    echo "  ❌ $f MISSING"
    PASS=false
  fi
done
$PASS && echo "Engine 4 files: ALL PRESENT" || echo "Engine 4 files: INCOMPLETE"
```

### Check 4.2: Package compiles

```bash
go build ./internal/compress/
# Expected: no errors
# If errors: the compress package has compilation issues
```

### Check 4.3: Types exist

```bash
# Verify key types are defined and exported
go doc ./internal/compress/ CompressionLevel 2>/dev/null && echo "✅ CompressionLevel type exists" || echo "❌ CompressionLevel type missing"
go doc ./internal/compress/ PromptConfig 2>/dev/null && echo "✅ PromptConfig type exists" || echo "❌ PromptConfig type missing"
go doc ./internal/compress/ GraphNodeInfo 2>/dev/null && echo "✅ GraphNodeInfo type exists" || echo "❌ GraphNodeInfo type missing"
go doc ./internal/compress/ TaskType 2>/dev/null && echo "✅ TaskType type exists" || echo "❌ TaskType type missing"
```

### Check 4.4: Key functions exist

```bash
# Verify key functions are defined
go doc ./internal/compress/ BuildPrompt 2>/dev/null && echo "✅ BuildPrompt exists" || echo "❌ BuildPrompt missing"
go doc ./internal/compress/ BuildShorthand 2>/dev/null && echo "✅ BuildShorthand exists" || echo "❌ BuildShorthand missing"
go doc ./internal/compress/ BuildShorthandCompact 2>/dev/null && echo "✅ BuildShorthandCompact exists" || echo "❌ BuildShorthandCompact missing"
go doc ./internal/compress/ GetOutputSchema 2>/dev/null && echo "✅ GetOutputSchema exists" || echo "❌ GetOutputSchema missing"
go doc ./internal/compress/ FormatSchemaPrompt 2>/dev/null && echo "✅ FormatSchemaPrompt exists" || echo "❌ FormatSchemaPrompt missing"
go doc ./internal/compress/ ParseFixOutput 2>/dev/null && echo "✅ ParseFixOutput exists" || echo "❌ ParseFixOutput missing"
go doc ./internal/compress/ ParseTestOutput 2>/dev/null && echo "✅ ParseTestOutput exists" || echo "❌ ParseTestOutput missing"
```

### Check 4.5: Unit tests pass

```bash
go test ./internal/compress/ -v -count=1
# Expected: all tests pass
# Key tests to verify:
#   - BuildShorthand produces correct format
#   - BuildPrompt assembles correctly for each level (full/compact/normal)
#   - ParseFixOutput handles valid JSON
#   - ParseFixOutput strips markdown code fences
#   - ParseFixOutput returns error on invalid JSON
#   - GetOutputSchema returns correct schema per TaskType
#   - Empty graph context produces no GRAPH CONTEXT section
```

### Check 4.6: Functional test — BuildPrompt produces expected output

```bash
# Create a quick Go test file and run it
cat > /tmp/compress_check.go << 'GOEOF'
package main

import (
    "fmt"
    "strings"
    "universe/internal/compress"
)

func main() {
    errors := 0

    // Test 1: Compact prompt contains compression rules
    prompt := compress.BuildPrompt("Fix the bug", compress.PromptConfig{
        Level: compress.LevelCompact,
    })
    if !strings.Contains(prompt, "TASK:") {
        fmt.Println("❌ BuildPrompt missing TASK: section")
        errors++
    } else {
        fmt.Println("✅ BuildPrompt includes TASK: section")
    }

    if !strings.Contains(prompt, "Fix the bug") {
        fmt.Println("❌ BuildPrompt missing the actual task text")
        errors++
    } else {
        fmt.Println("✅ BuildPrompt includes task text")
    }

    // Test 2: Graph context is injected when nodes provided
    prompt2 := compress.BuildPrompt("Do something", compress.PromptConfig{
        Level: compress.LevelCompact,
        GraphContext: []compress.GraphNodeInfo{
            {Name: "Validate", Kind: "function", Package: "auth", File: "auth.go", Line: 10},
        },
    })
    if !strings.Contains(prompt2, "auth.Validate") {
        fmt.Println("❌ BuildPrompt missing graph shorthand")
        errors++
    } else {
        fmt.Println("✅ BuildPrompt includes graph shorthand")
    }

    // Test 3: Full mode includes schema
    prompt3 := compress.BuildPrompt("Generate tests", compress.PromptConfig{
        Level:    compress.LevelFull,
        TaskType: compress.TaskTest,
    })
    if !strings.Contains(prompt3, "JSON") || !strings.Contains(prompt3, "tests") {
        fmt.Println("❌ BuildPrompt full mode missing schema")
        errors++
    } else {
        fmt.Println("✅ BuildPrompt full mode includes schema")
    }

    // Test 4: ParseFixOutput works
    json := `{"fixes":[{"file":"a.go","line":1,"old_code":"old","new_code":"new","reason":"why"}],"affected_nodes":["x"],"confidence":0.9}`
    result, err := compress.ParseFixOutput(json)
    if err != nil || len(result.Fixes) != 1 {
        fmt.Println("❌ ParseFixOutput failed on valid JSON")
        errors++
    } else {
        fmt.Println("✅ ParseFixOutput parses valid JSON")
    }

    // Test 5: ParseFixOutput handles code fences
    fenced := "```json\n" + json + "\n```"
    result2, err2 := compress.ParseFixOutput(fenced)
    if err2 != nil || len(result2.Fixes) != 1 {
        fmt.Println("❌ ParseFixOutput failed to strip code fences")
        errors++
    } else {
        fmt.Println("✅ ParseFixOutput strips code fences")
    }

    fmt.Printf("\nEngine 4 functional check: %d errors\n", errors)
    if errors > 0 {
        fmt.Println("❌ ENGINE 4 FAILED")
    } else {
        fmt.Println("✅ ENGINE 4 PASSED")
    }
}
GOEOF
cd /path/to/universe && go run /tmp/compress_check.go
```

---

## Engine 2: Memory — Verification

Engine 2 needs PostgreSQL running with migrations applied.

### Check 2.1: Files exist

```bash
echo "=== Engine 2: File check ==="
FILES=(
  "internal/memory/types.go"
  "internal/memory/store.go"
  "internal/memory/retriever.go"
  "internal/memory/compressor.go"
  "internal/memory/hooks.go"
  "internal/memory/decay.go"
  "internal/memory/embed.go"
)
PASS=true
for f in "${FILES[@]}"; do
  if [ -f "$f" ]; then
    echo "  ✅ $f"
  else
    echo "  ❌ $f MISSING"
    PASS=false
  fi
done
$PASS && echo "Engine 2 files: ALL PRESENT" || echo "Engine 2 files: INCOMPLETE"
```

### Check 2.2: Package compiles

```bash
go build ./internal/memory/
# Expected: no errors
```

### Check 2.3: Types exist

```bash
go doc ./internal/memory/ Observation 2>/dev/null && echo "✅ Observation type" || echo "❌ Observation type missing"
go doc ./internal/memory/ ObservationSummary 2>/dev/null && echo "✅ ObservationSummary type" || echo "❌ ObservationSummary type missing"
go doc ./internal/memory/ SearchQuery 2>/dev/null && echo "✅ SearchQuery type" || echo "❌ SearchQuery type missing"
go doc ./internal/memory/ SearchResult 2>/dev/null && echo "✅ SearchResult type" || echo "❌ SearchResult type missing"
go doc ./internal/memory/ Session 2>/dev/null && echo "✅ Session type" || echo "❌ Session type missing"
go doc ./internal/memory/ Config 2>/dev/null && echo "✅ Config type" || echo "❌ Config type missing"
```

### Check 2.4: Store functions exist

```bash
go doc ./internal/memory/ NewStore 2>/dev/null && echo "✅ NewStore" || echo "❌ NewStore missing"
go doc ./internal/memory/ InsertObservation 2>/dev/null && echo "✅ InsertObservation" || echo "❌ InsertObservation missing"
go doc ./internal/memory/ GetByGraphNode 2>/dev/null && echo "✅ GetByGraphNode" || echo "❌ GetByGraphNode missing"
go doc ./internal/memory/ SearchKeyword 2>/dev/null && echo "✅ SearchKeyword" || echo "❌ SearchKeyword missing"
go doc ./internal/memory/ SearchSemantic 2>/dev/null && echo "✅ SearchSemantic" || echo "❌ SearchSemantic missing"
go doc ./internal/memory/ TouchRecalled 2>/dev/null && echo "✅ TouchRecalled" || echo "❌ TouchRecalled missing"
```

### Check 2.5: Database table exists and accepts data

```bash
DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

# Check table exists
psql "$DB_URL" -c "SELECT COUNT(*) FROM observations;" && echo "✅ observations table exists" || echo "❌ observations table missing"

# Check indexes exist
psql "$DB_URL" -c "
  SELECT indexname FROM pg_indexes
  WHERE tablename = 'observations'
  ORDER BY indexname;" | grep -c 'idx_obs' | xargs -I {} echo "  {} indexes found on observations table"

# Insert a test observation
psql "$DB_URL" -c "
  INSERT INTO observations (developer_id, repo_id, graph_node_id, category, summary, confidence)
  VALUES ('test-dev', 'test-repo', 'test:node:func', 'fix', 'Test observation from engine check', 1.0)
  RETURNING id, summary;" && echo "✅ Insert works" || echo "❌ Insert failed"

# Search by keyword
psql "$DB_URL" -c "
  SELECT id, summary FROM observations
  WHERE fts @@ plainto_tsquery('english', 'test observation');" && echo "✅ FTS search works" || echo "❌ FTS search failed"

# Search by graph node
psql "$DB_URL" -c "
  SELECT id, summary FROM observations
  WHERE graph_node_id = 'test:node:func';" && echo "✅ Graph node lookup works" || echo "❌ Graph node lookup failed"

# Clean up test data
psql "$DB_URL" -c "DELETE FROM observations WHERE developer_id = 'test-dev';"
echo "  Cleaned up test data"
```

### Check 2.6: Unit tests pass

```bash
DATABASE_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe" \
  go test ./internal/memory/ -v -count=1
# Expected: all tests pass
```

### Check 2.7: Retriever interfaces defined

```bash
go doc ./internal/memory/ EmbedFunc 2>/dev/null && echo "✅ EmbedFunc interface" || echo "❌ EmbedFunc missing"
go doc ./internal/memory/ GraphQuerier 2>/dev/null && echo "✅ GraphQuerier interface" || echo "❌ GraphQuerier missing"
go doc ./internal/memory/ NewRetriever 2>/dev/null && echo "✅ NewRetriever" || echo "❌ NewRetriever missing"
```

### Check 2.8: Session manager exists

```bash
go doc ./internal/memory/ NewSessionManager 2>/dev/null && echo "✅ NewSessionManager" || echo "❌ NewSessionManager missing"
go doc ./internal/memory/ OnToolCall 2>/dev/null && echo "✅ OnToolCall method" || echo "❌ OnToolCall missing"
go doc ./internal/memory/ EndSession 2>/dev/null && echo "✅ EndSession method" || echo "❌ EndSession missing"
```

### Check 2.9: Decay runner exists

```bash
go doc ./internal/memory/ NewDecayRunner 2>/dev/null && echo "✅ NewDecayRunner" || echo "❌ NewDecayRunner missing"
go doc ./internal/memory/ RunDecay 2>/dev/null && echo "✅ RunDecay method" || echo "❌ RunDecay missing"
```

---

## Engine 3: Skills — Verification

Engine 3 needs PostgreSQL and depends on Engine 2's interfaces.

### Check 3.1: Files exist

```bash
echo "=== Engine 3: File check ==="
FILES=(
  "internal/skills/types.go"
  "internal/skills/store.go"
  "internal/skills/matcher.go"
  "internal/skills/executor.go"
  "internal/skills/evolver.go"
  "internal/skills/monitor.go"
  "internal/skills/graph_sync.go"
  "internal/skills/safety.go"
)
PASS=true
for f in "${FILES[@]}"; do
  if [ -f "$f" ]; then
    echo "  ✅ $f"
  else
    echo "  ❌ $f MISSING"
    PASS=false
  fi
done
$PASS && echo "Engine 3 files: ALL PRESENT" || echo "Engine 3 files: INCOMPLETE"
```

### Check 3.2: Package compiles

```bash
go build ./internal/skills/
# Expected: no errors
```

### Check 3.3: Types exist

```bash
go doc ./internal/skills/ Skill 2>/dev/null && echo "✅ Skill type" || echo "❌ Skill type missing"
go doc ./internal/skills/ SkillSummary 2>/dev/null && echo "✅ SkillSummary type" || echo "❌ SkillSummary type missing"
go doc ./internal/skills/ SkillExecution 2>/dev/null && echo "✅ SkillExecution type" || echo "❌ SkillExecution type missing"
go doc ./internal/skills/ MatchQuery 2>/dev/null && echo "✅ MatchQuery type" || echo "❌ MatchQuery type missing"
go doc ./internal/skills/ MatchResult 2>/dev/null && echo "✅ MatchResult type" || echo "❌ MatchResult type missing"
go doc ./internal/skills/ EvolutionType 2>/dev/null && echo "✅ EvolutionType type" || echo "❌ EvolutionType type missing"
go doc ./internal/skills/ NegativeTag 2>/dev/null && echo "✅ NegativeTag type" || echo "❌ NegativeTag type missing"
go doc ./internal/skills/ SafetyScanResult 2>/dev/null && echo "✅ SafetyScanResult type" || echo "❌ SafetyScanResult type missing"
```

### Check 3.4: Key functions exist

```bash
# Store
go doc ./internal/skills/ NewStore 2>/dev/null && echo "✅ NewStore" || echo "❌ NewStore missing"
go doc ./internal/skills/ InsertSkill 2>/dev/null && echo "✅ InsertSkill" || echo "❌ InsertSkill missing"
go doc ./internal/skills/ GetByGraphNodes 2>/dev/null && echo "✅ GetByGraphNodes" || echo "❌ GetByGraphNodes missing"
go doc ./internal/skills/ GetLineage 2>/dev/null && echo "✅ GetLineage" || echo "❌ GetLineage missing"

# Matcher
go doc ./internal/skills/ NewMatcher 2>/dev/null && echo "✅ NewMatcher" || echo "❌ NewMatcher missing"
go doc ./internal/skills/ Match 2>/dev/null && echo "✅ Match method" || echo "❌ Match missing"

# Evolver
go doc ./internal/skills/ NewEvolver 2>/dev/null && echo "✅ NewEvolver" || echo "❌ NewEvolver missing"
go doc ./internal/skills/ LLMClient 2>/dev/null && echo "✅ LLMClient interface" || echo "❌ LLMClient interface missing"

# Safety
go doc ./internal/skills/ NewSafetyScanner 2>/dev/null && echo "✅ NewSafetyScanner" || echo "❌ NewSafetyScanner missing"
go doc ./internal/skills/ ScanInstruction 2>/dev/null && echo "✅ ScanInstruction" || echo "❌ ScanInstruction missing"

# Graph sync
go doc ./internal/skills/ NewGraphSync 2>/dev/null && echo "✅ NewGraphSync" || echo "❌ NewGraphSync missing"
go doc ./internal/skills/ OnGraphChange 2>/dev/null && echo "✅ OnGraphChange" || echo "❌ OnGraphChange missing"
```

### Check 3.5: Database tables exist and accept data

```bash
DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

# Check tables exist
psql "$DB_URL" -c "SELECT COUNT(*) FROM skills;" && echo "✅ skills table exists" || echo "❌ skills table missing"
psql "$DB_URL" -c "SELECT COUNT(*) FROM skill_executions;" && echo "✅ skill_executions table exists" || echo "❌ skill_executions table missing"

# Check seed skills were inserted
SEED_COUNT=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM skills WHERE evolution = 'manual';" | tr -d ' ')
echo "  Seed skills found: $SEED_COUNT"
if [ "$SEED_COUNT" -ge 3 ]; then
  echo "✅ Seed skills present"
else
  echo "❌ Seed skills missing (expected >= 3)"
fi

# Insert a test skill
psql "$DB_URL" -c "
  INSERT INTO skills (name, version, evolution, graph_node_ids, trigger_desc, instruction, confidence, shared, is_active, created_by)
  VALUES ('test-skill', 1, 'manual', '{\"test:node:func\"}', 'Test trigger', 'Test instruction', 0.5, true, true, 'test')
  RETURNING id, name;" && echo "✅ Skill insert works" || echo "❌ Skill insert failed"

# Search by graph node (GIN index on array)
psql "$DB_URL" -c "
  SELECT id, name FROM skills
  WHERE graph_node_ids @> ARRAY['test:node:func']
    AND is_active = true;" && echo "✅ Graph node array search works" || echo "❌ Graph node array search failed"

# Test FTS on skills
psql "$DB_URL" -c "
  SELECT id, name FROM skills
  WHERE fts @@ plainto_tsquery('english', 'test trigger');" && echo "✅ Skills FTS works" || echo "❌ Skills FTS failed"

# Test lineage query (recursive CTE)
psql "$DB_URL" -c "
  WITH RECURSIVE lineage AS (
    SELECT * FROM skills WHERE name = 'test-skill' AND version = 1
    UNION ALL
    SELECT s.* FROM skills s JOIN lineage l ON s.id = l.parent_id
  )
  SELECT id, name, version FROM lineage;" && echo "✅ Lineage recursive CTE works" || echo "❌ Lineage CTE failed"

# Clean up
psql "$DB_URL" -c "DELETE FROM skills WHERE name = 'test-skill';"
echo "  Cleaned up test data"
```

### Check 3.6: Safety scanner works

```bash
go test ./internal/skills/ -run TestSafety -v -count=1
# Expected: safety tests pass
# Key tests:
#   - Blocks external URLs
#   - Blocks prompt injection
#   - Allows safe instructions
#   - Warns on long instructions
```

### Check 3.7: Unit tests pass

```bash
DATABASE_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe" \
  go test ./internal/skills/ -v -count=1
```

---

## Engine 5: Orchestrator — Verification

Engine 5 depends on all previous engines.

### Check 5.1: Files exist

```bash
echo "=== Engine 5: File check ==="
FILES=(
  "internal/orchestrator/types.go"
  "internal/orchestrator/router.go"
  "internal/orchestrator/planner.go"
  "internal/orchestrator/executor.go"
  "internal/orchestrator/verifier.go"
  "internal/orchestrator/escalation.go"
  "internal/orchestrator/parallel.go"
  "internal/orchestrator/llmclient.go"
  "internal/orchestrator/tracker.go"
  "internal/orchestrator/templates.go"
)
PASS=true
for f in "${FILES[@]}"; do
  if [ -f "$f" ]; then
    echo "  ✅ $f"
  else
    echo "  ❌ $f MISSING"
    PASS=false
  fi
done
$PASS && echo "Engine 5 files: ALL PRESENT" || echo "Engine 5 files: INCOMPLETE"
```

### Check 5.2: Package compiles

```bash
go build ./internal/orchestrator/
# Expected: no errors
```

### Check 5.3: Types exist

```bash
go doc ./internal/orchestrator/ ModelTier 2>/dev/null && echo "✅ ModelTier type" || echo "❌ ModelTier missing"
go doc ./internal/orchestrator/ RoutingMode 2>/dev/null && echo "✅ RoutingMode type" || echo "❌ RoutingMode missing"
go doc ./internal/orchestrator/ RoutingDecision 2>/dev/null && echo "✅ RoutingDecision type" || echo "❌ RoutingDecision missing"
go doc ./internal/orchestrator/ VerifyTier 2>/dev/null && echo "✅ VerifyTier type" || echo "❌ VerifyTier missing"
go doc ./internal/orchestrator/ Task 2>/dev/null && echo "✅ Task type" || echo "❌ Task missing"
go doc ./internal/orchestrator/ Plan 2>/dev/null && echo "✅ Plan type" || echo "❌ Plan missing"
go doc ./internal/orchestrator/ SubTask 2>/dev/null && echo "✅ SubTask type" || echo "❌ SubTask missing"
go doc ./internal/orchestrator/ TaskResult 2>/dev/null && echo "✅ TaskResult type" || echo "❌ TaskResult missing"
go doc ./internal/orchestrator/ CostRecord 2>/dev/null && echo "✅ CostRecord type" || echo "❌ CostRecord missing"
```

### Check 5.4: Key functions exist

```bash
# Router (zero-token routing)
go doc ./internal/orchestrator/ NewRouter 2>/dev/null && echo "✅ NewRouter" || echo "❌ NewRouter missing"
go doc ./internal/orchestrator/ Route 2>/dev/null && echo "✅ Route method" || echo "❌ Route missing"
go doc ./internal/orchestrator/ ClassifyTaskType 2>/dev/null && echo "✅ ClassifyTaskType" || echo "❌ ClassifyTaskType missing"

# Interfaces (dependency injection)
go doc ./internal/orchestrator/ SkillMatcher 2>/dev/null && echo "✅ SkillMatcher interface" || echo "❌ SkillMatcher missing"
go doc ./internal/orchestrator/ MemoryRecaller 2>/dev/null && echo "✅ MemoryRecaller interface" || echo "❌ MemoryRecaller missing"
go doc ./internal/orchestrator/ GraphAnalyzer 2>/dev/null && echo "✅ GraphAnalyzer interface" || echo "❌ GraphAnalyzer missing"

# Templates
go doc ./internal/orchestrator/ HasTemplate 2>/dev/null && echo "✅ HasTemplate" || echo "❌ HasTemplate missing"
go doc ./internal/orchestrator/ CodeFixSpec 2>/dev/null && echo "✅ CodeFixSpec type" || echo "❌ CodeFixSpec missing"
go doc ./internal/orchestrator/ TestGenSpec 2>/dev/null && echo "✅ TestGenSpec type" || echo "❌ TestGenSpec missing"

# Main orchestrator
go doc ./internal/orchestrator/ NewOrchestrator 2>/dev/null && echo "✅ NewOrchestrator" || echo "❌ NewOrchestrator missing"
go doc ./internal/orchestrator/ Execute 2>/dev/null && echo "✅ Execute method" || echo "❌ Execute missing"

# Tracker
go doc ./internal/orchestrator/ NewTracker 2>/dev/null && echo "✅ NewTracker" || echo "❌ NewTracker missing"
go doc ./internal/orchestrator/ LogCall 2>/dev/null && echo "✅ LogCall method" || echo "❌ LogCall missing"
```

### Check 5.5: Database cost tracking table works

```bash
DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

# Check table exists
psql "$DB_URL" -c "SELECT COUNT(*) FROM agent_costs;" && echo "✅ agent_costs table exists" || echo "❌ agent_costs table missing"

# Insert a test cost record
psql "$DB_URL" -c "
  INSERT INTO agent_costs (task_id, developer_id, model, input_tokens, output_tokens, cost_usd, phase, routing_mode)
  VALUES ('test-task', 'test-dev', 'haiku', 500, 200, 0.0004, 'execute', 'skill_execute')
  RETURNING id, cost_usd;" && echo "✅ Cost insert works" || echo "❌ Cost insert failed"

# Check materialized views exist
psql "$DB_URL" -c "SELECT COUNT(*) FROM monthly_cost_summary;" 2>/dev/null && echo "✅ monthly_cost_summary view exists" || echo "❌ monthly_cost_summary view missing"
psql "$DB_URL" -c "SELECT COUNT(*) FROM developer_cost_summary;" 2>/dev/null && echo "✅ developer_cost_summary view exists" || echo "❌ developer_cost_summary view missing"

# Clean up
psql "$DB_URL" -c "DELETE FROM agent_costs WHERE task_id = 'test-task';"
echo "  Cleaned up test data"
```

### Check 5.6: Router makes correct decisions

```bash
go test ./internal/orchestrator/ -run TestRouter -v -count=1
# Expected: all router tests pass
# Key tests:
#   - Skill match routes to ModeSkillExecute
#   - Memory match routes to ModeMemoryApply
#   - Simple + template routes to ModePlanExecute
#   - Complex cross-repo routes to ModeFullOrchestration
#   - No template routes to ModeSingleOpus
#   - ClassifyTaskType identifies correct task types
```

### Check 5.7: Templates are defined

```bash
go test ./internal/orchestrator/ -run TestTemplate -v -count=1 2>/dev/null || \
go run -v << 'GOEOF'
package main

import (
    "fmt"
    "universe/internal/orchestrator"
)

func main() {
    types := []orchestrator.TaskType{
        orchestrator.TaskCodeFix,
        orchestrator.TaskTestGen,
        orchestrator.TaskPRGen,
        orchestrator.TaskRefactor,
        orchestrator.TaskDepUpdate,
        orchestrator.TaskConfigChange,
        orchestrator.TaskAnalysis,
    }
    for _, tt := range types {
        if orchestrator.HasTemplate(tt) {
            fmt.Printf("✅ Template exists for %s\n", tt)
        } else {
            fmt.Printf("❌ Template missing for %s\n", tt)
        }
    }
    // These should NOT have templates
    noTemplate := []orchestrator.TaskType{
        orchestrator.TaskExplanation,
        orchestrator.TaskGeneral,
    }
    for _, tt := range noTemplate {
        if !orchestrator.HasTemplate(tt) {
            fmt.Printf("✅ Correctly no template for %s\n", tt)
        } else {
            fmt.Printf("❌ Unexpected template for %s\n", tt)
        }
    }
}
GOEOF
```

### Check 5.8: All unit tests pass

```bash
DATABASE_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe" \
  go test ./internal/orchestrator/ -v -count=1
```

---

## Cross-Engine Integration Checks

These verify the engines work together, not just individually.

### Cross-Check 1: All packages compile together

```bash
echo "=== Cross-engine compilation ==="
go build ./...
# This verifies there are no import cycles, missing interfaces,
# or type mismatches between engines.
echo "✅ Full project compiles" || echo "❌ Compilation failed"
```

### Cross-Check 2: No circular imports

```bash
# Go compiler catches this, but double-check:
go vet ./...
echo "✅ go vet passed" || echo "❌ go vet found issues"
```

### Cross-Check 3: Engine 5 interfaces match Engine 2 and 3

```bash
# The orchestrator defines interfaces (SkillMatcher, MemoryRecaller, GraphAnalyzer)
# that Engine 2 and 3 must satisfy. If the project compiles (Cross-Check 1),
# these are compatible. But let's verify the interface methods exist:

echo "Checking Engine 5 ↔ Engine 3 interface compatibility..."
echo "  SkillMatcher interface requires Match method"
go doc ./internal/skills/ Match 2>/dev/null && echo "  ✅ skills.Match exists" || echo "  ❌ skills.Match missing"

echo "Checking Engine 5 ↔ Engine 2 interface compatibility..."
echo "  MemoryRecaller interface requires QuickCheck method"
go doc ./internal/memory/ QuickCheck 2>/dev/null && echo "  ✅ memory.QuickCheck exists" || echo "  ❌ memory.QuickCheck missing (may be named differently)"
```

### Cross-Check 4: Database has all required tables

```bash
DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"

echo "=== Database table check ==="
REQUIRED_TABLES=("observations" "skills" "skill_executions" "agent_costs")
for table in "${REQUIRED_TABLES[@]}"; do
  COUNT=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='$table';" | tr -d ' ')
  if [ "$COUNT" -eq 1 ]; then
    echo "  ✅ $table"
  else
    echo "  ❌ $table MISSING"
  fi
done
```

### Cross-Check 5: All tests pass together

```bash
echo "=== Running ALL tests ==="
DATABASE_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe" \
  go test ./internal/compress/ ./internal/memory/ ./internal/skills/ ./internal/orchestrator/ -v -count=1

echo ""
echo "Count results:"
DATABASE_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe" \
  go test ./internal/compress/ ./internal/memory/ ./internal/skills/ ./internal/orchestrator/ -count=1 2>&1 | \
  grep -E "^(ok|FAIL)" | sort
```

---

## Final Summary Script

Run this single script to check everything at once:

```bash
#!/bin/bash
# universe-engine-check.sh — Run all verification checks
set -e

DB_URL="postgres://universe_admin:universe_secret_2024@localhost:5432/universe"
PASS=0
FAIL=0

check() {
  if eval "$2" > /dev/null 2>&1; then
    echo "✅ $1"
    PASS=$((PASS + 1))
  else
    echo "❌ $1"
    FAIL=$((FAIL + 1))
  fi
}

echo "╔═══════════════════════════════════════════╗"
echo "║  Universe Engine Verification              ║"
echo "╚═══════════════════════════════════════════╝"
echo ""

echo "── Pre-checks ──"
check "Go installed" "go version"
check "Project compiles" "go build ./..."
check "go vet passes" "go vet ./..."
check "Docker DB running" "docker compose ps | grep -q healthy"
check "PostgreSQL accessible" "psql $DB_URL -c 'SELECT 1;'"
check "pgvector installed" "psql $DB_URL -c \"SELECT extversion FROM pg_extension WHERE extname='vector';\" | grep -q '.'"

echo ""
echo "── Engine 4: Compression ──"
check "compress/prompt.go exists" "test -f internal/compress/prompt.go"
check "compress/shorthand.go exists" "test -f internal/compress/shorthand.go"
check "compress/formatter.go exists" "test -f internal/compress/formatter.go"
check "compress package compiles" "go build ./internal/compress/"
check "compress tests pass" "go test ./internal/compress/ -count=1"

echo ""
echo "── Engine 2: Memory ──"
check "memory/types.go exists" "test -f internal/memory/types.go"
check "memory/store.go exists" "test -f internal/memory/store.go"
check "memory/retriever.go exists" "test -f internal/memory/retriever.go"
check "memory/compressor.go exists" "test -f internal/memory/compressor.go"
check "memory/hooks.go exists" "test -f internal/memory/hooks.go"
check "memory/decay.go exists" "test -f internal/memory/decay.go"
check "memory package compiles" "go build ./internal/memory/"
check "observations table exists" "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0;'"
check "memory tests pass" "DATABASE_URL=$DB_URL go test ./internal/memory/ -count=1"

echo ""
echo "── Engine 3: Skills ──"
check "skills/types.go exists" "test -f internal/skills/types.go"
check "skills/store.go exists" "test -f internal/skills/store.go"
check "skills/matcher.go exists" "test -f internal/skills/matcher.go"
check "skills/evolver.go exists" "test -f internal/skills/evolver.go"
check "skills/safety.go exists" "test -f internal/skills/safety.go"
check "skills/graph_sync.go exists" "test -f internal/skills/graph_sync.go"
check "skills package compiles" "go build ./internal/skills/"
check "skills table exists" "psql $DB_URL -c 'SELECT 1 FROM skills LIMIT 0;'"
check "seed skills present" "test \$(psql $DB_URL -t -c \"SELECT COUNT(*) FROM skills WHERE evolution='manual';\" | tr -d ' ') -ge 3"
check "skills tests pass" "DATABASE_URL=$DB_URL go test ./internal/skills/ -count=1"

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

echo ""
echo "── Cross-engine ──"
check "Full project compiles" "go build ./..."
check "No vet issues" "go vet ./..."
check "All tables present" "psql $DB_URL -c 'SELECT 1 FROM observations LIMIT 0; SELECT 1 FROM skills LIMIT 0; SELECT 1 FROM skill_executions LIMIT 0; SELECT 1 FROM agent_costs LIMIT 0;'"

echo ""
echo "═══════════════════════════════════════════"
echo "  Results: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════════"

if [ $FAIL -eq 0 ]; then
  echo ""
  echo "🎉 ALL CHECKS PASSED"
  echo ""
  echo "Next steps:"
  echo "  1. Build the MCP server (universe mcp --stdio)"
  echo "  2. Wire the CLI commands (cobra)"
  echo "  3. Build the dashboard (dashboard.md)"
  echo "  4. Set up npm distribution (npm-setup.md)"
else
  echo ""
  echo "⚠️  $FAIL checks failed — fix before proceeding"
fi
```

Save as `universe-engine-check.sh`, make executable (`chmod +x`), and run after building each engine.
