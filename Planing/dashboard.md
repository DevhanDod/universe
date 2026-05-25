# Universe Dashboard

## Build Specification for Claude Code

**Name:** Universe Dashboard — per-engine visibility into graph, memory, skills, compression, and routing  
**Port:** 3001 (separate from MCP server on 8080 and graph visualizer on 3000)  
**Tech stack:** Go HTTP server serving a single-page React app (bundled as static files)  
**Estimated effort:** 2-3 days  
**Dependencies:** All 5 engines built. PostgreSQL running with data.  

---

## 1. What This Dashboard Does (Plain English)

Developers and managers need to SEE what the system is doing — not just metrics, but the actual data. The dashboard has 6 views:

1. **Overview** — headline numbers: total cost saved, tokens saved, engines status, monthly trend
2. **Graph** — the existing interactive graph, enhanced with memory/skill badge counts on nodes
3. **Memory** — live stream of every observation, filterable by developer, repo, graph node, category
4. **Skills** — every skill with its evolution tree (v1 → v2 → v3), success rates, frozen skill alerts
5. **Compression** — before/after of actual prompts, graph shorthand preview, token counts
6. **Routing** — flight tracker for every task: which model, why, escalation chain, cost per task

All 6 views are connected through graph node IDs. Click a function in any view → jump to that function in any other view.

---

## 2. Architecture

```
Developer's browser
       │
       │  http://localhost:3001
       │
┌──────▼──────────────────────────────────────┐
│  Go HTTP server (port 3001)                  │
│                                              │
│  /                → serves React SPA         │
│  /api/overview    → overview stats           │
│  /api/memory      → observations list        │
│  /api/skills      → skills list + lineage    │
│  /api/compression → compression samples      │
│  /api/routing     → task routing traces      │
│  /api/graph       → graph data (nodes+edges) │
│                                              │
│  Reads from PostgreSQL (same DB as engines)  │
└──────┬──────────────────────────────────────┘
       │
       ▼
  PostgreSQL (observations, skills, agent_costs tables)
```

The dashboard is a Go HTTP server that serves:
- Static files (the bundled React app) at `/`
- REST API endpoints at `/api/*` that query PostgreSQL

**Launched via CLI:**

```bash
universe dashboard
# Opens http://localhost:3001 in the browser
# Dashboard server runs in the foreground

universe dashboard --port 3001 --no-open
# Custom port, don't auto-open browser
```

---

## 3. Project Structure

```
UNIVERSE/
├── internal/
│   ├── dashboard/
│   │   ├── server.go         # Go HTTP server + API routes
│   │   ├── handlers.go       # API handler functions (one per view)
│   │   ├── queries.go        # PostgreSQL queries for each API endpoint
│   │   └── static/           # Built React app (index.html, bundle.js, styles.css)
│   └── ... (other engines)
├── dashboard/                 # React source code (built → internal/dashboard/static/)
│   ├── package.json
│   ├── src/
│   │   ├── App.jsx
│   │   ├── index.jsx
│   │   ├── pages/
│   │   │   ├── Overview.jsx
│   │   │   ├── Memory.jsx
│   │   │   ├── Skills.jsx
│   │   │   ├── Compression.jsx
│   │   │   ├── Routing.jsx
│   │   │   └── Graph.jsx
│   │   └── components/
│   │       ├── Layout.jsx        # Sidebar nav + main content area
│   │       ├── MetricCard.jsx
│   │       ├── ObservationRow.jsx
│   │       ├── SkillTree.jsx
│   │       ├── RoutingTrace.jsx
│   │       ├── CompressionDiff.jsx
│   │       ├── Filters.jsx
│   │       └── EngineStatus.jsx
│   └── build/                # Output of npm run build → copied to internal/dashboard/static/
└── ...
```

---

## 4. Go Backend: `internal/dashboard/server.go`

```go
package dashboard

// Server runs the dashboard HTTP server on a separate port.
// Serves the React SPA as static files and REST API endpoints.

// NewServer creates a new dashboard server.
// Parameters:
//   - databaseURL: PostgreSQL connection string
//   - port: port to listen on (default: 3001)
//   - staticDir: path to the built React app (default: embedded via go:embed)
//
// The static files can be embedded in the Go binary using go:embed,
// so the dashboard ships as a single binary with no external files needed.
//
//   //go:embed static/*
//   var staticFiles embed.FS
func NewServer(databaseURL string, port int) (*Server, error)

// Start starts the HTTP server.
// Registers all routes and begins listening.
//
// Routes:
//   GET /              → serves index.html (React SPA)
//   GET /assets/*      → serves JS, CSS, images
//
//   GET /api/overview             → overview stats
//   GET /api/memory               → paginated observations list
//   GET /api/memory/:id           → single observation detail
//   GET /api/skills               → all active skills
//   GET /api/skills/:id           → single skill detail
//   GET /api/skills/:id/lineage   → version DAG for a skill
//   GET /api/compression/samples  → recent compression before/after samples
//   GET /api/routing              → paginated task routing traces
//   GET /api/routing/:taskId      → single task routing detail
//   GET /api/graph/nodes          → all graph nodes with badge counts
//   GET /api/graph/edges          → all graph edges
//   GET /api/graph/node/:id       → single node with linked memories, skills, routes
//
// All API endpoints support these common query params:
//   ?developer=alice          filter by developer
//   ?repo=auth-service        filter by repository
//   ?graph_node=auth:Validate filter by graph node
//   ?category=fix             filter by category (memory only)
//   ?from=2026-01-01&to=2026-06-01  date range
//   ?page=1&limit=20          pagination
func (s *Server) Start() error

// Stop gracefully shuts down the server.
func (s *Server) Stop() error
```

