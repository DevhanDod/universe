package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Universe/universe/internal/analyzer"
	"github.com/Universe/universe/internal/extractor"
	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/languages"
	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/parser"
	"github.com/spf13/cobra"
)

// Version is set at build time: go build -ldflags "-X main.Version=0.1.0"
var Version = "dev"

const defaultGraphRel = ".universe/graph.json"

var persistentGraphPath string

func main() {
	rootCmd.PersistentFlags().StringVar(&persistentGraphPath, "graph-file", "",
		fmt.Sprintf("path to saved graph JSON (default: ./%s)", defaultGraphRel))

	rootCmd.AddCommand(analyzeCmd, queryCmd, statsCmd, graphCmd, mcpCmd, configCmd, dashboardCmd,
		initCmd, statusCmd, dbCmd, skillsCmd, languagesCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolvedGraphPath() string {
	if strings.TrimSpace(persistentGraphPath) != "" {
		return filepath.Clean(persistentGraphPath)
	}
	wd, err := os.Getwd()
	if err != nil {
		return defaultGraphRel
	}
	return filepath.Join(wd, defaultGraphRel)
}

var rootCmd = &cobra.Command{
	Use:          "universe",
	Short:        "Build and query codebase knowledge graphs",
	SilenceUsage: true,
	Version:      Version,
}

var (
	analyzeOutput        string
	analyzeFormat        string
	analyzeVerbose       bool
	analyzeIncludeSource bool
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze <path>",
	Short: "Analyze a project and build the dependency graph",
	Args:  cobra.ExactArgs(1),
	RunE:  runAnalyze,
}

func buildAnalyzer(verbose, includeSource bool) *analyzer.Analyzer {
	reg := parser.NewRegistry()
	reg.Register(parser.NewGoParser())
	reg.Register(parser.NewPythonParser())
	exts := []extractor.Extractor{
		extractor.NewGoExtractor(),
		extractor.NewPythonExtractor(),
	}
	return analyzer.NewAnalyzer(reg, exts, analyzer.Config{
		Verbose:       verbose,
		IncludeSource: includeSource,
	})
}

func analyzeLogWriter(cmd *cobra.Command) io.Writer {
	if strings.EqualFold(strings.TrimSpace(analyzeFormat), "json") {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	projectPath := filepath.Clean(args[0])
	logw := analyzeLogWriter(cmd)

	fmt.Fprintf(logw, "🔍 Scanning %s...\n", projectPath)

	an := buildAnalyzer(analyzeVerbose, analyzeIncludeSource)
	g, err := an.Analyze(projectPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(logw, "📊 Building knowledge graph...\n")
	printAnalyzeProgress(g, logw)

	stats := g.Stats()
	fmt.Fprintf(logw, "\n🧠 Graph summary:\n")
	printStatsBlockTo(stats, logw)

	printPackageDepsTo(g, logw)

	printUnsupportedLanguagesTo(g, logw)

	outPath := resolvedGraphPath()
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create graph directory: %w", err)
	}
	if err := g.ExportJSON(outPath); err != nil {
		return fmt.Errorf("save default graph: %w", err)
	}
	fmt.Fprintf(logw, "\n✓ Saved graph to %s\n", outPath)

	if strings.TrimSpace(analyzeOutput) != "" {
		cp := filepath.Clean(analyzeOutput)
		if err := g.ExportJSON(cp); err != nil {
			return fmt.Errorf("export --output: %w", err)
		}
		fmt.Fprintf(logw, "✓ Also wrote %s\n", cp)
	}

	switch strings.ToLower(strings.TrimSpace(analyzeFormat)) {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(stats); err != nil {
			return err
		}
	case "text":
	default:
		return fmt.Errorf("unknown --format %q (use json or text)", analyzeFormat)
	}
	return nil
}

func printAnalyzeProgress(g *graph.Graph, w io.Writer) {
	byLang := map[string]struct{ files, funcs, classes, structs, ifaces int }{}
	filesSeen := map[string]string{}
	for _, n := range g.Nodes {
		if n == nil || n.FilePath == "" {
			continue
		}
		lang := langFromPath(n.FilePath)
		b := byLang[lang]
		if _, ok := filesSeen[n.FilePath]; !ok {
			filesSeen[n.FilePath] = lang
			b.files++
		}
		switch n.Type {
		case models.NodeFunction, models.NodeMethod:
			b.funcs++
		case models.NodeClass:
			b.classes++
		case models.NodeStruct:
			b.structs++
		case models.NodeInterface:
			b.ifaces++
		}
		byLang[lang] = b
	}
	order := []string{"go", "python"}
	for _, lang := range order {
		b, ok := byLang[lang]
		if !ok || b.files == 0 {
			continue
		}
		switch lang {
		case "go":
			fmt.Fprintf(w, "   ✓ Parsed %d Go files (%d functions/methods, %d structs, %d interfaces)\n",
				b.files, b.funcs, b.structs, b.ifaces)
		case "python":
			fmt.Fprintf(w, "   ✓ Parsed %d Python files (%d functions, %d classes)\n",
				b.files, b.funcs, b.classes)
		}
	}
	depEdges, callEdges := 0, 0
	for _, e := range g.Edges {
		if e == nil {
			continue
		}
		switch e.Type {
		case models.EdgeDependsOn:
			depEdges++
		case models.EdgeCalls:
			callEdges++
		}
	}
	if depEdges > 0 {
		fmt.Fprintf(w, "   ✓ Extracted %d package/module dependency edges\n", depEdges)
	}
	if callEdges > 0 {
		fmt.Fprintf(w, "   ✓ Extracted %d call edges\n", callEdges)
	}
}

func langFromPath(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	default:
		return "other"
	}
}

func printStatsBlockTo(s graph.GraphStats, w io.Writer) {
	fmt.Fprintf(w, "   Total nodes:  %d\n", s.TotalNodes)
	fmt.Fprintf(w, "   Total edges:  %d\n", s.TotalEdges)
	fmt.Fprintf(w, "   Packages/modules: %d\n", s.PackageCount)
	fmt.Fprintf(w, "   Source files:     %d\n", s.FileCount)
	if len(s.NodesByType) > 0 {
		fmt.Fprintf(w, "   Nodes by type:\n")
		printSortedCountsTo(s.NodesByType, "      ", w)
	}
	printCoverageTo(s.Coverage, w)
}

func printCoverageTo(c models.Coverage, w io.Writer) {
	if c.TotalFiles == 0 {
		return
	}
	fmt.Fprintf(w, "\n📈 Coverage:\n")
	fmt.Fprintf(w, "   Files parsed: %d / %d (%.1f%%)\n",
		c.ParsedFiles, c.TotalFiles, c.FileCoverage*100)
	fmt.Fprintf(w, "   Bytes parsed: %s / %s (%.1f%%)\n",
		humanBytes(c.ParsedBytes), humanBytes(c.TotalBytes), c.ByteCoverage*100)
	if len(c.Breakdown) > 0 {
		fmt.Fprintf(w, "   By kind:\n")
		kinds := make([]string, 0, len(c.Breakdown))
		for k := range c.Breakdown {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			b := c.Breakdown[k]
			fmt.Fprintf(w, "      • %-10s files=%d parsed=%d  bytes=%s parsed=%s\n",
				k, b.Files, b.FilesParsed, humanBytes(b.Bytes), humanBytes(b.BytesParsed))
		}
	}
}

// printUnsupportedLanguagesTo scans the graph's file nodes, looks each up in
// the language manifest, and reports which languages were detected but parsed
// at less than the manifest's claimed tier — including completely unsupported
// languages (no manifest entry at all). This is the feedback loop: it tells
// the user what's missing, and tells us what to prioritise next.
func printUnsupportedLanguagesTo(g *graph.Graph, w io.Writer) {
	type langStat struct {
		fileCount int
		isKnown   bool   // present in the manifest
		support   languages.Support
	}
	stats := map[string]*langStat{}
	unknownByExt := map[string]int{} // extensions with no manifest entry

	for _, n := range g.Nodes {
		if n == nil || n.Type != models.NodeFile {
			continue
		}
		// Skip synthetic nodes (gitignore patterns) — those aren't real files.
		if n.Metadata != nil && n.Metadata["kind"] == "gitignore_pattern" {
			continue
		}
		path := n.FilePath
		if path == "" {
			continue
		}
		lang := languages.Lookup(path)
		if lang == nil {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == "" {
				ext = "(no extension)"
			}
			unknownByExt[ext]++
			continue
		}
		s, ok := stats[lang.Name]
		if !ok {
			s = &langStat{isKnown: true, support: lang.Support}
			stats[lang.Name] = s
		}
		s.fileCount++
	}

	// Anything in the manifest at inventory tier counts as "detected but not
	// yet supported" — the user has files in that language, and we don't
	// structurally understand them.
	var pending []string
	for name, s := range stats {
		if s.support == languages.SupportInventory {
			pending = append(pending, fmt.Sprintf("   %-14s %d files  — %s (planned)",
				name, s.fileCount, supportNeedLabel(s.support)))
		}
	}
	if len(pending) == 0 && len(unknownByExt) == 0 {
		return
	}

	fmt.Fprintf(w, "\n⚠️  Languages detected without full support:\n")
	sort.Strings(pending)
	for _, line := range pending {
		fmt.Fprintln(w, line)
	}
	if len(unknownByExt) > 0 {
		exts := make([]string, 0, len(unknownByExt))
		for e := range unknownByExt {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		for _, e := range exts {
			fmt.Fprintf(w, "   %-14s %d files  — unknown to universe\n", e, unknownByExt[e])
		}
	}
	fmt.Fprintln(w, "   Run `universe languages` for the full support matrix.")
}

func supportNeedLabel(s languages.Support) string {
	switch s {
	case languages.SupportInventory:
		return "inventory tier (no parser)"
	case languages.SupportSymbolic:
		return "symbolic tier (regex)"
	case languages.SupportStructural:
		return "structural tier (tree-sitter)"
	case languages.SupportDeep:
		return "deep tier (full parser)"
	}
	return string(s)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}

func printSortedCountsTo(m map[string]int, indent string, w io.Writer) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s• %s: %d\n", indent, k, m[k])
	}
}

func printPackageDepsTo(g *graph.Graph, w io.Writer) {
	type pair struct{ from, to string }
	seen := map[pair]struct{}{}
	var list []pair
	for _, e := range g.Edges {
		if e == nil || e.Type != models.EdgeDependsOn {
			continue
		}
		a, b := g.GetNode(e.From), g.GetNode(e.To)
		if a == nil || b == nil {
			continue
		}
		from := nodePkgLabel(a)
		to := nodePkgLabel(b)
		p := pair{from: from, to: to}
		if _, ok := seen[p]; ok || from == "" || to == "" {
			continue
		}
		seen[p] = struct{}{}
		list = append(list, p)
	}
	if len(list) == 0 {
		return
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].from != list[j].from {
			return list[i].from < list[j].from
		}
		return list[i].to < list[j].to
	})
	fmt.Fprintf(w, "\n📦 Package dependencies:\n")
	byFrom := map[string][]string{}
	for _, p := range list {
		byFrom[p.from] = append(byFrom[p.from], p.to)
	}
	fromKeys := make([]string, 0, len(byFrom))
	for k := range byFrom {
		fromKeys = append(fromKeys, k)
	}
	sort.Strings(fromKeys)
	for _, f := range fromKeys {
		to := append([]string(nil), byFrom[f]...)
		sort.Strings(to)
		fmt.Fprintf(w, "   %s → %s\n", f, strings.Join(to, ", "))
	}
}

