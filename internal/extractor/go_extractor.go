package extractor

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Universe/universe/internal/models"
)

const goLanguage = "go"

// NewGoExtractor returns a semantic extractor for Go parse results.
func NewGoExtractor() Extractor {
	return GoExtractor{}
}

type GoExtractor struct{}

func (GoExtractor) Language() string { return goLanguage }

func (e GoExtractor) Extract(result *models.ParseResult, allResults []*models.ParseResult) (*models.ParseResult, error) {
	if result == nil || !e.matches(result) {
		return result, nil
	}

	combined := gatherResults(result, allResults)

	pkgPathByFile := map[string]string{}
	importMapByFile := map[string]map[string]string{}
	pkgNodeByImportPath := map[string]string{}
	global := newSymbolIndex()

	for _, pr := range combined {
		if pr == nil || !languageIsGo(pr) {
			continue
		}
		global.ingestParseResult(pr)
		for _, n := range pr.Nodes {
			switch n.Type {
			case models.NodePackage:
				if ip := firstNonEmpty(
					metaGet(&n, "import_path"),
					metaGet(&n, "module_path"),
					metaGet(&n, "path"),
				); ip != "" {
					pkgPathByFile[n.FilePath] = ip
					pkgNodeByImportPath[ip] = n.ID
				}
			case models.NodeImport:
				m := importMapForFile(importMapByFile, n.FilePath)
				local := firstNonEmpty(
					metaGet(&n, "local_name"),
					metaGet(&n, "local"),
					metaGet(&n, "name"),
				)
				ip := firstNonEmpty(
					metaGet(&n, "import_path"),
					metaGet(&n, "path"),
					n.Name,
				)
				ip = strings.Trim(ip, `"`)
				if local == "" && ip != "" {
					local = path.Base(ip)
				}
				if local != "" && ip != "" {
					m[local] = ip
				}
			}
		}
	}

	currentPkg := pkgPathForFile(pkgPathByFile, result.FilePath)
	if currentPkg == "" {
		currentPkg = firstNonEmpty(
			findPackageImportPath(result),
			findPackageImportPathFromNodes(result.Nodes),
		)
		if currentPkg != "" {
			pkgPathByFile[result.FilePath] = currentPkg
		}
	}

	// Backfill pkgNodeByImportPath: the Go parser never sets `import_path` on
	// package nodes, so the initial sweep populated nothing. Derive import
	// paths from the import statements seen in *other* files — if file A
	// imports "github.com/foo/internal/bar", and file B's directory is
	// "internal/bar", then file B's package node is what that import points to.
	dirToPkgNode := map[string]string{} // "internal/dashboard" -> pkg-node-ID
	for _, pr := range combined {
		if pr == nil || !languageIsGo(pr) {
			continue
		}
		for _, n := range pr.Nodes {
			if n.Type == models.NodePackage && n.FilePath == pr.FilePath {
				dir := path.Dir(strings.ReplaceAll(n.FilePath, "\\", "/"))
				if dir != "" && dir != "." {
					if _, exists := dirToPkgNode[dir]; !exists {
						dirToPkgNode[dir] = n.ID
					}
				}
				break
			}
		}
	}
	// Collect every import path used anywhere, then match each against the
	// directory map. An import path is the dir itself or has the dir as suffix.
	seenIP := map[string]struct{}{}
	for _, mp := range importMapByFile {
		for _, ip := range mp {
			ip = strings.Trim(ip, `"`)
			if ip == "" {
				continue
			}
			if _, ok := seenIP[ip]; ok {
				continue
			}
			seenIP[ip] = struct{}{}
			if id, ok := pkgNodeByImportPath[ip]; ok && id != "" {
				continue
			}
			for dir, nodeID := range dirToPkgNode {
				if ip == dir || strings.HasSuffix(ip, "/"+dir) {
					pkgNodeByImportPath[ip] = nodeID
					break
				}
			}
		}
	}
	// Also register the current file's package node under its directory-derived
	// import-path candidates so signature-edge resolution lookups can hit it.
	for _, pr := range combined {
		if pr == nil || !languageIsGo(pr) {
			continue
		}
		for _, n := range pr.Nodes {
			if n.Type == models.NodePackage && n.FilePath == pr.FilePath {
				dir := path.Dir(strings.ReplaceAll(n.FilePath, "\\", "/"))
				if dir != "" && dir != "." {
					if _, exists := pkgNodeByImportPath[dir]; !exists {
						pkgNodeByImportPath[dir] = n.ID
					}
				}
				break
			}
		}
	}

	// Re-key the symbol index: it was built with short package names (because
	// the parser doesn't emit import paths). Now that we know each package's
	// import path via pkgNodeByImportPath, add parallel entries so lookups
	// keyed by the import path resolve too.
	shortToIP := map[string]string{}
	for ip, pkgNodeID := range pkgNodeByImportPath {
		parts := strings.Split(pkgNodeID, ":")
		if len(parts) >= 1 && parts[0] != "" {
			if _, exists := shortToIP[parts[0]]; !exists {
				shortToIP[parts[0]] = ip
			}
		}
	}
	for k, v := range global.any {
		if ip, ok := shortToIP[k.pkg]; ok && ip != k.pkg {
			if _, exists := global.any[symPair{ip, k.name}]; !exists {
				global.any[symPair{ip, k.name}] = v
			}
		}
	}
	for k, v := range global.funcs {
		parts := strings.SplitN(k, "\x00", 2)
		if len(parts) == 2 {
			if ip, ok := shortToIP[parts[0]]; ok && ip != parts[0] {
				nk := symKeyFunc(ip, parts[1])
				if _, exists := global.funcs[nk]; !exists {
					global.funcs[nk] = v
				}
			}
		}
	}

	for i := range result.Nodes {
		e.annotateExported(&result.Nodes[i])
	}

	seenEdge := edgeKeySetExisting(result.Edges)

	for i := range result.Edges {
		edge := &result.Edges[i]
		if edge.Metadata == nil {
			edge.Metadata = map[string]string{}
		}

		switch edge.Type {
		case models.EdgeCalls:
			toID, ok := e.resolveGoCall(edge, result.FilePath, currentPkg, importMapByFile, global)
			if ok {
				key := edgeKey(edge.From, toID, models.EdgeCalls)
				if !seenEdge[key] {
					edge.To = toID
					edge.Metadata["call_resolved"] = "true"
					seenEdge[key] = true
				}
			} else {
				// Fallback: if the call qualifier matches an import alias in
				// this file (e.g. `fmt.Println` where `fmt` is imported), point
				// the edge at that import node. Otherwise the edge has an
				// unresolvable `To` and gets silently dropped by the frontend
				// — that's why so many cross-package dependencies were invisible.
				qual, _ := goCallParts(edge)
				if qual != "" {
					if _, ok := importMapByFile[result.FilePath][qual]; ok {
						// Derive import node ID from the calling function's ID
						// (format: pkg:file:name) → reuse pkg + file segments.
						if importID := goImportNodeIDFromCaller(edge.From, qual); importID != "" {
							key := edgeKey(edge.From, importID, models.EdgeCalls)
							if !seenEdge[key] {
								edge.To = importID
								edge.Metadata["call_resolved_to_import"] = "true"
								seenEdge[key] = true
							}
						}
					}
				}
			}
		case models.EdgeReturns, models.EdgeDependsOn:
			// Signature edges from the parser whose `To` is still a bare type
			// name (parser couldn't resolve cross-file). Try the global index
			// for the current package, then any package imported by this file.
			if metaGetEdge(edge, "resolved") == "true" || strings.Contains(edge.To, ":") {
				continue
			}
			name := strings.TrimSpace(edge.To)
			if name == "" {
				continue
			}
			if id, ok := global.lookupAny(currentPkg, name); ok {
				edge.To = id
				edge.Metadata["resolved"] = "true"
				continue
			}
			for _, ip := range importMapByFile[result.FilePath] {
				if id, ok := global.lookupAny(ip, name); ok {
					edge.To = id
					edge.Metadata["resolved"] = "true"
					break
				}
			}
		}
	}

	if _, ok := pkgPathByFile[result.FilePath]; !ok {
		if p := findPackageImportPath(result); p != "" {
			pkgPathByFile[result.FilePath] = p
		}
	}

	e.addPackageDependencyEdges(result, pkgPathByFile, importMapByFile, pkgNodeByImportPath, seenEdge)

	// Connect each per-file import node to the *target* package's package
	// node. Without this, import nodes are leaves and the visual chain
	// "caller → import → target folder" terminates at the import.
	for _, n := range result.Nodes {
		if n.Type != models.NodeImport {
			continue
		}
		ip := firstNonEmpty(metaGet(&n, "path"), metaGet(&n, "import_path"), n.Name)
		ip = strings.Trim(ip, `"`)
		if ip == "" {
			continue
		}
		toPkgNode := pkgNodeByImportPath[ip]
		if toPkgNode == "" || toPkgNode == n.ID {
			continue
		}
		key := edgeKey(n.ID, toPkgNode, models.EdgeDependsOn)
		if seenEdge[key] {
			continue
		}
		result.Edges = append(result.Edges, models.Edge{
			From: n.ID,
			To:   toPkgNode,
			Type: models.EdgeDependsOn,
			Metadata: map[string]string{
				"language":    goLanguage,
				"scope":       "import_target",
				"import_path": ip,
			},
		})
		seenEdge[key] = true
	}

	for _, iface := range collectInterfaces(combined) {
		for _, st := range collectStructs(combined) {
			// Cross-package implementation is valid Go (e.g. http.Handler
			// implemented by handlers in every package). Drop the same-pkg
			// guard so we capture those edges too.
			if st.name == iface.name && st.pkgPath == iface.pkgPath {
				continue
			}
			if implementsGo(st.methods, iface.methodNames) {
				fromID := st.nodeID
				toID := iface.nodeID
				key := edgeKey(fromID, toID, models.EdgeImplements)
				if fromID != "" && toID != "" && !seenEdge[key] {
					result.Edges = append(result.Edges, models.Edge{
						From: fromID,
						To:   toID,
						Type: models.EdgeImplements,
						Metadata: map[string]string{
							"language": goLanguage,
						},
					})
					seenEdge[key] = true
				}
			}
		}
	}

	return result, nil
}

