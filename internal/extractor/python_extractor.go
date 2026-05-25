package extractor

import (
	"path/filepath"
	"strings"

	"github.com/Universe/universe/internal/models"
)

const pyLanguage = "python"

// NewPythonExtractor returns a semantic extractor for Python parse results.
func NewPythonExtractor() Extractor {
	return PythonExtractor{}
}

type PythonExtractor struct{}

func (PythonExtractor) Language() string { return pyLanguage }

func (e PythonExtractor) Extract(result *models.ParseResult, allResults []*models.ParseResult) (*models.ParseResult, error) {
	if result == nil || !e.matches(result) {
		return result, nil
	}

	combined := gatherPyResults(result, allResults)

	modPathByFile := map[string]string{}
	modNodeByPath := map[string]string{}
	importBindingsByFile := map[string][]importBinding{}

	idx := newPyIndex()

	for _, pr := range combined {
		if pr == nil || !languageIsPy(pr) {
			continue
		}
		idx.ingest(pr)

		modulePath := resolvePyModulePath(pr)
		if modulePath != "" {
			modPathByFile[pr.FilePath] = modulePath
		}

		for _, n := range pr.Nodes {
			switch n.Type {
			case models.NodeModule:
				mp := firstNonEmpty(
					metaGet(&n, "module_path"),
					metaGet(&n, "fqn"),
					modulePath,
					pyModuleFromFilePath(n.FilePath),
				)
				if mp != "" {
					modPathByFile[n.FilePath] = mp
					modNodeByPath[mp] = n.ID
				}
			}
		}

		if binds := collectImportBindings(pr); len(binds) > 0 {
			importBindingsByFile[pr.FilePath] = append(importBindingsByFile[pr.FilePath], binds...)
		}
	}

	if mp := resolvePyModulePath(result); mp != "" {
		if _, ok := modPathByFile[result.FilePath]; !ok {
			modPathByFile[result.FilePath] = mp
		}
	}

	for i := range result.Nodes {
		e.annotatePyExported(&result.Nodes[i])
	}

	seen := edgeKeySetExisting(result.Edges)

	for i := range result.Edges {
		edge := &result.Edges[i]
		if edge.Type != models.EdgeCalls {
			continue
		}
		if edge.Metadata == nil {
			edge.Metadata = map[string]string{}
		}

		mod := modPathByFile[result.FilePath]
		if mod == "" {
			mod = resolvePyModulePath(result)
		}

		toID, ok := e.resolvePyCall(edge, mod, importBindingsByFile[result.FilePath], idx)
		if ok && edge.To == "" {
			key := edgeKey(edge.From, toID, models.EdgeCalls)
			if !seen[key] {
				edge.To = toID
				edge.Metadata["call_resolved"] = "true"
				seen[key] = true
			}
		}
	}

	e.addPyInheritanceEdges(result, modPathByFile, importBindingsByFile[result.FilePath], idx, seen)
	e.addPyModuleDependsOn(result, modPathByFile, importBindingsByFile[result.FilePath], modNodeByPath, seen)

	return result, nil
}

func (PythonExtractor) matches(pr *models.ParseResult) bool {
	if pr.Language != "" && pr.Language != pyLanguage {
		return false
	}
	return strings.HasSuffix(strings.ToLower(pr.FilePath), ".py")
}