func nodePkgLabel(n *models.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type {
	case models.NodePackage, models.NodeModule:
		return firstNonEmpty(n.Name, n.Package)
	default:
		return firstNonEmpty(n.Package, n.Name)
	}
}

func firstNonEmpty(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a != "" {
		return a
	}
	return b
}

var (
	queryDependsOn    string
	queryDependencies string
	queryType         string
)

var queryCmd = &cobra.Command{
	Use:   "query [query]",
	Short: "Search or filter nodes in the saved graph",
	RunE:  runQuery,
}

func runQuery(cmd *cobra.Command, args []string) error {
	q := ""
	if len(args) > 0 {
		q = strings.TrimSpace(args[0])
	}
	if queryDependsOn == "" && queryDependencies == "" && q == "" {
		return fmt.Errorf("provide a search string or --depends-on / --dependencies")
	}

	g, err := loadGraph(resolvedGraphPath())
	if err != nil {
		return err
	}

	nt, err := parseNodeTypeOptional(queryType)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	switch {
	case queryDependsOn != "":
		targets := findNodesBySubstring(g, queryDependsOn)
		targets = filterNodeTypeSlice(targets, nt)
		if len(targets) == 0 {
			fmt.Fprintf(w, "No nodes matched %q\n", queryDependsOn)
			return nil
		}
		fmt.Fprintf(w, "🔍 Matches for %q (showing dependents)\n\n", queryDependsOn)
		for _, t := range targets {
			fmt.Fprintf(w, "✓ %s [%s]\n", t.Name, t.Type)
			for _, d := range g.GetDependents(t.ID) {
				fmt.Fprintf(w, "   ↑ %s (%s)\n", d.Name, d.Type)
			}
			fmt.Fprintln(w)
		}
	case queryDependencies != "":
		targets := findNodesBySubstring(g, queryDependencies)
		targets = filterNodeTypeSlice(targets, nt)
		if len(targets) == 0 {
			fmt.Fprintf(w, "No nodes matched %q\n", queryDependencies)
			return nil
		}
		fmt.Fprintf(w, "🔍 Matches for %q (showing dependencies)\n\n", queryDependencies)
		for _, t := range targets {
			fmt.Fprintf(w, "✓ %s [%s]\n", t.Name, t.Type)
			for _, d := range g.GetDependencies(t.ID) {
				fmt.Fprintf(w, "   → %s (%s)\n", d.Name, d.Type)
			}
			fmt.Fprintln(w)
		}
	default:
		nodes := g.Search(q)
		nodes = filterNodeTypeSlice(nodes, nt)
		if len(nodes) == 0 {
			fmt.Fprintf(w, "No nodes matched %q\n", q)
			return nil
		}
		fmt.Fprintf(w, "🔍 Results for %q (%d):\n\n", q, len(nodes))
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Type != nodes[j].Type {
				return nodes[i].Type < nodes[j].Type
			}
			return nodes[i].Name < nodes[j].Name
		})
		for _, n := range nodes {
			loc := n.FilePath
			if loc != "" {
				fmt.Fprintf(w, "✓ %-20s %-10s %s\n", truncate(n.Name, 20), n.Type, loc)
			} else {
				fmt.Fprintf(w, "✓ %-20s %s\n", truncate(n.Name, 20), n.Type)
			}
		}
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func findNodesBySubstring(g *graph.Graph, needle string) []*models.Node {
	n := strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return nil
	}
	var out []*models.Node
	for _, node := range g.Nodes {
		if node == nil {
			continue
		}
		if strings.Contains(strings.ToLower(node.Name), n) ||
			strings.Contains(strings.ToLower(node.Package), n) ||
			strings.Contains(strings.ToLower(node.ID), n) {
			out = append(out, node)
		}
	}
	return out
}

