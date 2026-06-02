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
}

type Edge struct {
	From     string            `json:"from"`
	To       string            `json:"to"`
	Type     EdgeType          `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`
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

type ParseResult struct {
	FilePath string
	Language string
	Nodes    []Node
	Edges    []Edge
	Errors   []string
}