---

## 5. Go Backend: `internal/dashboard/handlers.go`

One handler function per API endpoint. Each handler queries PostgreSQL and returns JSON.

```go
package dashboard

import "net/http"

// ============================================================
// OVERVIEW
// ============================================================

// HandleOverview returns headline stats across all engines.
//
// GET /api/overview
//
// Response:
// {
//   "engines": [
//     {"number": 1, "name": "Knowledge Graph", "status": "active", "detail": "142 nodes, 287 edges"},
//     {"number": 2, "name": "Persistent Memory", "status": "active", "detail": "341 observations, 78% recall rate"},
//     {"number": 3, "name": "Evolving Skills", "status": "active", "detail": "58 active, 1 frozen"},
//     {"number": 4, "name": "Compression", "status": "active", "detail": "compact mode, 72% reduction"},
//     {"number": 5, "name": "Orchestrator", "status": "active", "detail": "89% Haiku, $1.84 today"}
//   ],
//   "monthly": {
//     "actual_cost": 62.00,
//     "would_have_cost": 6800.00,
//     "savings_usd": 6738.00,
//     "savings_pct": 99.1,
//     "total_tasks": 516,
//     "haiku_pct": 89,
//     "skill_uses": 198,
//     "memory_hits": 402,
//     "takeovers": 3
//   },
//   "trend": [
//     {"month": "Jan", "actual": 420, "would_have": 5200},
//     {"month": "Feb", "actual": 310, "would_have": 5800},
//     ...
//   ]
// }
func (s *Server) HandleOverview(w http.ResponseWriter, r *http.Request)

// ============================================================
// MEMORY
// ============================================================

// HandleMemoryList returns paginated observations.
//
// GET /api/memory?developer=alice&category=fix&graph_node=auth:Validate&page=1&limit=20
//
// Response:
// {
//   "observations": [
//     {
//       "id": "uuid",
//       "developer_id": "alice",
//       "graph_node_id": "auth-service:auth:ValidateToken",
//       "category": "fix",
//       "summary": "Fixed type mismatch — changed param from int to string",
//       "confidence": 0.92,
//       "shared": true,
//       "created_at": "2026-05-23T14:30:00Z",
//       "recalled_at": "2026-05-25T09:15:00Z"
//     },
//     ...
//   ],
//   "total": 341,
//   "page": 1,
//   "limit": 20,
//   "filters_applied": {"developer": "alice", "category": "fix"}
// }
func (s *Server) HandleMemoryList(w http.ResponseWriter, r *http.Request)

// HandleMemoryDetail returns full observation with detail field.
//
// GET /api/memory/:id
//
// Response: full Observation JSON including the detail field,
// tool_calls, and session_id
func (s *Server) HandleMemoryDetail(w http.ResponseWriter, r *http.Request)

// ============================================================
// SKILLS
// ============================================================

// HandleSkillsList returns all active skills with metrics.
//
// GET /api/skills?language=go&sort=success_rate
//
// Response:
// {
//   "skills": [
//     {
//       "id": "uuid",
//       "name": "cross-repo-type-fix",
//       "version": 3,
//       "evolution": "fix",
//       "language": "go",
//       "trigger_desc": "When there's a type mismatch between repos",
//       "graph_node_ids": ["auth:ValidateToken", "gateway:LoginHandler"],
//       "confidence": 0.92,
//       "success_rate": 0.91,
//       "times_applied": 34,
//       "times_succeeded": 31,
//       "is_frozen": false,
//       "created_at": "2026-03-12T...",
//       "created_by": "auto"
//     },
//     ...
//   ],
//   "stats": {
//     "total_active": 58,
//     "total_frozen": 1,
//     "by_evolution": {"captured": 30, "fix": 18, "derived": 7, "manual": 3},
//     "avg_success_rate": 0.84
//   }
// }
func (s *Server) HandleSkillsList(w http.ResponseWriter, r *http.Request)

// HandleSkillDetail returns a single skill with its full instruction text.
//
// GET /api/skills/:id
//
// Response: full Skill JSON including instruction, test_case,
// negative_tags, success_by_complexity
func (s *Server) HandleSkillDetail(w http.ResponseWriter, r *http.Request)

// HandleSkillLineage returns the full evolution tree for a skill.
//
// GET /api/skills/:id/lineage
//
// Response:
// {
//   "lineage": [
//     {"id": "uuid-v1", "name": "type-fix", "version": 1, "evolution": "captured", "parent_id": null, "created_by": "alice", "created_at": "..."},
//     {"id": "uuid-v2", "name": "type-fix", "version": 2, "evolution": "fix", "parent_id": "uuid-v1", "created_by": "auto", "created_at": "..."},
//     {"id": "uuid-v3", "name": "type-fix", "version": 3, "evolution": "fix", "parent_id": "uuid-v2", "created_by": "auto", "created_at": "..."}
//   ],
//   "derived": [
//     {"id": "uuid-py", "name": "type-fix-python", "version": 1, "evolution": "derived", "parent_id": "uuid-v1", "created_by": "bob", "created_at": "..."}
//   ]
// }
//
// The lineage array is the direct parent chain (linear).
// The derived array contains branches (DERIVED variants from any point in the chain).
// Together they form the version DAG.
func (s *Server) HandleSkillLineage(w http.ResponseWriter, r *http.Request)

// ============================================================
// COMPRESSION
// ============================================================

// HandleCompressionSamples returns recent before/after compression examples.
//
// GET /api/compression/samples?limit=10
//
// Response:
// {
//   "samples": [
//     {
//       "task_id": "task-123",
//       "timestamp": "2026-05-25T10:30:00Z",
//       "level": "compact",
//       "before_tokens": 847,
//       "after_tokens": 237,
//       "savings_pct": 72.0,
//       "before_preview": "I'd be happy to help you fix the type mismatch! Let me look at...",
//       "after_preview": "Type mismatch in auth.ValidateToken (validate.go:42). Parameter expects int...",
//       "graph_shorthand": "• auth.ValidateToken [func] (validate.go:42) ← gateway.LoginHandler..."
//     },
//     ...
//   ],
//   "stats": {
//     "avg_output_reduction": 72.0,
//     "avg_input_reduction": 35.0,
//     "active_level": "compact",
//     "total_tokens_saved_today": 48200
//   }
// }
//
// NOTE: To capture compression samples, Engine 4's BuildPrompt function
// needs to log before/after to a compression_samples table.
// Add this table in a new migration (see section 7).
func (s *Server) HandleCompressionSamples(w http.ResponseWriter, r *http.Request)

// ============================================================
// ROUTING
// ============================================================

// HandleRoutingList returns paginated task routing traces.
//
// GET /api/routing?developer=alice&routing_mode=skill_execute&page=1&limit=20
//
// Response:
// {
//   "tasks": [
//     {
//       "task_id": "task-123",
//       "developer_id": "alice",
//       "prompt_preview": "Fix the type mismatch in auth.ValidateToken",
//       "routing_mode": "skill_execute",
//       "total_tokens": 380,
//       "total_cost": 0.0005,
//       "would_have_cost": 0.32,
//       "latency_ms": 2100,
//       "skill_used": "cross-repo-type-fix v3",
//       "memory_hit": true,
//       "escalation_steps": 0,
//       "was_takeover": false,
//       "created_at": "2026-05-25T10:30:00Z"
//     },
//     ...
//   ],
//   "total": 516,
//   "page": 1,
//   "stats": {
//     "tasks_today": 47,
//     "haiku_pct": 89,
//     "cost_today": 1.84,
//     "takeovers_today": 0,
//     "by_routing_mode": {
//       "skill_execute": 38,
//       "memory_apply": 24,
//       "plan_execute": 22,
//       "single_haiku": 8,
//       "full_orchestration": 5,
//       "single_opus": 3
//     }
//   }
// }
func (s *Server) HandleRoutingList(w http.ResponseWriter, r *http.Request)

// HandleRoutingDetail returns the full routing trace for a single task.
//
// GET /api/routing/:taskId
//
// Response:
// {
//   "task_id": "task-123",
//   "developer_id": "alice",
//   "prompt": "Fix the type mismatch in auth.ValidateToken",
//   "routing_mode": "skill_execute",
//   "routing_reason": "Skill match found: cross-repo-type-fix v3 (91% success, 0.85 graph overlap)",
//   "trace": [
//     {"step": 1, "action": "router_decision", "detail": "Checked skills → match found", "tokens": 0, "model": null, "duration_ms": 12},
//     {"step": 2, "action": "memory_recall", "detail": "Found: Alice fixed same function 2 weeks ago", "tokens": 0, "model": null, "duration_ms": 45},
//     {"step": 3, "action": "execute", "detail": "Haiku applied skill instruction with memory context", "tokens": 380, "model": "haiku", "duration_ms": 1800},
//     {"step": 4, "action": "verify", "detail": "Tier 1 automated: go vet passed, tests passed", "tokens": 0, "model": null, "duration_ms": 250},
//     {"step": 5, "action": "complete", "detail": "Success", "tokens": 0, "model": null, "duration_ms": 0}
//   ],
//   "total_tokens": 380,
//   "total_cost": 0.0005,
//   "would_have_cost": 0.32,
//   "total_latency_ms": 2107,
//   "created_at": "2026-05-25T10:30:00Z"
// }
func (s *Server) HandleRoutingDetail(w http.ResponseWriter, r *http.Request)

// ============================================================
// GRAPH (enhanced with badge counts)
// ============================================================

// HandleGraphNodes returns all graph nodes with memory/skill counts.
//
// GET /api/graph/nodes
//
// Response:
// {
//   "nodes": [
//     {
//       "id": "auth-service:auth:ValidateToken",
//       "name": "ValidateToken",
//       "kind": "function",
//       "package": "auth",
//       "repo": "auth-service",
//       "file": "validate.go",
//       "line": 42,
//       "memory_count": 3,
//       "skill_count": 2,
//       "has_stale_skill": false,
//       "last_activity": "2026-05-25T10:30:00Z"
//     },
//     ...
//   ]
// }
//
// SQL for badge counts:
//   SELECT n.*,
//     (SELECT COUNT(*) FROM observations o WHERE o.graph_node_id = n.id) AS memory_count,
//     (SELECT COUNT(*) FROM skills s WHERE n.id = ANY(s.graph_node_ids) AND s.is_active = true) AS skill_count,
//     (SELECT COUNT(*) > 0 FROM skills s WHERE n.id = ANY(s.graph_node_ids) AND s.is_active = true
//       AND s.negative_tags::text LIKE '%graph_changed%') AS has_stale_skill
//   FROM graph_nodes n
func (s *Server) HandleGraphNodes(w http.ResponseWriter, r *http.Request)

// HandleGraphNodeDetail returns everything linked to a single graph node.
// This is the "click a node, see everything" endpoint.
//
// GET /api/graph/node/:id
//
// Response:
// {
//   "node": { ...node details... },
//   "callers": ["gateway:LoginHandler", "token:RefreshToken"],
//   "callees": ["crypto:VerifyJWT"],
//   "memories": [ ...observations for this node... ],
//   "skills": [ ...skills covering this node... ],
//   "recent_routes": [ ...recent tasks involving this node... ]
// }
func (s *Server) HandleGraphNodeDetail(w http.ResponseWriter, r *http.Request)
```