func filterNodeTypeSlice(nodes []*models.Node, t models.NodeType) []*models.Node {
	if t == "" {
		return nodes
	}
	var out []*models.Node
	for _, n := range nodes {
		if n != nil && n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

func parseNodeTypeOptional(s string) (models.NodeType, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "", nil
	}
	m := map[string]models.NodeType{
		"package":   models.NodePackage,
		"file":      models.NodeFile,
		"function":  models.NodeFunction,
		"method":    models.NodeMethod,
		"struct":    models.NodeStruct,
		"interface": models.NodeInterface,
		"type":      models.NodeType_,
		"variable":  models.NodeVariable,
		"import":    models.NodeImport,
		"class":     models.NodeClass,
		"module":    models.NodeModule,
	}
	if v, ok := m[s]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unknown node type %q", s)
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show statistics for the saved graph",
	RunE:  runStats,
}

func runStats(cmd *cobra.Command, args []string) error {
	g, err := loadGraph(resolvedGraphPath())
	if err != nil {
		return err
	}
	s := g.Stats()
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "📊 Graph statistics (%s)\n\n", resolvedGraphPath())
	printStatsBlockTo(s, w)
	if len(s.EdgesByType) > 0 {
		fmt.Fprintf(w, "   Edges by type:\n")
		printSortedCountsTo(s.EdgesByType, "      ", w)
	}
	return nil
}