func (GoExtractor) matches(pr *models.ParseResult) bool {
	if pr.Language != "" && pr.Language != goLanguage {
		return false
	}
	return strings.HasSuffix(strings.ToLower(pr.FilePath), ".go")
}

func gatherResults(cur *models.ParseResult, all []*models.ParseResult) []*models.ParseResult {
	out := make([]*models.ParseResult, 0, len(all)+1)
	out = append(out, all...)
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

func languageIsGo(pr *models.ParseResult) bool {
	if pr.Language != "" && pr.Language != goLanguage {
		return false
	}
	return strings.HasSuffix(strings.ToLower(pr.FilePath), ".go")
}

func importMapForFile(m map[string]map[string]string, file string) map[string]string {
	if m[file] == nil {
		m[file] = make(map[string]string)
	}
	return m[file]
}

func pkgPathForFile(byFile map[string]string, file string) string {
	if p, ok := byFile[file]; ok {
		return p
	}
	return ""
}

func findPackageImportPath(pr *models.ParseResult) string {
	for _, n := range pr.Nodes {
		if n.Type != models.NodePackage {
			continue
		}
		if ip := firstNonEmpty(
			metaGet(&n, "import_path"),
			metaGet(&n, "module_path"),
			metaGet(&n, "path"),
		); ip != "" {
			return ip
		}
	}
	return ""
}

func findPackageImportPathFromNodes(nodes []models.Node) string {
	for _, n := range nodes {
		if n.Type != models.NodePackage {
			continue
		}
		if ip := firstNonEmpty(
			metaGet(&n, "import_path"),
			metaGet(&n, "module_path"),
			metaGet(&n, "path"),
		); ip != "" {
			return ip
		}
	}
	return ""
}

func metaGet(n *models.Node, k string) string {
	if n.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(n.Metadata[k])
}

func edgeKey(from, to string, et models.EdgeType) string {
	return string(et) + "\x00" + from + "\x00" + to
}

func edgeKeySetExisting(edges []models.Edge) map[string]bool {
	m := make(map[string]bool, len(edges))
	for _, e := range edges {
		m[edgeKey(e.From, e.To, e.Type)] = true
	}
	return m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func (GoExtractor) annotateExported(n *models.Node) {
	switch n.Type {
	case models.NodeFunction, models.NodeMethod, models.NodeStruct, models.NodeInterface, models.NodeType_, models.NodeVariable:
	default:
		return
	}
	name := exportedNameToken(n.Name)
	if name == "" {
		return
	}
	if n.Metadata == nil {
		n.Metadata = map[string]string{}
	}
	if !mapHasKey(n.Metadata, "exported") {
		first, _ := utf8.DecodeRuneInString(name)
		n.Metadata["exported"] = strconvBool(unicode.IsUpper(first))
	}
}

func exportedNameToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func strconvBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func mapHasKey(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

func (GoExtractor) resolveGoCall(
	edge *models.Edge,
	filePath, currentPkg string,
	importMapByFile map[string]map[string]string,
	global *symbolIndex,
) (string, bool) {
	if edge.To != "" && metaGetEdge(edge, "call_resolved") == "true" {
		return edge.To, true
	}

	qual, callee := goCallParts(edge)
	if callee == "" {
		return "", false
	}

	if qual == "" {
		if id, ok := global.lookupAny(currentPkg, callee); ok {
			return id, true
		}
		return "", false
	}

	imports := importMapByFile[filePath]
	targetPkg := ""
	if ip, ok := imports[qual]; ok {
		targetPkg = ip
	}

	if targetPkg == "" && isProbablySelectorPackage(qual, currentPkg) {
		targetPkg = currentPkg
	}

	if targetPkg != "" {
		if id, ok := global.lookupFunc(targetPkg, callee); ok {
			return id, true
		}
		if id, ok := global.lookupAny(targetPkg, callee); ok {
			return id, true
		}
	}

	return "", false
}

func isProbablySelectorPackage(qual, currentPkg string) bool {
	return qual != "" && (qual == path.Base(currentPkg) || strings.HasSuffix(currentPkg, "/"+qual))
}

// goImportNodeIDFromCaller derives the import-node ID for `alias` that lives
// in the same file as the caller. Node IDs follow the parser's pkg:file:name
// convention, so we reuse the pkg + file segments of the caller and swap the
// trailing name for the import alias.
func goImportNodeIDFromCaller(callerID, alias string) string {
	if callerID == "" || alias == "" {
		return ""
	}
	parts := strings.Split(callerID, ":")
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + ":" + parts[1] + ":" + alias
}

func metaGetEdge(edge *models.Edge, k string) string {
	if edge.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(edge.Metadata[k])
}

func goCallParts(edge *models.Edge) (qualifier, callee string) {
	md := edge.Metadata
	if md == nil {
		return "", ""
	}
	if expr := firstNonEmpty(md["call"], md["call_expr"], md["expression"], md["callee_expression"]); expr != "" {
		return splitCallExpression(expr)
	}
	q := firstNonEmpty(md["call_qualifier"], md["qualifier"], md["receiver_pkg"], md["import_alias"])
	fn := firstNonEmpty(md["call_name"], md["callee"], md["name"], md["selector"])
	if fn != "" {
		return strings.TrimSpace(q), strings.TrimSpace(fn)
	}
	return "", ""
}

func splitCallExpression(expr string) (qualifier, callee string) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", ""
	}
	idx := strings.LastIndex(expr, ".")
	if idx <= 0 {
		return "", expr
	}
	return strings.TrimSpace(expr[:idx]), strings.TrimSpace(expr[idx+1:])
}

type symbolIndex struct {
	funcs map[string]string
	any   map[symPair]string
}

type symPair struct {
	pkg, name string
}

func newSymbolIndex() *symbolIndex {
	return &symbolIndex{
		funcs: make(map[string]string),
		any:   make(map[symPair]string),
	}
}

func (sx *symbolIndex) ingestParseResult(pr *models.ParseResult) {
	pkg := findPackageImportPath(pr)
	for _, n := range pr.Nodes {
		// Index every symbol under BOTH the short package name (used in node
		// IDs and `n.Package`) AND the import path (used by resolveGoCall and
		// the signature-edge resolver). Either lookup path now hits.
		keys := []string{}
		if n.Package != "" {
			keys = append(keys, n.Package)
		}
		if pkg != "" && pkg != n.Package {
			keys = append(keys, pkg)
		}
		if len(keys) == 0 || n.ID == "" {
			continue
		}
		switch n.Type {
		case models.NodeFunction:
			for _, k := range keys {
				sx.funcs[symKeyFunc(k, n.Name)] = n.ID
				sx.any[symPair{k, n.Name}] = n.ID
			}
		case models.NodeMethod:
			mn := methodNameFromNode(n)
			for _, k := range keys {
				sx.any[symPair{k, mn}] = n.ID
			}
		case models.NodeStruct, models.NodeInterface, models.NodeType_:
			// Types are referenced by struct fields, function params/returns,
			// and var declarations — index them so the signature-edge resolver
			// can find them across packages.
			for _, k := range keys {
				sx.any[symPair{k, n.Name}] = n.ID
			}
		}
	}
}

func symKeyFunc(pkgPath, fname string) string {
	return pkgPath + "\x00" + fname
}

func methodNameFromNode(n models.Node) string {
	if mn := strings.TrimSpace(firstNonEmpty(metaGet(&n, "method"), metaGet(&n, "selector"))); mn != "" {
		return mn
	}
	sig := n.Signature
	if _, name := parseGoMethodSignature(sig); name != "" {
		return name
	}
	name := exportedNameToken(n.Name)
	return name
}

func (sx *symbolIndex) lookupFunc(pkgPath, name string) (string, bool) {
	id, ok := sx.funcs[symKeyFunc(pkgPath, name)]
	return id, ok
}

func (sx *symbolIndex) lookupAny(pkgPath, name string) (string, bool) {
	if id, ok := sx.lookupFunc(pkgPath, name); ok {
		return id, true
	}
	if id, ok := sx.any[symPair{pkgPath, name}]; ok {
		return id, true
	}
	return "", false
}

var rxGoMethod = regexp.MustCompile(`(?is)^func\s*\(\s*\w+\s*(\*?)(\w+)\s*\)\s*(\w+)\s*\(`)

func parseGoMethodSignature(sig string) (receiverBase, methodName string) {
	m := rxGoMethod.FindStringSubmatch(sig)
	if len(m) != 4 {
		return "", ""
	}
	return m[2], m[3]
}

type ifaceInfo struct {
	nodeID      string
	name        string
	pkgPath     string
	methodNames map[string]struct{}
}

type structInfo struct {
	nodeID  string
	name    string
	pkgPath string
	methods map[string]struct{}
}

func collectInterfaces(parseResults []*models.ParseResult) []ifaceInfo {
	var out []ifaceInfo
	rxName := regexp.MustCompile(`(?:^|;|\{|\n)\s*([A-Za-z_]\w*)\s*\(`)
	for _, pr := range parseResults {
		if pr == nil || !languageIsGo(pr) {
			continue
		}
		ip := findPackageImportPath(pr)
		for _, n := range pr.Nodes {
			if n.Type != models.NodeInterface {
				continue
			}
			pkg := ip
			if n.Package != "" {
				pkg = n.Package
			}
			names := map[string]struct{}{}
			for _, s := range splitCommaSep(firstNonEmpty(metaGet(&n, "methods"), metaGet(&n, "method_set"))) {
				if s != "" {
					names[s] = struct{}{}
				}
			}
			if len(names) == 0 {
				sig := strings.TrimSpace(n.Signature)
				if idx := strings.Index(sig, "interface"); idx >= 0 {
					inner := sig[idx:]
					for _, m := range rxName.FindAllStringSubmatch(inner, -1) {
						name := m[1]
						if isGoIfaceNoise(name) {
							continue
						}
						names[name] = struct{}{}
					}
				}
			}
			base := exportedNameToken(n.Name)
			out = append(out, ifaceInfo{
				nodeID:      n.ID,
				name:        base,
				pkgPath:     pkg,
				methodNames: names,
			})
		}
	}
	return out
}

func isGoIfaceNoise(s string) bool {
	switch s {
	case "interface", "func", "map", "chan", "struct", "const", "var", "type":
		return true
	default:
		return false
	}
}

func collectStructs(parseResults []*models.ParseResult) []structInfo {
	var out []structInfo
	for _, pr := range parseResults {
		if pr == nil || !languageIsGo(pr) {
			continue
		}
		ip := findPackageImportPath(pr)
		methodIndex := methodsByReceiver(pr)

		for _, n := range pr.Nodes {
			if n.Type != models.NodeStruct {
				continue
			}
			pkg := ip
			if n.Package != "" {
				pkg = n.Package
			}
			base := exportedNameToken(n.Name)
			mset := map[string]struct{}{}
			if m, ok := methodIndex[base]; ok {
				for k := range m {
					mset[k] = struct{}{}
				}
			}
			if star, ok := methodIndex["*"+base]; ok {
				for k := range star {
					mset[k] = struct{}{}
				}
			}
			out = append(out, structInfo{
				nodeID:  n.ID,
				name:    base,
				pkgPath: pkg,
				methods: mset,
			})
		}
	}
	return out
}

func methodsByReceiver(pr *models.ParseResult) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, n := range pr.Nodes {
		if n.Type != models.NodeMethod {
			continue
		}
		rb := firstNonEmpty(
			metaGet(&n, "receiver_base"),
			metaGet(&n, "receiver_type"),
			metaGet(&n, "receiver"),
		)
		rb = strings.TrimPrefix(rb, "*")
		rb = strings.TrimSpace(rb)
		if rb == "" {
			if m := rxGoMethod.FindStringSubmatch(n.Signature); len(m) == 4 {
				ptrRecv := m[1] == "*"
				b, mn := m[2], m[3]
				addReceiverMethod(out, b, mn)
				if ptrRecv {
					addReceiverMethod(out, "*"+b, mn)
				}
			}
			continue
		}
		method := firstNonEmpty(
			metaGet(&n, "method"),
			metaGet(&n, "selector"),
		)
		if method == "" {
			if _, mn := parseGoMethodSignature(n.Signature); mn != "" {
				method = mn
			} else if exportedNameToken(n.Name) != "" {
				method = exportedNameToken(n.Name)
			}
		}
		if method == "" {
			continue
		}
		addReceiverMethod(out, rb, method)
	}
	return out
}

