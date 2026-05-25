# Universe — Code Knowledge Graph Engine

## Project Overview

Universe is a Go CLI tool that analyzes codebases and builds a knowledge graph mapping every package, function, import, type, interface, and dependency. You point it at a project path, it scans every source file, parses them using tree-sitter, extracts the code structure, and stores it as a queryable graph.

The end goal is cross-repo dependency detection — when code changes in one repo, Universe can tell you what breaks in other repos. But this phase focuses on: **build the graph engine for a single repo first**.

```bash
# Usage
universe analyze /path/to/project
universe query "what depends on package auth"
universe graph --output graph.json
```

---

## Tech Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Language | Go 1.22+ | Performance, single binary, great concurrency |
| AST Parsing | tree-sitter via `github.com/smacker/go-tree-sitter` | Fast incremental parsing, multi-language support |
| Go Grammar | `github.com/smacker/go-tree-sitter/golang` | tree-sitter grammar for Go files |
| Python Grammar | `github.com/smacker/go-tree-sitter/python` | tree-sitter grammar for Python files |
| Graph Storage | In-memory graph (phase 1), KuzuDB or PostgreSQL (phase 2) | Start simple, migrate later |
| CLI Framework | `github.com/spf13/cobra` | Standard Go CLI framework |
| Output | JSON + terminal pretty-print | Machine-readable + human-readable |

---

## Project Structure

```
universe/
├── cmd/
│   └── universe/
│       └── main.go                  # CLI entry point using cobra
│
├── internal/
│   ├── parser/
│   │   ├── parser.go                # Parser interface (all languages implement this)
│   │   ├── registry.go              # Parser registry — maps file extensions to parsers
│   │   ├── go_parser.go             # Go language parser using tree-sitter
│   │   └── python_parser.go         # Python language parser using tree-sitter
│   │
│   ├── extractor/
│   │   ├── extractor.go             # Extractor interface
│   │   ├── go_extractor.go          # Go-specific: extract functions, imports, types, interfaces
│   │   └── python_extractor.go      # Python-specific: extract functions, imports, classes
│   │
│   ├── graph/
│   │   ├── graph.go                 # Graph data structure (nodes + edges)
│   │   ├── store.go                 # Graph storage interface
│   │   └── memory_store.go          # In-memory graph store (phase 1)
│   │
│   ├── scanner/
│   │   └── scanner.go               # Directory walker — finds source files, routes to parser
│   │
│   ├── analyzer/
│   │   └── analyzer.go              # Orchestrator — ties scanner + parser + extractor + graph
│   │
│   └── models/
│       └── models.go                # Shared types: Node, Edge, ParseResult, etc.
│
├── go.mod
├── go.sum
└── README.md
```

---

## Phase 1: Core Data Models

### File: `internal/models/models.go`

Define all shared types used across the project.

```go
package models

type NodeType string

const (
    NodePackage   NodeType = "package"
    NodeFile      NodeType = "file"
    NodeFunction  NodeType = "function"
    NodeMethod    NodeType = "method"
    NodeStruct    NodeType = "struct"
    NodeInterface NodeType = "interface"
    NodeType_     NodeType = "type"
    NodeVariable  NodeType = "variable"
    NodeImport    NodeType = "import"
    NodeClass     NodeType = "class"    // Python
    NodeModule    NodeType = "module"   // Python
)

type EdgeType string

const (
    EdgeImports    EdgeType = "imports"
    EdgeCalls      EdgeType = "calls"
    EdgeImplements EdgeType = "implements"
    EdgeDependsOn  EdgeType = "depends_on"
    EdgeContains   EdgeType = "contains"    // package contains function
    EdgeReturns    EdgeType = "returns"
    EdgeReceives   EdgeType = "receives"    // method receiver
    EdgeInherits   EdgeType = "inherits"    // Python class inheritance
)

// Node represents a code entity in the knowledge graph
type Node struct {
    ID        string            `json:"id"`         // unique: "repo:file:name" or "repo:package"
    Name      string            `json:"name"`
    Type      NodeType          `json:"type"`
    FilePath  string            `json:"file_path"`
    Package   string            `json:"package"`
    StartLine int               `json:"start_line"`
    EndLine   int               `json:"end_line"`
    Signature string            `json:"signature,omitempty"`  // function signature
    Metadata  map[string]string `json:"metadata,omitempty"`   // extra info (exported, params, return type)
}

// Edge represents a relationship between two nodes
type Edge struct {
    From     string            `json:"from"`      // source node ID
    To       string            `json:"to"`        // target node ID
    Type     EdgeType          `json:"type"`
    Metadata map[string]string `json:"metadata,omitempty"`
}

// ParseResult is what every parser returns
type ParseResult struct {
    FilePath string
    Language string
    Nodes    []Node
    Edges    []Edge
    Errors   []string
}
```