var (
	graphOutput string
	graphFormat string
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Export the full saved graph",
	RunE:  runGraphExport,
}

func runGraphExport(cmd *cobra.Command, args []string) error {
	g, err := loadGraph(resolvedGraphPath())
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(graphFormat)) {
	case "json":
		data, err := json.MarshalIndent(g, "", "  ")
		if err != nil {
			return err
		}
		if strings.TrimSpace(graphOutput) == "" {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		}
		return os.WriteFile(filepath.Clean(graphOutput), append(data, '\n'), 0o644)
	case "dot":
		s := graphToDOT(g)
		if strings.TrimSpace(graphOutput) == "" {
			_, err = io.WriteString(cmd.OutOrStdout(), s)
			return err
		}
		return os.WriteFile(filepath.Clean(graphOutput), []byte(s), 0o644)
	default:
		return fmt.Errorf("unknown --format %q (use json or dot)", graphFormat)
	}
}

func loadGraph(path string) (*graph.Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no graph at %s — run `universe analyze` first", path)
		}
		return nil, err
	}
	var g graph.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse graph: %w", err)
	}
	if g.Nodes == nil {
		g.Nodes = make(map[string]*models.Node)
	}
	if g.Files == nil {
		g.Files = make(map[string]*models.FileInfo)
	}
	return &g, nil
}