---

## 6. Go Backend: `internal/dashboard/queries.go`

All PostgreSQL queries used by the handlers.

```go
package dashboard

// All queries are parameterized (no SQL injection risk).
// Each query function takes a database connection and filter parameters,
// returns typed results.

// ============================================================
// OVERVIEW QUERIES
// ============================================================

// QueryEngineStats returns status for each engine.
//
// Engine 1: SELECT COUNT(*) as nodes, COUNT(*) as edges FROM graph data
// Engine 2: SELECT COUNT(*) as observations,
//           COUNT(*) FILTER (WHERE recalled_at IS NOT NULL) / COUNT(*)::float as recall_rate
//           FROM observations
// Engine 3: SELECT COUNT(*) FILTER (WHERE is_active) as active,
//           COUNT(*) FILTER (WHERE is_frozen) as frozen
//           FROM skills
// Engine 5: Read from monthly_cost_summary materialized view
func QueryEngineStats(db *pgxpool.Pool) (*EngineStatsResponse, error)

// QueryMonthlyTrend returns month-by-month cost data.
// Reads from the monthly_cost_summary materialized view.
func QueryMonthlyTrend(db *pgxpool.Pool) ([]MonthlyDataPoint, error)

// ============================================================
// MEMORY QUERIES
// ============================================================

// QueryObservations returns filtered, paginated observations.
//
// SQL:
//   SELECT id, developer_id, repo_id, graph_node_id, category,
//          summary, confidence, shared, created_at, recalled_at
//   FROM observations
//   WHERE ($1 = '' OR developer_id = $1)
//     AND ($2 = '' OR category = $2)
//     AND ($3 = '' OR graph_node_id = $3)
//     AND ($4 = '' OR repo_id = $4)
//     AND created_at >= $5 AND created_at <= $6
//   ORDER BY created_at DESC
//   LIMIT $7 OFFSET $8
//
// Also returns total count for pagination.
func QueryObservations(db *pgxpool.Pool, filters ObservationFilters) (*ObservationListResponse, error)

// QueryObservationDetail returns a single observation with full detail.
func QueryObservationDetail(db *pgxpool.Pool, id string) (*ObservationDetailResponse, error)

// ============================================================
// SKILLS QUERIES
// ============================================================

// QuerySkills returns all active skills with metrics.
//
// SQL:
//   SELECT id, name, version, evolution, language, trigger_desc,
//          graph_node_ids, confidence,
//          CASE WHEN times_applied > 0
//            THEN times_succeeded::float / times_applied
//            ELSE 0 END as success_rate,
//          times_applied, times_succeeded, times_failed,
//          is_frozen, created_by, created_at
//   FROM skills
//   WHERE is_active = true
//     AND ($1 = '' OR language = $1)
//   ORDER BY
//     CASE WHEN $2 = 'success_rate' THEN times_succeeded::float / GREATEST(times_applied, 1) END DESC,
//     CASE WHEN $2 = 'confidence' THEN confidence END DESC,
//     CASE WHEN $2 = 'applied' THEN times_applied END DESC,
//     created_at DESC
func QuerySkills(db *pgxpool.Pool, filters SkillFilters) (*SkillListResponse, error)

// QuerySkillDetail returns full skill including instruction text.
func QuerySkillDetail(db *pgxpool.Pool, id string) (*SkillDetailResponse, error)

// QuerySkillLineage returns the version DAG for a skill.
//
// Two queries:
// 1. Linear lineage (recursive CTE walking parent_id up):
//    WITH RECURSIVE lineage AS (
//      SELECT * FROM skills WHERE id = $1
//      UNION ALL
//      SELECT s.* FROM skills s JOIN lineage l ON s.id = l.parent_id
//    )
//    SELECT * FROM lineage ORDER BY version ASC
//
// 2. Derived branches (children with evolution = 'derived'):
//    SELECT * FROM skills WHERE parent_id IN (SELECT id FROM lineage) AND evolution = 'derived'
func QuerySkillLineage(db *pgxpool.Pool, id string) (*SkillLineageResponse, error)

// ============================================================
// COMPRESSION QUERIES
// ============================================================

// QueryCompressionSamples returns recent before/after samples.
// Reads from the compression_samples table (see section 7 for schema).
func QueryCompressionSamples(db *pgxpool.Pool, limit int) (*CompressionSamplesResponse, error)

// QueryCompressionStats returns aggregate compression metrics.
//
// SQL:
//   SELECT
//     AVG(1.0 - after_tokens::float / GREATEST(before_tokens, 1)) * 100 as avg_reduction,
//     SUM(before_tokens - after_tokens) as total_tokens_saved_today
//   FROM compression_samples
//   WHERE created_at >= CURRENT_DATE
func QueryCompressionStats(db *pgxpool.Pool) (*CompressionStatsResponse, error)

// ============================================================
// ROUTING QUERIES
// ============================================================

// QueryRoutingTasks returns paginated routing traces.
//
// SQL:
//   SELECT DISTINCT ON (task_id)
//     task_id, developer_id, routing_mode,
//     SUM(input_tokens + output_tokens) OVER (PARTITION BY task_id) as total_tokens,
//     SUM(cost_usd) OVER (PARTITION BY task_id) as total_cost,
//     MAX(latency_ms) OVER (PARTITION BY task_id) as total_latency,
//     MAX(escalation_steps) OVER (PARTITION BY task_id) as escalation_steps,
//     BOOL_OR(was_takeover) OVER (PARTITION BY task_id) as was_takeover,
//     BOOL_OR(memory_hit) OVER (PARTITION BY task_id) as memory_hit,
//     MIN(created_at) OVER (PARTITION BY task_id) as created_at
//   FROM agent_costs
//   WHERE ($1 = '' OR developer_id = $1)
//     AND ($2 = '' OR routing_mode = $2)
//   ORDER BY task_id, created_at
//   LIMIT $3 OFFSET $4
func QueryRoutingTasks(db *pgxpool.Pool, filters RoutingFilters) (*RoutingListResponse, error)

// QueryRoutingDetail returns the full trace for a single task.
//
// SQL:
//   SELECT * FROM agent_costs
//   WHERE task_id = $1
//   ORDER BY created_at ASC
//
// Each row is one step in the trace (plan, execute, verify, escalate, etc.)
func QueryRoutingDetail(db *pgxpool.Pool, taskID string) (*RoutingDetailResponse, error)

// QueryRoutingStats returns today's routing breakdown.
//
// SQL:
//   SELECT
//     COUNT(*) as tasks_today,
//     COUNT(*) FILTER (WHERE model = 'haiku')::float / GREATEST(COUNT(*), 1) * 100 as haiku_pct,
//     SUM(cost_usd) as cost_today,
//     COUNT(*) FILTER (WHERE was_takeover) as takeovers_today,
//     routing_mode, COUNT(*) as count
//   FROM agent_costs
//   WHERE created_at >= CURRENT_DATE
//   GROUP BY routing_mode
func QueryRoutingStats(db *pgxpool.Pool) (*RoutingStatsResponse, error)

// ============================================================
// GRAPH QUERIES (enhanced)
// ============================================================

// QueryGraphNodesWithBadges returns graph nodes + memory/skill counts.
// See HandleGraphNodes for the SQL.
func QueryGraphNodesWithBadges(db *pgxpool.Pool) (*GraphNodesResponse, error)

// QueryGraphNodeDetail returns everything linked to a graph node.
func QueryGraphNodeDetail(db *pgxpool.Pool, nodeID string) (*GraphNodeDetailResponse, error)
```

