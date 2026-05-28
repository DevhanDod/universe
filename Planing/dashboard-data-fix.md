# Fix: Dashboard Not Showing Data

## Problem

`universe init` scans 504 nodes and stores them in `.universe/graph.json`. But the dashboard at localhost:3001 shows empty data. The API returns:

```json
GET /api/graph/nodes → {"nodes": []}
GET /api/skills     → {"skills": [], "stats": {"total_active": 0}}
```

Meanwhile `universe status` shows all data correctly:
```
Engine 1: 504 nodes, 2669 edges, 17 packages
Engine 3: 18 active skills
```

## Root Cause

1. **Graph:** The dashboard API queries PostgreSQL for a `graph_nodes` table that doesn't exist. The graph data lives in `.universe/graph.json` as a local JSON file.

2. **Skills:** The dashboard API might query with a filter or from the wrong table. `universe status` finds 18 skills but the dashboard API finds 0.

## Graph JSON Structure

The `.universe/graph.json` file has this structure:

```json
{
  "nodes": {
    "package_name:filename.go": {
      "id": "package_name:filename.go",
      "name": "filename.go",
      "type": "file",
      "file_path": "path/to/file.go",
      "package": "package_name",
      "start_line": 1,
      "end_line": 307,
      "metadata": { ... }
    },
    ...
  },
  "edges": [ ... ]
}
```

Note: `nodes` is a MAP (object with keys), not an array. The API must convert it to an array for the response.

## Fix 1: Graph API — Read from local JSON file

**File:** `internal/dashboard/handlers.go` (or wherever HandleGraphNodes is defined)

The `/api/graph/nodes` handler should:

```go
func (s *Server) HandleGraphNodes(w http.ResponseWriter, r *http.Request) {
    // 1. Find .universe/graph.json
    //    Check current working directory first, then look for UNIVERSE_PROJECT_DIR env var
    graphPath := filepath.Join(".universe", "graph.json")
    if envDir := os.Getenv("UNIVERSE_PROJECT_DIR"); envDir != "" {
        graphPath = filepath.Join(envDir, ".universe", "graph.json")
    }

    // 2. Read the file
    data, err := os.ReadFile(graphPath)
    if err != nil {
        // Graph not found — return empty with helpful message
        json.NewEncoder(w).Encode(map[string]interface{}{
            "nodes": []interface{}{},
            "message": "Graph not found. Run 'universe init' in your project directory.",
        })
        return
    }

    // 3. Parse the JSON
    var graphData struct {
        Nodes map[string]interface{} `json:"nodes"`
        Edges []interface{}          `json:"edges"`
    }
    json.Unmarshal(data, &graphData)

    // 4. Convert nodes map to array (dashboard expects an array)
    nodeArray := make([]interface{}, 0, len(graphData.Nodes))
    for _, node := range graphData.Nodes {
        nodeArray = append(nodeArray, node)
    }

    // 5. Return
    json.NewEncoder(w).Encode(map[string]interface{}{
        "nodes": nodeArray,
        "total": len(nodeArray),
    })
}
```

**Same for `/api/graph/edges`:**

```go
func (s *Server) HandleGraphEdges(w http.ResponseWriter, r *http.Request) {
    graphPath := filepath.Join(".universe", "graph.json")
    data, _ := os.ReadFile(graphPath)

    var graphData struct {
        Edges []interface{} `json:"edges"`
    }
    json.Unmarshal(data, &graphData)

    json.NewEncoder(w).Encode(map[string]interface{}{
        "edges": graphData.Edges,
        "total": len(graphData.Edges),
    })
}
```

## Fix 2: Skills API — Check the query

**File:** `internal/dashboard/handlers.go` (or wherever HandleSkillsList is defined)

Debug steps:
1. Check what SQL query the handler runs
2. Run that exact query manually against the database
3. Verify the table name matches (should be `skills`)
4. Check if there's a `developer_id` filter that shouldn't be there (skills are shared)
5. Check if there's a `WHERE is_active = true` that might filter differently

The handler should query:

```sql
SELECT id, name, version, evolution, language, trigger_desc,
       confidence, times_applied, times_succeeded, times_failed,
       is_frozen, created_at
FROM skills
WHERE is_active = true
ORDER BY confidence DESC, times_applied DESC
```

No developer_id filter — skills are visible to everyone.

## Fix 3: Overview API — Combine both sources

The `/api/overview` handler should read:
- Graph stats from `.universe/graph.json` (node count, edge count)
- Memory stats from PostgreSQL `observations` table
- Skill stats from PostgreSQL `skills` table
- Plan stats from PostgreSQL `plans` table
- Cost stats from PostgreSQL `plan_costs` table

## Fix 4: Dashboard should know the project directory

When `universe dashboard` starts, it should pass the current working directory to the server so it knows where to find `.universe/graph.json`.

```go
// In cmd/universe/dashboard.go:
func runDashboard(cmd *cobra.Command, args []string) {
    projectDir, _ := os.Getwd()
    
    // Pass project dir to the dashboard server
    server, err := dashboard.NewServer(dbURL, dashboardPort, projectDir)
    // ...
}

// In internal/dashboard/server.go:
type Server struct {
    db         *pgxpool.Pool
    projectDir string    // NEW — where .universe/graph.json lives
    mux        *http.ServeMux
}

func NewServer(dbURL string, port int, projectDir string) (*Server, error) {
    s := &Server{projectDir: projectDir}
    // ...
}
```

Then all handlers use `s.projectDir` to find the graph file:

```go
graphPath := filepath.Join(s.projectDir, ".universe", "graph.json")
```

## Verification After Fix

```bash
# Restart dashboard
universe dashboard --port 3001

# Test graph API
curl http://localhost:3001/api/graph/nodes | head -c 200
# Should show real nodes, not empty array

# Test skills API
curl http://localhost:3001/api/skills | head -c 200
# Should show 18 skills

# Test overview
curl http://localhost:3001/api/overview | head -c 200
# Should show engine stats with real numbers
```