func graphToDOT(g *graph.Graph) string {
	var b strings.Builder
	b.WriteString("digraph universe {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=rounded];\n")

	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := g.Nodes[id]
		if n == nil {
			continue
		}
		lbl := dotEscape(fmt.Sprintf("%s\n%s", n.Name, n.Type))
		b.WriteString(fmt.Sprintf("  %q [label=\"%s\"];\n", id, lbl))
	}
	for _, e := range g.Edges {
		if e == nil {
			continue
		}
		lbl := dotEscape(string(e.Type))
		b.WriteString(fmt.Sprintf("  %q -> %q [label=\"%s\"];\n", e.From, e.To, lbl))
	}
	b.WriteString("}\n")
	return b.String()
}

func dotEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func init() {
	analyzeCmd.Flags().StringVarP(&analyzeOutput, "output", "o", "", "also write graph JSON to this file")
	analyzeCmd.Flags().StringVar(&analyzeFormat, "format", "text", "stdout summary format: json|text")
	analyzeCmd.Flags().BoolVarP(&analyzeVerbose, "verbose", "v", false, "verbose parsing output")
	analyzeCmd.Flags().BoolVar(&analyzeIncludeSource, "include-source", true, "include file source content in graph JSON")
	queryCmd.Args = cobra.MaximumNArgs(1)
	queryCmd.Flags().StringVar(&queryDependsOn, "depends-on", "", "show nodes that depend on a symbol or package matching this name")
	queryCmd.Flags().StringVar(&queryDependencies, "dependencies", "", "show outbound dependencies for a matching node")
	queryCmd.Flags().StringVar(&queryType, "type", "", "filter by node type (e.g. function, package, class)")
	graphCmd.Flags().StringVarP(&graphOutput, "output", "o", "", "output file (default: stdout)")
	graphCmd.Flags().StringVar(&graphFormat, "format", "json", "json|dot")
}