---

## Phase 2: Parser Interface & Registry

### File: `internal/parser/parser.go`

```go
package parser

import "github.com/yourcompany/universe/internal/models"

// Parser interface — every language parser implements this
type Parser interface {
    // Parse takes a file path and its content, returns extracted nodes and edges
    Parse(filePath string, content []byte) (*models.ParseResult, error)

    // SupportedExtensions returns file extensions this parser handles (e.g., [".go"])
    SupportedExtensions() []string

    // Language returns the language name (e.g., "go", "python")
    Language() string
}
```

### File: `internal/parser/registry.go`

The registry maps file extensions to parsers. When the scanner finds a `.go` file, it asks the registry for the Go parser. When it finds `.py`, it gets the Python parser.

```go
package parser

// Registry holds all registered parsers, keyed by file extension
type Registry struct {
    parsers map[string]Parser  // ".go" -> GoParser, ".py" -> PythonParser
}

func NewRegistry() *Registry { ... }

// Register adds a parser for its supported extensions
func (r *Registry) Register(p Parser) { ... }

// GetParser returns the parser for a given file extension, or nil if unsupported
func (r *Registry) GetParser(extension string) Parser { ... }

// SupportedExtensions returns all extensions the registry can handle
func (r *Registry) SupportedExtensions() []string { ... }
```

---

## Phase 3: Go Parser (tree-sitter)

### File: `internal/parser/go_parser.go`

Uses tree-sitter with the Go grammar to parse `.go` files into an AST, then walks the tree to extract code entities.

**Dependencies:**
```
go get github.com/smacker/go-tree-sitter
```

Note: `github.com/smacker/go-tree-sitter` includes Go grammar built-in at `github.com/smacker/go-tree-sitter/golang`.

**What to extract from Go files:**

1. **Package declaration** — `package main` → creates a Package node
2. **Import statements** — `import "fmt"` or `import "github.com/company/auth"` → creates Import nodes + "imports" edges
3. **Function declarations** — `func HandleLogin(w http.ResponseWriter, r *http.Request) error` → creates Function node with signature, parameters, return types
4. **Method declarations** — `func (s *Server) Start()` → creates Method node with receiver info
5. **Struct declarations** — `type User struct { ... }` → creates Struct node with fields
6. **Interface declarations** — `type AuthService interface { ... }` → creates Interface node with methods
7. **Type declarations** — `type UserID string` → creates Type node
8. **Function calls** — `auth.ValidateToken(token)` → creates "calls" edge

**Tree-sitter Go node types to look for:**
- `package_clause` → package name
- `import_declaration` → imports (may contain `import_spec_list`)
- `import_spec` → individual import path
- `function_declaration` → top-level functions
- `method_declaration` → methods with receivers
- `type_declaration` → contains `type_spec`
- `type_spec` → struct, interface, or type alias
- `struct_type` → struct with field declarations
- `interface_type` → interface with method specs
- `call_expression` → function/method calls

**Implementation approach:**
1. Create a tree-sitter parser with Go language
2. Parse the file content into a tree
3. Walk the root node's children
4. For each node, check its type and extract relevant information
5. Build Node and Edge objects
6. Return a ParseResult

**Helper functions needed:**
- `getNodeText(node *sitter.Node, source []byte) string` — extract the source text for a node
- `extractFunctionSignature(node *sitter.Node, source []byte) string` — build a human-readable signature
- `extractImportPath(node *sitter.Node, source []byte) string` — get the import path string
- `findChildren(node *sitter.Node, nodeType string) []*sitter.Node` — find all children of a given type
- `walkTree(node *sitter.Node, callback func(*sitter.Node))` — recursive tree walker

---

## Phase 4: Python Parser (tree-sitter)

### File: `internal/parser/python_parser.go`

Same pattern as Go parser but for Python. Uses `github.com/smacker/go-tree-sitter/python`.

**What to extract from Python files:**

1. **Import statements** — `import os`, `from flask import Flask`, `from ..models import User`
2. **Function definitions** — `def validate_token(token: str) -> bool:`
3. **Class definitions** — `class UserService:` with methods and inheritance
4. **Class inheritance** — `class Admin(User):` → "inherits" edge
5. **Method definitions** — functions inside classes
6. **Decorators** — `@app.route("/login")` (store as metadata)
7. **Function calls** — `auth_service.validate(token)` → "calls" edge
8. **Global variables / constants** — `MAX_RETRIES = 3`

**Tree-sitter Python node types:**
- `import_statement` → `import x`
- `import_from_statement` → `from x import y`
- `function_definition` → functions
- `class_definition` → classes (check for `argument_list` in superclass)
- `decorated_definition` → decorated functions/classes
- `call` → function calls
- `assignment` → variable assignments (top-level = global/constant)