---

## 7. Additional Database Migration

The compression dashboard needs a table to store before/after samples. Add this migration:

```sql
-- Migration: 005_compression_samples.sql

CREATE TABLE IF NOT EXISTS compression_samples (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         TEXT NOT NULL,
    developer_id    TEXT NOT NULL,
    compression_level TEXT NOT NULL,       -- 'full', 'compact', 'normal'
    before_tokens   INT NOT NULL,
    after_tokens    INT NOT NULL,
    before_preview  TEXT NOT NULL,          -- first 200 chars of the uncompressed prompt
    after_preview   TEXT NOT NULL,          -- first 200 chars of the compressed prompt
    graph_shorthand TEXT,                   -- the generated shorthand (if any)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_comp_samples_created ON compression_samples(created_at);
CREATE INDEX idx_comp_samples_developer ON compression_samples(developer_id);

-- Also add a task_prompt column to agent_costs for routing trace preview
-- (if not already present)
ALTER TABLE agent_costs ADD COLUMN IF NOT EXISTS task_prompt_preview TEXT;
```

Engine 4's `BuildPrompt` function needs a small addition: after building the prompt, log a sample to this table (async, non-blocking). One sample per task, not per sub-task.

---

## 8. React Frontend

### 8.1 Tech stack

- React 18 (via Vite for dev, built to static files for production)
- Recharts for charts (already used in the graph visualizer)
- React Router for navigation between views
- No CSS framework — custom CSS with dark/light mode support
- Build output: single `index.html` + `bundle.js` + `styles.css`

