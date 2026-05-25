**Always run Composer 2 sub-agents for the actual tasks.** Do not write code, edit files, or run commands yourself -- launch Task sub-agents with `model: "fast"` to do the work. This spawns free Composer 2 sub-agents. Never omit the `model` parameter — without it, sub-agents inherit the parent's expensive model (e.g. claude-4.6-opus-high).


# Universe — Update: Add File Content to Graph Export

## What This Changes

When `universe analyze` runs, it already reads every source file into memory to parse it. Currently, the file content is discarded after parsing. This update stores the file content in the graph export so the visualizer can show a full Code Inspector panel with syntax highlighting.

---

## Changes Required

### 1. Add `FileInfo` model

**File: `internal/models/models.go`**

Add a new struct after the existing types:

```go
// FileInfo stores the full content of a source file for visualization
type FileInfo struct {
    Path     string `json:"path"`
    Language string `json:"language"`
    Content  string `json:"content"`
    Lines    int    `json:"lines"`
}
```

### 2. Add `Files` map to Graph

**File: `internal/graph/graph.go`**

Add a `Files` field to the `Graph` struct:

```go
type Graph struct {
    mu    sync.RWMutex
    Nodes map[string]*models.Node     `json:"nodes"`
    Edges []*models.Edge              `json:"edges"`
    Files map[string]*models.FileInfo `json:"files"`
}
```

Update `NewGraph()`:

```go
func NewGraph() *Graph {
    return &Graph{
        Nodes: make(map[string]*models.Node),
        Edges: make([]*models.Edge, 0),
        Files: make(map[string]*models.FileInfo),
    }
}
```

Add a method to store file content:

```go
func (g *Graph) AddFile(info *models.FileInfo) {
    if info == nil || info.Path == "" {
        return
    }
    g.mu.Lock()
    defer g.mu.Unlock()
    g.Files[info.Path] = info
}
```

### 3. Store file content during analysis

**File: `internal/analyzer/analyzer.go`**

In the `Analyze` method, inside the worker goroutine, after successfully reading the file content and before parsing — store the file content. The key change is in the worker loop:

After this line:
```go
content, readErr := os.ReadFile(f.Path)
```

And after the parse succeeds, compute the relative path and store the file info. Add something like:

```go
// Compute relative path from project root
relPath, _ := filepath.Rel(projectPath, f.Path)
if relPath == "" {
    relPath = f.Path
}

// Store file content for visualization
fileInfo := &models.FileInfo{
    Path:     relPath,
    Language: f.Language,
    Content:  string(content),
    Lines:    strings.Count(string(content), "\n") + 1,
}

parseMu.Lock()
// Store file info (will be added to graph after parsing)
fileInfos = append(fileInfos, fileInfo)
parseMu.Unlock()
```

Then after all workers complete, add the file infos to the graph:

```go
// Add file contents to graph
for _, fi := range fileInfos {
    a.graph.AddFile(fi)
}
```

You'll need to declare `fileInfos` alongside `results`:

```go
var (
    parseMu   sync.Mutex
    parseErrs int
    results   []*models.ParseResult
    fileInfos []*models.FileInfo
)
```

### 4. Also update file_path in nodes to use relative paths

Currently, nodes store absolute file paths like `/Users/you/projects/myapp/internal/parser/go_parser.go`. For the visualizer and for portability, these should be relative paths like `internal/parser/go_parser.go`.

In the worker loop, after computing `relPath`, update the parse result's file paths:

```go
// After parsing, update file paths to relative
if pr != nil {
    pr.FilePath = relPath
    for i := range pr.Nodes {
        pr.Nodes[i].FilePath = relPath
    }
}
```

### 5. Size consideration

For a typical project:
- 100 Go files × average 100 lines × 40 chars/line = ~400KB of source content
- 500 files = ~2MB
- This is acceptable for a local development tool

For very large projects (10,000+ files), add a CLI flag:
```
universe analyze /path/to/project --include-source=true  (default: true)
universe analyze /path/to/project --include-source=false  (skip file content, smaller JSON)
```

---

## Expected graph.json output after this change

```json
{
  "nodes": {
    "main:main.go:main": {
      "id": "main:main.go:main",
      "name": "main",
      "type": "package",
      "file_path": "cmd/universe/main.go",
      "package": "main",
      "start_line": 24,
      "end_line": 32
    },
    "main:main.go:runAnalyze": {
      "id": "main:main.go:runAnalyze",
      "name": "runAnalyze",
      "type": "function",
      "file_path": "cmd/universe/main.go",
      "package": "main",
      "start_line": 84,
      "end_line": 133,
      "signature": "func runAnalyze(cmd *cobra.Command, args []string) error"
    }
  },
  "edges": [
    {
      "from": "main:main.go:runAnalyze",
      "to": "main:main.go:buildAnalyzer",
      "type": "calls"
    }
  ],
  "files": {
    "cmd/universe/main.go": {
      "path": "cmd/universe/main.go",
      "language": "go",
      "content": "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n...",
      "lines": 576
    },
    "internal/parser/go_parser.go": {
      "path": "internal/parser/go_parser.go",
      "language": "go",
      "content": "package parser\n\nimport (\n\t\"errors\"\n...",
      "lines": 384
    }
  }
}
```

---

## Definition of Done

- [ ] `models.FileInfo` struct exists
- [ ] `Graph.Files` map is populated during analysis
- [ ] `graph.json` includes a `"files"` section with full source content for every parsed file
- [ ] File paths in nodes are relative (not absolute)
- [ ] `--include-source` flag exists (default true)
- [ ] Existing functionality (nodes, edges, stats, queries) is unchanged