func addReceiverMethod(out map[string]map[string]struct{}, receiver, method string) {
	if _, ok := out[receiver]; !ok {
		out[receiver] = map[string]struct{}{}
	}
	out[receiver][method] = struct{}{}
}

func implementsGo(structMethods, ifaceMethods map[string]struct{}) bool {
	if len(ifaceMethods) == 0 {
		return false
	}
	for m := range ifaceMethods {
		if _, ok := structMethods[m]; !ok {
			return false
		}
	}
	return true
}

func splitCommaSep(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (GoExtractor) addPackageDependencyEdges(
	result *models.ParseResult,
	pkgPathByFile map[string]string,
	importMapByFile map[string]map[string]string,
	pkgNodeByImportPath map[string]string,
	seen map[string]bool,
) {
	if result == nil {
		return
	}
	fromImports := aggregateImportPackages(result.FilePath, importMapByFile[result.FilePath])
	fromPkg := pkgPathForFile(pkgPathByFile, result.FilePath)
	if fromPkg == "" {
		fromPkg = findPackageImportPath(result)
	}
	if fromPkg == "" {
		return
	}

	fromPkgNode := pkgNodeByImportPath[fromPkg]

	for _, toImport := range fromImports {
		if toImport == fromPkg || toImport == "" {
			continue
		}
		toPkgNode := pkgNodeByImportPath[toImport]
		if fromPkgNode == "" || toPkgNode == "" {
			continue
		}
		key := edgeKey(fromPkgNode, toPkgNode, models.EdgeDependsOn)
		if seen[key] {
			continue
		}
		result.Edges = append(result.Edges, models.Edge{
			From: fromPkgNode,
			To:   toPkgNode,
			Type: models.EdgeDependsOn,
			Metadata: map[string]string{
				"language":      goLanguage,
				"scope":         "package",
				"import_path":   toImport,
				"from_import":   fromPkg,
				"source_file":   result.FilePath,
				"aggregate":     "imports",
				"package_level": "true",
			},
		})
		seen[key] = true
	}
}

func aggregateImportPackages(_ string, imports map[string]string) []string {
	if len(imports) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(imports))
	var out []string
	for _, ip := range imports {
		ip = strings.Trim(ip, `"`)
		if ip == "" {
			continue
		}
		if _, dup := uniq[ip]; dup {
			continue
		}
		uniq[ip] = struct{}{}
		out = append(out, ip)
	}
	return out
}

var _ Extractor = (*GoExtractor)(nil)