---

## Phase 5: Extractor Layer

### File: `internal/extractor/extractor.go`

The extractor adds **semantic understanding** on top of the raw parse. The parser gives you syntax — the extractor gives you meaning.

```go
package extractor

import "github.com/yourcompany/universe/internal/models"

// Extractor adds semantic analysis on top of parsed results
type Extractor interface {
    // Extract takes raw parse results and enriches them with semantic edges
    // For example: detecting which function calls map to which package imports
    Extract(result *models.ParseResult, allResults []*models.ParseResult) (*models.ParseResult, error)

    // Language returns which language this extractor handles
    Language() string
}
```

**What extractors do beyond parsing:**

1. **Resolve function calls to imports** — if a Go file imports `"github.com/company/auth"` and calls `auth.ValidateToken()`, the extractor creates an edge from the calling function to the `ValidateToken` function in the auth package.

2. **Detect exported vs unexported** — in Go, uppercase = exported. The extractor marks this in metadata. This matters for cross-repo dependencies — only exported symbols can be used by other repos.

3. **Build package-level dependency graph** — aggregate file-level imports into package-level "depends_on" edges.

4. **Detect interface implementations** — if a struct has methods matching an interface's method set, create an "implements" edge.

---

## Phase 6: Graph Storage

### File: `internal/graph/graph.go`

```go
package graph

import "github.com/yourcompany/universe/internal/models"

// Graph is the knowledge graph that stores all code entities and relationships
type Graph struct {
    Nodes map[string]*models.Node  // keyed by node ID
    Edges []*models.Edge
}

func NewGraph() *Graph { ... }

// AddNode adds a node to the graph (upserts by ID)
func (g *Graph) AddNode(node *models.Node) { ... }

// AddEdge adds an edge to the graph
func (g *Graph) AddEdge(edge *models.Edge) { ... }

// GetNode returns a node by ID
func (g *Graph) GetNode(id string) *models.Node { ... }

// GetDependents returns all nodes that depend on the given node
// This is the KEY query: "what breaks if I change this?"
func (g *Graph) GetDependents(nodeID string) []*models.Node { ... }

// GetDependencies returns all nodes that the given node depends on
func (g *Graph) GetDependencies(nodeID string) []*models.Node { ... }

// GetByType returns all nodes of a given type
func (g *Graph) GetByType(nodeType models.NodeType) []*models.Node { ... }

// GetEdgesFrom returns all edges originating from a node
func (g *Graph) GetEdgesFrom(nodeID string) []*models.Edge { ... }

// GetEdgesTo returns all edges pointing to a node
func (g *Graph) GetEdgesTo(nodeID string) []*models.Edge { ... }

// Search finds nodes by name (partial match)
func (g *Graph) Search(query string) []*models.Node { ... }

// ExportJSON exports the entire graph as JSON
func (g *Graph) ExportJSON(filePath string) error { ... }

// Stats returns graph statistics
func (g *Graph) Stats() GraphStats { ... }

type GraphStats struct {
    TotalNodes      int            `json:"total_nodes"`
    TotalEdges      int            `json:"total_edges"`
    NodesByType     map[string]int `json:"nodes_by_type"`
    EdgesByType     map[string]int `json:"edges_by_type"`
    PackageCount    int            `json:"package_count"`
    FileCount       int            `json:"file_count"`
}
```

---

## Phase 7: Directory Scanner

### File: `internal/scanner/scanner.go`

Walks a project directory, finds all source files, skips irrelevant files, and routes each file to the correct parser.

```go
package scanner

// Scanner walks a directory tree and finds all parseable source files
type Scanner struct {
    registry *parser.Registry
}

// Scan walks the directory and returns all parseable file paths grouped by language
func (s *Scanner) Scan(rootPath string) ([]ScannedFile, error) { ... }

type ScannedFile struct {
    Path      string
    Extension string
    Language  string
}
```

**Important: Skip these directories/files:**
- `.git/`
- `vendor/` (Go deps)
- `node_modules/`
- `__pycache__/`
- `.venv/`, `venv/`, `env/`
- `dist/`, `build/`, `target/`
- `.terraform/`
- Any hidden directories (starting with `.`)
- Binary files
- Generated files (e.g., `*.pb.go`, `*_generated.go`)
- Test files (parse them but mark as `is_test: true` in metadata)

---

## Phase 8: Analyzer (Orchestrator)

### File: `internal/analyzer/analyzer.go`

Ties everything together. This is the main engine.

