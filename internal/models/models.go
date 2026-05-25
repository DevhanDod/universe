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

type ParseResult struct {
	FilePath string
	Language string
	Nodes    []Node
	Edges    []Edge
	Errors   []string
}
