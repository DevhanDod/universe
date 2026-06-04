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
	NodeClass     NodeType = "class"
	NodeModule    NodeType = "module"
)

type EdgeType string

const (
	EdgeImports    EdgeType = "imports"
	EdgeCalls      EdgeType = "calls"
	EdgeImplements EdgeType = "implements"
	EdgeDependsOn  EdgeType = "depends_on"
	EdgeContains   EdgeType = "contains"
	EdgeReturns    EdgeType = "returns"
	EdgeReceives   EdgeType = "receives"
	EdgeInherits   EdgeType = "inherits"
	EdgeIgnores    EdgeType = "ignores"
)

// File classification — set on a NodeFile's Metadata["kind"] so the agent can
// filter the graph (source vs config vs doc vs asset) without re-classifying paths.
const (
	FileKindSource   = "source"   // has a structural parser (go, py, …)
	FileKindConfig   = "config"   // yaml/toml/ini/dockerfile/makefile/etc.
	FileKindDoc      = "doc"      // md, rst, txt
	FileKindLockfile = "lockfile" // package-lock.json, go.sum, poetry.lock
	FileKindImage    = "image"    // png/jpg/svg/…
	FileKindBinary   = "binary"   // any other non-text blob
	FileKindOther    = "other"    // text we don't have a category for
)

type Node struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      NodeType          `json:"type"`
	FilePath  string            `json:"file_path"`
	Package   string            `json:"package"`
	StartLine int               `json:"start_line"`
	EndLine   int               `json:"end_line"`
	Signature string            `json:"signature,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`

	// Precomputed at index time so MCP responses can answer in one call
	// instead of forcing the agent to explore via repeated tool calls.
	Cluster     string   `json:"cluster,omitempty"`
	Flows       []string `json:"flows,omitempty"`
	CallerCount int      `json:"caller_count,omitempty"`
	CalleeCount int      `json:"callee_count,omitempty"`
}

type Edge struct {
	From     string            `json:"from"`
	To       string            `json:"to"`
	Type     EdgeType          `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// Confidence score for this relationship.
	//   1.0  — same file, AST-extracted
	//   0.9  — cross-file within same package
	//   0.8  — cross-package, resolved via import
	//   0.6  — cross-package, name-based match
	//   0.5  — external / unresolved target
	//   0.0  — not set; readers MUST treat as 1.0 for backward
	//          compatibility with graphs built before v0.2.8.
	Confidence float64 `json:"confidence,omitempty"`
}

// FileInfo stores the full content of a source file for visualization
type FileInfo struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Content  string `json:"content"`
	Lines    int    `json:"lines"`
}

// Coverage summarises how much of the scanned codebase the graph actually
// understands. FileCoverage alone is misleading (one huge unparsed file equals
// a tiny .gitignore) so ByteCoverage is the honest number; the per-kind
// Breakdown tells you where to invest next.
type Coverage struct {
	TotalFiles   int                        `json:"total_files"`
	ParsedFiles  int                        `json:"parsed_files"`
	TotalBytes   int64                      `json:"total_bytes"`
	ParsedBytes  int64                      `json:"parsed_bytes"`
	FileCoverage float64                    `json:"file_coverage"` // 0..1
	ByteCoverage float64                    `json:"byte_coverage"` // 0..1
	Breakdown    map[string]CoverageBucket  `json:"breakdown"`     // keyed by FileKind*
}

type CoverageBucket struct {
	Files       int   `json:"files"`
	FilesParsed int   `json:"files_parsed"`
	Bytes       int64 `json:"bytes"`
	BytesParsed int64 `json:"bytes_parsed"`
}

// Cluster groups related nodes into a functional community (auth, db, tests, …).
// Computed during `universe init` by label propagation over the call graph.
type Cluster struct {
	Name        string   `json:"name"`
	NodeCount   int      `json:"node_count"`
	NodeIDs     []string `json:"node_ids,omitempty"`
	KeyFiles    []string `json:"key_files,omitempty"`
	EntryPoints []string `json:"entry_points,omitempty"`
}

// FlowStep is one node visited along an execution flow.
type FlowStep struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Cluster string `json:"cluster,omitempty"`
	StepNum int    `json:"step_num"`
}

// Flow is an execution path traced from an entry point (main, handler, test, …).
type Flow struct {
	Name       string     `json:"name"`
	EntryPoint string     `json:"entry_point"`
	Steps      []FlowStep `json:"steps"`
	StepCount  int        `json:"step_count"`
	CrossRepo  bool       `json:"cross_repo,omitempty"`
	Clusters   []string   `json:"clusters,omitempty"`
}

// Impact is one node affected by a change to the root node.
type Impact struct {
	NodeID     string  `json:"node_id"`
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Confidence float64 `json:"confidence"`
	Relation   string  `json:"relation,omitempty"`
}

// ImpactSummary is the precomputed blast radius for one node.
type ImpactSummary struct {
	NodeID           string             `json:"node_id"`
	NodeName         string             `json:"node_name"`
	TotalAffected    int                `json:"total_affected"`
	CrossRepo        bool               `json:"cross_repo,omitempty"`
	RiskLevel        string             `json:"risk_level"`
	ByDepth          map[int][]Impact   `json:"by_depth"`
	AffectedFlows    []string           `json:"affected_flows,omitempty"`
	AffectedClusters []string           `json:"affected_clusters,omitempty"`
	Summary          string             `json:"summary"`
}

type ParseResult struct {
	FilePath string
	Language string
	Nodes    []Node
	Edges    []Edge
	Errors   []string
}