### 8.2 `dashboard/package.json`

```json
{
  "name": "universe-dashboard",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "vite build --outDir ../internal/dashboard/static",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "react-router-dom": "^6.23.0",
    "recharts": "^2.12.0"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "^4.3.0",
    "vite": "^5.4.0"
  }
}
```

### 8.3 `dashboard/src/App.jsx` — Main app with sidebar navigation

```jsx
// App.jsx — main layout with sidebar navigation

// SIDEBAR NAVIGATION:
//   - Overview    (icon: chart-bar)         /
//   - Graph       (icon: hierarchy-3)       /graph
//   - Memory      (icon: brain)             /memory
//   - Skills      (icon: dna)               /skills
//   - Compression (icon: arrows-minimize)   /compression
//   - Routing     (icon: route)             /routing
//
// LAYOUT:
//   ┌─────────┬──────────────────────────────────────┐
//   │ Sidebar │ Main content (page component)         │
//   │ (200px) │                                       │
//   │ Logo    │                                       │
//   │ Links   │                                       │
//   │         │                                       │
//   │ Status  │                                       │
//   │ bar at  │                                       │
//   │ bottom  │                                       │
//   └─────────┴──────────────────────────────────────┘
//
// SIDEBAR BOTTOM: mini engine status (5 dots: green=active, red=error)
//
// COLOR SCHEME:
//   Dark theme by default (matches developer IDEs)
//   Background: #060810
//   Sidebar: #0A0C10
//   Cards: #0F1117
//   Borders: #1a1f2e
//   Text primary: #e5e7eb
//   Text secondary: #6b7280
//   Accent green: #34d399 (savings, success)
//   Accent amber: #D4920A (skills, warnings)
//   Accent blue: #2B7CC9 (graph, info)
//   Accent purple: #534AB7 (routing, orchestrator)
//   Accent coral: #D85A30 (compression)
//   Accent teal: #0F6E56 (memory)
//
// FONTS:
//   Headings + body: system sans-serif stack
//   Numbers + code: JetBrains Mono (loaded from Google Fonts)
```