func gatherPyResults(cur *models.ParseResult, all []*models.ParseResult) []*models.ParseResult {
	out := make([]*models.ParseResult, 0, len(all)+1)
	out = append(out, all...)
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

func languageIsPy(pr *models.ParseResult) bool {
	if pr.Language != "" && pr.Language != pyLanguage {
		return false
	}
	return strings.HasSuffix(strings.ToLower(pr.FilePath), ".py")
}

type importBinding struct {
	Local       string
	Module      string
	Symbol      string
	IsNamespace bool
}

func collectImportBindings(pr *models.ParseResult) []importBinding {
	var out []importBinding
	for _, n := range pr.Nodes {
		if n.Type != models.NodeImport {
			continue
		}

		module := strings.TrimSpace(firstNonEmpty(
			metaGet(&n, "module"),
			metaGet(&n, "from_module"),
			metaGet(&n, "import_path"),
		))

		symbol := strings.TrimSpace(metaGet(&n, "symbol"))
		local := strings.TrimSpace(firstNonEmpty(
			metaGet(&n, "local_name"),
			metaGet(&n, "local"),
			metaGet(&n, "binding"),
			metaGet(&n, "alias"),
		))
		isFrom := metaGet(&n, "kind") == "from" ||
			metaGet(&n, "style") == "from" ||
			metaGet(&n, "from_import") == "true" ||
			symbol != ""

		if !isFrom {
			if module == "" {
				module = strings.TrimSpace(n.Name)
			}
			if local == "" {
				local = dottedLastSegment(module)
			}
			if module != "" && local != "" {
				out = append(out, importBinding{
					Local:       local,
					Module:      module,
					Symbol:      "",
					IsNamespace: true,
				})
			}
			continue
		}

		if module == "" {
			module = strings.TrimSpace(metaGet(&n, "package"))
		}
		if local == "" {
			local = symbol
		}
		if module != "" && symbol != "" && local != "" {
			out = append(out, importBinding{
				Local:  local,
				Module: module,
				Symbol: symbol,
			})
		}
	}
	return out
}

func resolvePyModulePath(pr *models.ParseResult) string {
	for _, n := range pr.Nodes {
		if n.Type != models.NodeModule {
			continue
		}
		if v := firstNonEmpty(
			metaGet(&n, "module_path"),
			metaGet(&n, "fqn"),
			strings.TrimSuffix(filepath.Base(n.FilePath), ".py"),
		); v != "" {
			return v
		}
	}
	for _, n := range pr.Nodes {
		if v := metaGet(&n, "module_path"); v != "" {
			return v
		}
	}
	return pyModuleFromFilePath(pr.FilePath)
}

func pyModuleFromFilePath(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(base), ".py"), "")
}

type pyIndex struct {
	funcs   map[pySym]string
	classes map[pySym]string
}

type pySym struct {
	module, name string
}

func newPyIndex() *pyIndex {
	return &pyIndex{
		funcs:   make(map[pySym]string),
		classes: make(map[pySym]string),
	}
}

func (ix *pyIndex) ingest(pr *models.ParseResult) {
	mod := resolvePyModulePath(pr)
	for _, n := range pr.Nodes {
		m := mod
		if n.Package != "" {
			m = n.Package
		}
		if m == "" {
			continue
		}
		switch n.Type {
		case models.NodeFunction:
			if n.ID != "" {
				name := dottedLastSegment(strings.TrimSpace(n.Name))
				if name != "" {
					ix.funcs[pySym{m, name}] = n.ID
				}
			}
		case models.NodeClass:
			if n.ID != "" {
				name := dottedLastSegment(strings.TrimSpace(n.Name))
				if name != "" {
					ix.classes[pySym{m, name}] = n.ID
				}
			}
		}
	}
}

func (ix *pyIndex) lookupFunc(module, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if id, ok := ix.funcs[pySym{module, name}]; ok {
		return id, true
	}
	if alt, ok := ix.funcs[pySym{module, dottedLastSegment(name)}]; ok {
		return alt, true
	}
	return "", false
}

func (ix *pyIndex) lookupClass(module, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if id, ok := ix.classes[pySym{module, name}]; ok {
		return id, true
	}
	if alt, ok := ix.classes[pySym{module, dottedLastSegment(name)}]; ok {
		return alt, true
	}
	return "", false
}

func (PythonExtractor) annotatePyExported(n *models.Node) {
	switch n.Type {
	case models.NodeFunction, models.NodeClass, models.NodeVariable, models.NodeModule:
	default:
		return
	}
	tok := pyExportedNameToken(n.Name)
	if tok == "" {
		return
	}
	if n.Metadata == nil {
		n.Metadata = map[string]string{}
	}
	if mapHasKey(n.Metadata, "exported") {
		return
	}
	n.Metadata["exported"] = strconvBool(!strings.HasPrefix(tok, "_"))
}

func pyExportedNameToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func (PythonExtractor) resolvePyCall(edge *models.Edge, currentModule string, binds []importBinding, idx *pyIndex) (string, bool) {
	if metaGetEdge(edge, "call_resolved") == "true" && edge.To != "" {
		return edge.To, true
	}
	qual, callee := pythonCallParts(edge)
	if callee == "" {
		return "", false
	}

	localToMod := map[string]importBinding{}
	for _, b := range binds {
		if b.Local != "" {
			localToMod[b.Local] = b
		}
	}

	if qual == "" {
		if id, ok := idx.lookupFunc(currentModule, callee); ok {
			return id, true
		}
		if b, ok := localToMod[callee]; ok && !b.IsNamespace && b.Symbol != "" {
			return idx.lookupFunc(b.Module, b.Symbol)
		}
		return "", false
	}

	b := localToMod[qual]
	switch {
	case b.Module != "" && b.Symbol != "" && qual == b.Local:
		return idx.lookupFunc(b.Module, b.Symbol)

	case b.Module != "" && b.IsNamespace && qual != "":
		return idx.lookupFunc(b.Module, callee)

	default:
		if b.Module != "" {
			return idx.lookupFunc(b.Module, callee)
		}
		return "", false
	}
}

func pythonCallParts(edge *models.Edge) (qualifier, callee string) {
	if edge.Metadata == nil {
		return "", ""
	}
	md := edge.Metadata
	if expr := firstNonEmpty(md["call"], md["call_expr"], md["expression"], md["callee_expression"]); expr != "" {
		return splitCallExpression(expr)
	}
	q := firstNonEmpty(md["call_qualifier"], md["receiver"], md["object"])
	fn := firstNonEmpty(md["call_name"], md["callee"], md["name"], md["selector"])
	if fn != "" {
		return strings.TrimSpace(q), strings.TrimSpace(fn)
	}
	return "", ""
}

func dottedLastSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}

func parseBasesList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (PythonExtractor) addPyInheritanceEdges(
	result *models.ParseResult,
	modPathByFile map[string]string,
	binds []importBinding,
	idx *pyIndex,
	seen map[string]bool,
) {
	mod := modPathByFile[result.FilePath]
	if mod == "" {
		mod = resolvePyModulePath(result)
	}
	localAlias := map[string]string{}
	for _, b := range binds {
		if b.Local != "" && b.Module != "" {
			localAlias[b.Local] = b.Module
		}
	}
	for _, n := range result.Nodes {
		if n.Type != models.NodeClass {
			continue
		}
		bases := parseBasesList(firstNonEmpty(
			metaGet(&n, "bases"),
			metaGet(&n, "base_classes"),
		))
		for _, baseExpr := range bases {
			q, short := splitCallExpression(strings.TrimSpace(baseExpr))
			if short == "" {
				continue
			}
			targetMod := mod
			if q != "" {
				if m, ok := localAlias[q]; ok {
					targetMod = m
				} else {
					targetMod = q
				}
			}
			baseID, ok := idx.lookupClass(targetMod, short)
			if !ok && q == "" {
				baseID, ok = idx.lookupClass(mod, short)
			}
			if !ok {
				continue
			}
			key := edgeKey(n.ID, baseID, models.EdgeInherits)
			if seen[key] {
				continue
			}
			result.Edges = append(result.Edges, models.Edge{
				From: n.ID,
				To:   baseID,
				Type: models.EdgeInherits,
				Metadata: map[string]string{
					"language": pyLanguage,
				},
			})
			seen[key] = true
		}
	}
}

func (PythonExtractor) addPyModuleDependsOn(
	result *models.ParseResult,
	modPathByFile map[string]string,
	binds []importBinding,
	modNodeByPath map[string]string,
	seen map[string]bool,
) {
	fromMod := modPathByFile[result.FilePath]
	if fromMod == "" {
		fromMod = resolvePyModulePath(result)
	}
	fromNode := modNodeByPath[fromMod]
	if fromNode == "" {
		return
	}

	uniq := map[string]struct{}{}
	for _, b := range binds {
		m := strings.TrimSpace(b.Module)
		if m == "" || m == fromMod {
			continue
		}
		uniq[m] = struct{}{}
	}
	for depMod := range uniq {
		toNode := modNodeByPath[depMod]
		if toNode == "" {
			continue
		}
		key := edgeKey(fromNode, toNode, models.EdgeDependsOn)
		if seen[key] {
			continue
		}
		result.Edges = append(result.Edges, models.Edge{
			From: fromNode,
			To:   toNode,
			Type: models.EdgeDependsOn,
			Metadata: map[string]string{
				"language":       pyLanguage,
				"scope":          "module",
				"depends_module": depMod,
				"source_file":    result.FilePath,
				"aggregate":      "imports",
				"module_level":   "true",
			},
		})
		seen[key] = true
	}
}

var _ Extractor = (*PythonExtractor)(nil)
