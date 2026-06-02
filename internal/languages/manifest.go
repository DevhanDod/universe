// Package languages is the single source of truth for what universe can do
// per language. The analyzer uses it to classify scanned files, the CLI
// renders it as `universe languages`, and the analyze summary uses it to
// warn the user about unsupported file types in their project.
//
// To add or upgrade a language: edit Catalog. Don't sprinkle language
// awareness anywhere else.
package languages

import (
	"path/filepath"
	"strings"
)

// Support is how deeply universe understands a language. Be honest in entries:
// promoting a language above its real support level is worse than admitting
// "inventory only" — agents and users plan around what's claimed.
type Support string

const (
	// SupportDeep: full structural parser with call graph, type relationships,
	// scope-aware imports. Edges include calls, depends_on, returns, receives.
	SupportDeep Support = "deep"

	// SupportStructural: functions, classes, imports, top-level symbols. No
	// call graph. Tree-sitter-backed mapper with a generic config.
	SupportStructural Support = "structural"

	// SupportSymbolic: regex/line-based extraction of named symbols. No AST,
	// no scope. Right tier for shell/terraform/dockerfile/etc.
	SupportSymbolic Support = "symbolic"

	// SupportInventory: file node only — universe knows the file exists, has
	// size/kind/ignored metadata, and the agent can read its bytes. No parsing.
	// This is the floor every file gets.
	SupportInventory Support = "inventory"
)

// Language describes one language entry in the catalog.
type Language struct {
	// Name is the human-facing label (e.g. "JavaScript").
	Name string
	// Extensions are lowercase, with leading dot. First entry is canonical.
	Extensions []string
	// Filenames matches by basename (lowercase) for files without an
	// informative extension (Dockerfile, Makefile, .gitignore).
	Filenames []string
	// Support is the *current* support level for this language. When you ship
	// a parser, flip this field — that's the public commitment.
	Support Support
	// Parser is the implementation tag (e.g. "go-ast", "tree-sitter-python",
	// "regex-shell"). Empty for inventory-only entries.
	Parser string
}

// Catalog is the manifest. Order it by Support tier then alphabetical so the
// `universe languages` output reads top-down from "best to worst".
var Catalog = []Language{
	// ── deep ───────────────────────────────────────────────────────────────
	{Name: "Go", Extensions: []string{".go"}, Support: SupportDeep, Parser: "go-ast"},
	{Name: "Python", Extensions: []string{".py"}, Support: SupportDeep, Parser: "tree-sitter-python"},

	// ── structural (planned, not yet shipped) ──────────────────────────────
	{Name: "JavaScript", Extensions: []string{".js", ".jsx", ".mjs", ".cjs"}, Support: SupportInventory},
	{Name: "TypeScript", Extensions: []string{".ts", ".tsx"}, Support: SupportInventory},
	{Name: "Java", Extensions: []string{".java"}, Support: SupportInventory},
	{Name: "HTML", Extensions: []string{".html", ".htm"}, Support: SupportInventory},
	{Name: "CSS", Extensions: []string{".css", ".scss", ".sass", ".less"}, Support: SupportInventory},
	{Name: "Vue", Extensions: []string{".vue"}, Support: SupportInventory},
	{Name: "Svelte", Extensions: []string{".svelte"}, Support: SupportInventory},
	{Name: "Rust", Extensions: []string{".rs"}, Support: SupportInventory},
	{Name: "C#", Extensions: []string{".cs"}, Support: SupportInventory},
	{Name: "C++", Extensions: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh"}, Support: SupportInventory},
	{Name: "C", Extensions: []string{".c", ".h"}, Support: SupportInventory},
	{Name: "Ruby", Extensions: []string{".rb"}, Support: SupportInventory},
	{Name: "PHP", Extensions: []string{".php"}, Support: SupportInventory},
	{Name: "Kotlin", Extensions: []string{".kt", ".kts"}, Support: SupportInventory},
	{Name: "Swift", Extensions: []string{".swift"}, Support: SupportInventory},

	// ── symbolic (planned, not yet shipped) ────────────────────────────────
	{Name: "Shell", Extensions: []string{".sh", ".bash", ".zsh"}, Support: SupportInventory},
	{Name: "PowerShell", Extensions: []string{".ps1", ".psm1"}, Support: SupportInventory},
	{Name: "Terraform", Extensions: []string{".tf", ".tfvars"}, Support: SupportInventory},
	{Name: "Ansible", Extensions: []string{".yml", ".yaml"}, Support: SupportInventory}, // ambiguous with generic YAML — TODO: detect ansible by content
	{Name: "Dockerfile", Filenames: []string{"dockerfile"}, Support: SupportInventory},
	{Name: "Makefile", Filenames: []string{"makefile", "gnumakefile"}, Support: SupportInventory},
	{Name: "SQL", Extensions: []string{".sql"}, Support: SupportInventory},
	{Name: "GraphQL", Extensions: []string{".graphql", ".gql"}, Support: SupportInventory},
	{Name: "Protobuf", Extensions: []string{".proto"}, Support: SupportInventory},

	// ── data / config / docs (inventory tier is the right tier for these) ──
	{Name: "JSON", Extensions: []string{".json"}, Support: SupportInventory},
	{Name: "YAML", Extensions: []string{".yml", ".yaml"}, Support: SupportInventory},
	{Name: "TOML", Extensions: []string{".toml"}, Support: SupportInventory},
	{Name: "XML", Extensions: []string{".xml"}, Support: SupportInventory},
	{Name: "Markdown", Extensions: []string{".md", ".markdown", ".mdx"}, Support: SupportInventory},
}

// Lookup finds the catalog entry for a path. Returns nil if nothing matches —
// the file still goes into the graph at inventory tier, just under "Unknown".
func Lookup(path string) *Language {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	// Filename match wins over extension (Dockerfile has no .ext).
	for i := range Catalog {
		for _, fn := range Catalog[i].Filenames {
			if base == fn {
				return &Catalog[i]
			}
		}
	}
	if ext == "" {
		return nil
	}
	for i := range Catalog {
		for _, e := range Catalog[i].Extensions {
			if e == ext {
				return &Catalog[i]
			}
		}
	}
	return nil
}

// ByTier groups the catalog by Support tier for display. Returned in
// deep → structural → symbolic → inventory order.
func ByTier() map[Support][]Language {
	out := map[Support][]Language{
		SupportDeep:       {},
		SupportStructural: {},
		SupportSymbolic:   {},
		SupportInventory:  {},
	}
	for _, l := range Catalog {
		out[l.Support] = append(out[l.Support], l)
	}
	return out
}

// TierOrder is the canonical display order.
var TierOrder = []Support{SupportDeep, SupportStructural, SupportSymbolic, SupportInventory}

// TierLabel returns a human-readable header for a tier.
func TierLabel(s Support) string {
	switch s {
	case SupportDeep:
		return "DEEP SUPPORT (call graphs, types, scope)"
	case SupportStructural:
		return "STRUCTURAL SUPPORT (functions, classes, imports)"
	case SupportSymbolic:
		return "SYMBOLIC SUPPORT (named symbols only)"
	case SupportInventory:
		return "INVENTORY ONLY (file tracked, not parsed)"
	default:
		return string(s)
	}
}