### 8.4 Page Components — What Each Shows

#### Overview.jsx
- Engine status strip (5 engines with status badge + one-line detail)
- 4 metric cards: monthly cost, total saved, cost reduction %, per-task cost
- Area chart: actual cost vs would-have-been cost (6 month trend)
- Pie chart: routing mode breakdown
- Area chart: Haiku % over time (trending toward 90%+)

#### Memory.jsx
- Filter bar: developer dropdown, category pills (fix/pattern/decision/failure/convention), graph node search, date range
- 3 metric cards: total observations, recall hit rate, shared count
- Observation list: each row shows timestamp, developer avatar, category badge, summary text, graph node link, confidence bar
- Click any observation → expand to show full detail + tool_calls
- Click any graph node badge → navigates to `/graph?node=<id>`

#### Skills.jsx
- Filter bar: language dropdown, sort by (success rate / confidence / most applied), search
- 3 metric cards: active skills, avg success rate, frozen count
- Skill list: each row shows name, version, evolution badge (captured/fix/derived), language tag, success rate bar, confidence bar, applied count
- Click any skill → expand to show:
  - Full instruction text (in a code block)
  - Evolution tree (visual: v1 → v2 → v3 with branch for derived)
  - Graph nodes covered (clickable badges)
  - Negative tags (if any)
  - Test case (if any)
  - Recent execution log (last 10)