```go
package analyzer

// Analyzer orchestrates the full pipeline: scan → parse → extract → graph
type Analyzer struct {
    scanner   *scanner.Scanner
    registry  *parser.Registry
    graph     *graph.Graph
}

// Analyze runs the full pipeline on a project path
func (a *Analyzer) Analyze(projectPath string) (*graph.Graph, error) {
    // 1. Scan directory for source files
    // 2. For each file:
    //    a. Read file content
    //    b. Get parser from registry by file extension
    //    c. Parse file → get ParseResult
    //    d. Add nodes and edges to graph
    // 3. Run extractors for semantic analysis
    // 4. Return the completed graph
}
```

---

## Phase 9: CLI (Cobra)

### File: `cmd/universe/main.go`

```go
// Commands:
//
// universe analyze <path>          — analyze a project and build the graph
//   --output, -o <file>            — save graph to JSON file
//   --format <json|text>           — output format (default: text)
//   --verbose, -v                  — show detailed parsing info
//
// universe query <query>           — query the graph
//   --depends-on <name>            — find what depends on a symbol
//   --dependencies <name>          — find what a symbol depends on
//   --type <type>                  — filter by node type
//
// universe stats                   — show graph statistics
//
// universe graph                   — export full graph
//   --output, -o <file>            — output file (default: stdout)
//   --format <json|dot>            — output format
```

---

## Build & Run

```bash
# Build
go build -o universe ./cmd/universe/

# Analyze a Go project
./universe analyze /path/to/your/go/project

# Analyze with JSON output
./universe analyze /path/to/project -o graph.json

# Query: what depends on the "auth" package?
./universe query --depends-on auth

# Show stats
./universe stats
```

---

## Expected Output Example

When you run `universe analyze /path/to/project`, the output should look something like:

```
🔍 Scanning /path/to/project...
   Found 47 Go files across 8 packages
   Found 12 Python files across 3 modules

📊 Building knowledge graph...
   ✓ Parsed 47 Go files (234 functions, 18 structs, 5 interfaces)
   ✓ Parsed 12 Python files (67 functions, 9 classes)
   ✓ Extracted 312 dependency edges
   ✓ Extracted 89 call edges

🧠 Graph Summary:
   Nodes: 423
   Edges: 401
   Packages: 11
   Top-level functions: 301
   Structs/Classes: 27
   Interfaces: 5

📦 Package Dependencies:
   api → auth, models, database
   auth → models, crypto
   database → models
   handlers → api, auth, models
```

---

## Important Implementation Notes

1. **CGo is required** — tree-sitter is a C library. Make sure `CGO_ENABLED=1` and a C compiler is available.

2. **Parse files concurrently** — use goroutines + a worker pool to parse multiple files in parallel. This is where Go shines. Use a `sync.WaitGroup` or channel-based worker pool with a configurable concurrency limit (default: `runtime.NumCPU()`).

3. **Node IDs must be globally unique** — use the format `"<package>:<file>:<name>"` so that when we later add multi-repo support, we can prefix with repo name: `"<repo>:<package>:<file>:<name>"`.

4. **Handle parse errors gracefully** — if one file fails to parse, log the error and continue. Don't abort the entire analysis.

5. **Test files matter** — parse `_test.go` and `test_*.py` files but mark them with `metadata["is_test"] = "true"`. These create "tests" edges that are valuable for the testing pipeline later.

6. **Exported symbols** — for Go, mark functions/types starting with uppercase as `metadata["exported"] = "true"`. For Python, mark symbols not starting with `_` as exported. Only exported symbols can be dependencies for other repos.

7. **Memory** — for a typical project (100-500 files), in-memory graph is fine. Don't over-engineer storage yet.

---

## Future Phases (Not in This Build)

These are coming next but DO NOT build them now. Just design the interfaces so they can be added later:

- **Phase 10:** Multi-repo support — analyze multiple repos and detect cross-repo dependencies
- **Phase 11:** Webhook listener — receive Git push events and incrementally update the graph
- **Phase 12:** Terraform/Ansible/YAML parsers — infrastructure-as-code support
- **Phase 13:** REST API server — serve the graph over HTTP for the Universe dashboard
- **Phase 14:** AI test agent integration — use the graph to determine blast radius and trigger tests

---

## Definition of Done

The first version is complete when:

- [ ] `universe analyze /path/to/go/project` correctly identifies all packages, functions, structs, interfaces, and imports
- [ ] `universe analyze /path/to/python/project` correctly identifies all modules, functions, classes, and imports
- [ ] Function calls are detected and mapped to their target functions
- [ ] The graph can answer: "what depends on function X?" and "what does package Y depend on?"
- [ ] Output is clean and readable in both terminal and JSON formats
- [ ] Parsing errors don't crash the tool — they're logged and skipped
- [ ] Analysis of a 100-file project completes in under 5 seconds