- Frozen skills section at top with amber border and "Review" button

#### Compression.jsx
- 3 metric cards: avg output reduction, avg input reduction, tokens saved today
- Sample list: each row shows a before/after side-by-side
  - Left: "Before" with token count, preview text (red/muted)
  - Right: "After" with token count, preview text (green/bright)
  - Below: graph shorthand that was generated (monospace block)
- Token waterfall chart: Baseline → After Graph → After Memory → After Skills → After Compression (horizontal bar chart)

#### Routing.jsx
- Filter bar: developer dropdown, routing mode pills, date range, show takeovers only toggle
- 4 metric cards: tasks today, Haiku %, cost today, takeovers
- Task list: each row shows timestamp, developer, prompt preview (truncated), routing mode badge, tokens, cost, latency, escalation icon if any
- Click any task → expand to show the full routing trace:
  - Step-by-step vertical timeline with colored dots
  - Each step: action, detail text, model badge (Opus/Haiku/none), token count, duration
  - Escalation steps shown in amber
  - Takeover steps shown in red
  - Final step shows: total tokens, total cost, "would have cost" comparison

#### Graph.jsx
- The existing graph visualizer, enhanced with:
  - Memory count badge on each node (teal dot with number)
  - Skill count badge on each node (amber dot with number)
  - Amber border on nodes with stale skills
  - Click a badge → navigate to `/memory?graph_node=<id>` or `/skills?graph_node=<id>`
  - Hover popup: node name, recent memory summary, best skill, last routing

### 8.5 Shared Components

```
components/
├── Layout.jsx          # Sidebar + main content wrapper
├── MetricCard.jsx      # Number card (label, value, sub-text, trend arrow)
├── ObservationRow.jsx  # One observation in the memory list
├── SkillTree.jsx       # Visual evolution tree (v1 → v2 → v3 with branches)
├── RoutingTrace.jsx    # Vertical timeline of routing steps
├── CompressionDiff.jsx # Side-by-side before/after with token counts
├── Filters.jsx         # Reusable filter bar (dropdowns, pills, search, date range)
├── EngineStatus.jsx    # Engine status badge (number, name, status, detail)
├── ConfidenceBar.jsx   # Thin horizontal bar showing 0.0-1.0 confidence
├── Badge.jsx           # Category/evolution/model badge
└── GraphNodeLink.jsx   # Clickable graph node reference (monospace, blue)
```

---

## 9. CLI Integration

Add the `dashboard` command to your Universe CLI:

```go
// In cmd/universe/main.go or your cobra command setup:

var dashboardCmd = &cobra.Command{
    Use:   "dashboard",
    Short: "Open the Universe dashboard",
    Run: func(cmd *cobra.Command, args []string) {
        port, _ := cmd.Flags().GetInt("port")
        noOpen, _ := cmd.Flags().GetBool("no-open")
        dbURL := getDBURL() // from config

        server, err := dashboard.NewServer(dbURL, port)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }

        fmt.Printf("📊 Universe Dashboard running at http://localhost:%d\n", port)

        if !noOpen {
            // Open browser
            openBrowser(fmt.Sprintf("http://localhost:%d", port))
        }

        // Run until Ctrl+C
        server.Start()
    },
}

func init() {
    dashboardCmd.Flags().Int("port", 3001, "Dashboard port")
    dashboardCmd.Flags().Bool("no-open", false, "Don't auto-open browser")
    rootCmd.AddCommand(dashboardCmd)
}
```

---

## 10. Cross-View Navigation

Every view links to every other view through graph node IDs. Here's the navigation map:

```
GRAPH NODE click
  → /memory?graph_node=auth:ValidateToken     (see memories for this function)
  → /skills?graph_node=auth:ValidateToken     (see skills covering this function)
  → /routing?graph_node=auth:ValidateToken    (see tasks involving this function)

MEMORY observation click
  → /graph?node=auth:ValidateToken            (see this function in the graph)
  → /skills?id=skill-uuid                     (if a skill was used in this session)
  → /routing?task_id=task-123                 (see how this session was routed)

SKILL click
  → /graph?node=auth:ValidateToken            (see covered graph nodes)
  → /memory?graph_node=auth:ValidateToken     (see memories for covered nodes)
  → /routing?skill_id=skill-uuid              (see tasks that used this skill)

ROUTING task click
  → /skills?id=skill-uuid                     (see the skill that was used)
  → /memory?id=observation-uuid               (see the memory that was recalled)
  → /graph?node=auth:ValidateToken            (see involved graph nodes)
```

Implement with React Router `<Link>` components and query params. Every clickable entity (graph node, observation, skill, task) has a URL that other views can link to.

---

## 11. Build and Deployment

```bash
# Development (hot reload):
cd dashboard
npm install
npm run dev
# Opens at localhost:5173 with Vite dev server
# API calls proxied to Go backend at localhost:3001

# Production build:
cd dashboard
npm run build
# Outputs to internal/dashboard/static/

# The Go binary embeds the static files:
# //go:embed static/*
# var staticFiles embed.FS
#
# So the final binary includes the dashboard — no separate files needed.
# "universe dashboard" serves everything from the single binary.

# Full build:
go build -o universe ./cmd/universe
./universe dashboard
# Dashboard at localhost:3001, fully self-contained
```

---

## 12. Testing

### API tests (Go)

```go
// Test 1: Overview endpoint returns all engine stats
func TestHandleOverview(t *testing.T) {}

// Test 2: Memory list with filters
func TestHandleMemoryList_Filtered(t *testing.T) {}

// Test 3: Memory list pagination
func TestHandleMemoryList_Pagination(t *testing.T) {}

// Test 4: Skill lineage returns correct DAG
func TestHandleSkillLineage(t *testing.T) {}

// Test 5: Routing detail returns full trace
func TestHandleRoutingDetail(t *testing.T) {}

// Test 6: Graph nodes include badge counts
func TestHandleGraphNodes_BadgeCounts(t *testing.T) {}

// Test 7: Graph node detail includes linked memories and skills
func TestHandleGraphNodeDetail(t *testing.T) {}

// Test 8: Compression samples endpoint
func TestHandleCompressionSamples(t *testing.T) {}

// Test 9: Routing stats aggregation
func TestQueryRoutingStats(t *testing.T) {}

// Test 10: CORS headers present (for dev mode)
func TestCORSHeaders(t *testing.T) {}
```

---

## 13. Acceptance Criteria

The dashboard is complete when:

- [ ] `universe dashboard` starts the server on port 3001
- [ ] Browser auto-opens to `http://localhost:3001`
- [ ] Sidebar navigation works between all 6 views
- [ ] Overview shows engine status, monthly cost, and trend chart
- [ ] Memory view shows filterable, paginated observation list
- [ ] Memory observations show category badges, graph node links, confidence bars
- [ ] Skills view shows all active skills with evolution badges and success rates
- [ ] Skills lineage shows the version DAG (v1 → v2 → v3 with derived branches)
- [ ] Frozen skills are highlighted with review action
- [ ] Compression view shows before/after samples with token counts
- [ ] Routing view shows task list with routing mode badges and cost
- [ ] Routing detail shows full step-by-step trace with model badges
- [ ] Graph view shows nodes with memory/skill badge counts
- [ ] Clicking any graph node badge navigates to the correct filtered view
- [ ] Cross-view navigation works (memory → graph → skills → routing → back)
- [ ] Dark theme renders correctly
- [ ] All 10 API tests pass
- [ ] Static files are embedded in the Go binary (no external files needed)
- [ ] `go build ./...` succeeds

---

## 14. What NOT to Build

- Do NOT build real-time WebSocket updates — polling every 30 seconds is fine for V1
- Do NOT build user authentication for the dashboard — it runs locally, localhost only
- Do NOT build data export (CSV/PDF) — copy-paste from the browser works for now
- Do NOT build custom chart builder — fixed views are enough
- Do NOT build mobile layout — desktop only for V1
- Do NOT add the dashboard to the npm package yet — it's a development/admin tool

---

## 15. Future Improvements

1. **Auto-refresh** — poll API every 30 seconds, animate new entries sliding in
2. **Real-time mode** — WebSocket for live routing traces as they happen
3. **Alerting** — browser notifications when a skill is frozen or takeover rate spikes
4. **Team comparison** — side-by-side developer stats
5. **Skill playground** — test a skill instruction against sample inputs in the browser
6. **Memory search** — search bar with hybrid search (keyword + semantic) from the dashboard
7. **Export** — download observations, skills, or cost data as CSV
8. **Mobile layout** — responsive design for checking stats on phone
9. **Auth** — add login when dashboard is exposed beyond localhost (team server mode)
10. **Embed in Cursor** — show a mini dashboard widget inside Cursor's sidebar panel
