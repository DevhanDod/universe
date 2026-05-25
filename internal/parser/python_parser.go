//go:build cgo

package parser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Universe/universe/internal/models"
	sitter "github.com/smacker/go-tree-sitter"
	pythonlang "github.com/smacker/go-tree-sitter/python"
)

type PythonParser struct{}

func NewPythonParser() *PythonParser {
	return &PythonParser{}
}

func (*PythonParser) Language() string {
	return "python"
}

func (*PythonParser) SupportedExtensions() []string {
	return []string{".py"}
}

func (*PythonParser) Parse(filePath string, content []byte) (*models.ParseResult, error) {
	p := sitter.NewParser()
	p.SetLanguage(pythonlang.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	modKey := dottedPathFromDir(filePath)
	stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	ps := &parseState{
		filePath: filePath,
		content:  content,
		modKey:   modKey,
		stem:     stem,
		binds:    make(map[string]string),
	}

	ps.fileID = ps.nodeID("__file__")
	fileMeta := map[string]string{}
	if isTestPyFile(filePath) {
		fileMeta["is_test"] = "true"
	}
	ps.addNode(models.Node{
		ID:        ps.fileID,
		Name:      filepath.Base(filePath),
		Type:      models.NodeFile,
		FilePath:  filePath,
		Package:   modKey,
		StartLine: 1 + int(root.StartPoint().Row),
		EndLine:   1 + int(root.EndPoint().Row),
		Metadata:  fileMeta,
	})

	ps.collectStructuralErrors(root)
	ps.walkModuleStatements(root)

	return &models.ParseResult{
		FilePath: filePath,
		Language: "python",
		Nodes:    ps.nodes,
		Edges:    ps.edges,
		Errors:   ps.errs,
	}, nil
}

type parseState struct {
	filePath string
	content  []byte
	modKey   string
	stem     string
	fileID   string

	nodes []models.Node
	edges []models.Edge
	errs  []string

	binds map[string]string
}

func dottedPathFromDir(filePath string) string {
	dir := filepath.Clean(filepath.Dir(filePath))
	if dir == "." {
		return "."
	}
	var parts []string
	for dir != "." && dir != "" {
		base := filepath.Base(dir)
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		parts = append([]string{base}, parts...)
		dir = next
	}
	return strings.Join(parts, ".")
}

func isTestPyFile(path string) bool {
	base := filepath.Base(path)
	if filepath.Ext(base) != ".py" {
		return false
	}
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(strings.TrimSuffix(base, ".py"), "_test")
}

func (ps *parseState) nodeID(parts ...string) string {
	body := strings.Join(parts, "|")
	body = strings.ReplaceAll(body, ":", "_")
	return fmt.Sprintf("%s:%s:%s", ps.modKey, ps.stem, body)
}

func (ps *parseState) foreignID(fragment string) string {
	frag := strings.ReplaceAll(strings.ReplaceAll(fragment, ":", "_"), "|", "_")
	return ps.nodeID("ext", frag)
}

func (ps *parseState) addNode(n models.Node) {
	ps.nodes = append(ps.nodes, n)
}

func (ps *parseState) addEdge(e models.Edge) {
	ps.edges = append(ps.edges, e)
}

func (ps *parseState) bind(name, id string) {
	if name == "" {
		return
	}
	ps.binds[name] = id
}

func (ps *parseState) resolveSymbol(name string, classHints ...string) string {
	if id, ok := ps.binds[name]; ok {
		return id
	}
	for _, cn := range classHints {
		if cn == "" {
			continue
		}
		if id, ok := ps.binds[cn+"."+name]; ok {
			return id
		}
	}
	return ""
}

func defMetadata(name string, base map[string]string) map[string]string {
	var md map[string]string
	if base != nil {
		md = make(map[string]string, len(base)+1)
		for k, v := range base {
			md[k] = v
		}
	} else {
		md = make(map[string]string, 1)
	}
	if !strings.HasPrefix(name, "_") {
		md["exported"] = "true"
	}
	return md
}

func (ps *parseState) collectStructuralErrors(n *sitter.Node) {
	if n == nil {
		return
	}
	if n.Type() == "ERROR" || n.IsMissing() {
		ps.errs = append(ps.errs,
			fmt.Sprintf("syntax issue at %d:%d (%s)", 1+int(n.StartPoint().Row), int(n.StartPoint().Column), n.Type()))
	}
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		ps.collectStructuralErrors(n.NamedChild(int(i)))
	}
}

func (ps *parseState) walkModuleStatements(mod *sitter.Node) {
	if mod == nil {
		return
	}
	for i := uint32(0); i < mod.NamedChildCount(); i++ {
		ps.visitModuleStatement(mod.NamedChild(int(i)))
	}
}

func (ps *parseState) visitModuleStatement(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "import_statement":
		ps.handleImportStatement(n)
	case "import_from_statement":
		ps.handleImportFromStatement(n)
	case "future_import_statement":
		ps.handleFutureImport(n)
	case "function_definition":
		ps.handleFunction(n, nil, "", ps.fileID, "")
	case "class_definition":
		ps.handleClass(n, nil, ps.fileID)
	case "decorated_definition":
		ps.handleDecorated(n, ps.fileID, "")
	case "expression_statement":
		ps.handleExpressionStatementModule(n)
	}
}

func (ps *parseState) handleExpressionStatementModule(n *sitter.Node) {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(int(i))
		if c.Type() == "assignment" {
			ps.handleModuleAssignment(c)
		}
	}
}

func (ps *parseState) handleModuleAssignment(assign *sitter.Node) {
	lhs := assign.ChildByFieldName("left")
	if lhs == nil {
		return
	}
	var names []string
	ps.collectAssignmentNames(lhs, &names)
	for _, nm := range names {
		ps.emitVariable(nm, assign, ps.fileID)
	}
}

func (ps *parseState) collectAssignmentNames(n *sitter.Node, out *[]string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "identifier":
		*out = append(*out, n.Content(ps.content))
	case "pattern_list", "tuple_pattern":
		for i := uint32(0); i < n.NamedChildCount(); i++ {
			ps.collectAssignmentNames(n.NamedChild(int(i)), out)
		}
	default:
		for i := uint32(0); i < n.NamedChildCount(); i++ {
			ps.collectAssignmentNames(n.NamedChild(int(i)), out)
		}
	}
}

func (ps *parseState) emitVariable(name string, decl *sitter.Node, container string) {
	id := ps.nodeID(name)
	md := defMetadata(name, nil)
	ps.addNode(models.Node{
		ID:        id,
		Name:      name,
		Type:      models.NodeVariable,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: 1 + int(decl.StartPoint().Row),
		EndLine:   1 + int(decl.EndPoint().Row),
		Signature: strings.TrimSpace(headerBeforeBody(decl, ps.content)),
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: container, To: id, Type: models.EdgeContains})
	ps.bind(name, id)
}

type importContext struct {
	style    string
	fromMod  string
	stmtNode *sitter.Node
}

func (ps *parseState) handleImportFromStatement(stmt *sitter.Node) {
	modNode := stmt.ChildByFieldName("module_name")
	from := ""
	if modNode != nil {
		from = strings.TrimSpace(modNode.Content(ps.content))
	}
	ctx := importContext{style: "from", fromMod: from, stmtNode: stmt}
	ps.eachImportBindingSkipModule(stmt, modNode, ctx)
}

func (ps *parseState) eachImportBindingSkipModule(root, moduleField *sitter.Node, ctx importContext) {
	var walk func(*sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if moduleField != nil && sameNode(x, moduleField) {
			return
		}
		switch x.Type() {
		case "dotted_name":
			sym := strings.TrimSpace(x.Content(ps.content))
			ps.emitFromImport(ctx, sym, sym)
			return
		case "aliased_import":
			dn := x.ChildByFieldName("name")
			al := x.ChildByFieldName("alias")
			if dn == nil {
				return
			}
			sym := strings.TrimSpace(dn.Content(ps.content))
			bind := sym
			if al != nil {
				bind = strings.TrimSpace(al.Content(ps.content))
			}
			ps.emitFromImport(ctx, sym, bind)
			return
		case "wildcard_import":
			ps.emitWildcardImport(ctx)
			return
		}
		for i := uint32(0); i < x.NamedChildCount(); i++ {
			walk(x.NamedChild(int(i)))
		}
	}
	walk(root)
}

func sameNode(a, b *sitter.Node) bool {
	if a == nil || b == nil {
		return false
	}
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}

func (ps *parseState) handleFutureImport(stmt *sitter.Node) {
	ctx := importContext{style: "future", fromMod: "__future__", stmtNode: stmt}
	ps.eachImportBindingSkipModule(stmt, nil, ctx)
}

func (ps *parseState) emitPlainImport(module string, ctx importContext) {
	bind := firstSegment(module)
	id := ps.nodeID("import", "module", module)
	md := map[string]string{"kind": ctx.style, "module": module}
	start, end := ps.stmtLines(ctx.stmtNode)
	ps.addNode(models.Node{
		ID:        id,
		Name:      module,
		Type:      models.NodeImport,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: start,
		EndLine:   end,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: ps.fileID, To: id, Type: models.EdgeImports})
	ps.bind(bind, id)
}

func (ps *parseState) emitPlainImportAliased(module, bind string, ctx importContext) {
	id := ps.nodeID("import", "module", module, "as", bind)
	md := map[string]string{"kind": ctx.style, "module": module, "alias": bind}
	start, end := ps.stmtLines(ctx.stmtNode)
	ps.addNode(models.Node{
		ID:        id,
		Name:      module + " as " + bind,
		Type:      models.NodeImport,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: start,
		EndLine:   end,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: ps.fileID, To: id, Type: models.EdgeImports})
	ps.bind(bind, id)
}

func (ps *parseState) emitFromImport(ctx importContext, symbol, bind string) {
	id := ps.nodeID("import", "from", ctx.fromMod, symbol)
	md := map[string]string{
		"kind":    ctx.style,
		"module":  ctx.fromMod,
		"symbol":  symbol,
		"binding": bind,
	}
	start, end := ps.stmtLines(ctx.stmtNode)
	ps.addNode(models.Node{
		ID:        id,
		Name:      symbol + " from " + ctx.fromMod,
		Type:      models.NodeImport,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: start,
		EndLine:   end,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: ps.fileID, To: id, Type: models.EdgeImports})
	ps.bind(bind, id)
}

func (ps *parseState) emitWildcardImport(ctx importContext) {
	id := ps.nodeID("import", "wildcard", ctx.fromMod)
	md := map[string]string{"kind": ctx.style, "module": ctx.fromMod, "wildcard": "true"}
	start, end := ps.stmtLines(ctx.stmtNode)
	ps.addNode(models.Node{
		ID:        id,
		Name:      "* from " + ctx.fromMod,
		Type:      models.NodeImport,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: start,
		EndLine:   end,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: ps.fileID, To: id, Type: models.EdgeImports})
}

func (ps *parseState) stmtLines(s *sitter.Node) (int, int) {
	if s == nil {
		return 0, 0
	}
	return 1 + int(s.StartPoint().Row), 1 + int(s.EndPoint().Row)
}

func firstSegment(dotted string) string {
	if i := strings.IndexByte(dotted, '.'); i >= 0 {
		return dotted[:i]
	}
	return dotted
}

func (ps *parseState) handleImportStatement(stmt *sitter.Node) {
	ctx := importContext{style: "import", stmtNode: stmt}
	ps.eachImportBinding(stmt, ctx)
}

func (ps *parseState) handleDecorated(dec *sitter.Node, parentContain, className string) {
	var decTexts []string
	var def *sitter.Node
	for i := uint32(0); i < dec.NamedChildCount(); i++ {
		ch := dec.NamedChild(int(i))
		switch ch.Type() {
		case "decorator":
			if t := decoratorExpression(ch, ps.content); t != "" {
				decTexts = append(decTexts, t)
			}
		case "class_definition", "function_definition":
			def = ch
		}
	}
	var decMeta map[string]string
	if len(decTexts) > 0 {
		decMeta = map[string]string{"decorators": strings.Join(decTexts, "\n")}
	}
	if def == nil {
		return
	}
	switch def.Type() {
	case "function_definition":
		ps.handleFunction(def, decMeta, className, parentContain, "")
	case "class_definition":
		ps.handleClass(def, decMeta, parentContain)
	}
}

func decoratorExpression(d *sitter.Node, src []byte) string {
	for i := uint32(0); i < d.NamedChildCount(); i++ {
		c := d.NamedChild(int(i))
		txt := strings.TrimSpace(c.Content(src))
		if txt != "" && txt != "@" {
			return txt
		}
	}
	return ""
}

func (ps *parseState) handleClass(class *sitter.Node, decMeta map[string]string, parentContain string) {
	name := firstFunctionOrClassName(class, ps.content)
	if name == "" {
		return
	}
	id := ps.nodeID(name)
	md := defMetadata(name, decMeta)
	sig := strings.TrimSpace(headerBeforeBody(class, ps.content))
	ps.addNode(models.Node{
		ID:        id,
		Name:      name,
		Type:      models.NodeClass,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: 1 + int(class.StartPoint().Row),
		EndLine:   1 + int(class.EndPoint().Row),
		Signature: sig,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: parentContain, To: id, Type: models.EdgeContains})
	ps.bind(name, id)

	argList := class.ChildByFieldName("superclasses")
	if argList != nil && argList.Type() == "argument_list" {
		ps.emitInheritanceEdges(id, argList)
	}

	body := suiteBlock(class.ChildByFieldName("body"))
	ps.walkClassBody(body, id, name)
}

func (ps *parseState) emitInheritanceEdges(classID string, argList *sitter.Node) {
	for i := uint32(0); i < argList.NamedChildCount(); i++ {
		arg := argList.NamedChild(int(i))
		if arg.Type() == "," {
			continue
		}
		if arg.Type() == "keyword_argument" {
			continue
		}
		baseExpr := strings.TrimSpace(arg.Content(ps.content))
		base := primaryToNameHint(arg, ps.content)
		if base == "" {
			base = baseExpr
		}
		parentID := ps.resolveSuperclass(base)
		ps.addEdge(models.Edge{
			From:     classID,
			To:       parentID,
			Type:     models.EdgeInherits,
			Metadata: map[string]string{"base_expression": baseExpr},
		})
	}
}

func (ps *parseState) resolveSuperclass(base string) string {
	if id := ps.resolveSymbol(base); id != "" {
		return id
	}
	return ps.foreignID("class:" + base)
}

func (ps *parseState) walkClassBody(body *sitter.Node, classID, className string) {
	if body == nil {
		return
	}
	ps.scanCalls(body, classID, "", className)
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		st := body.NamedChild(int(i))
		switch st.Type() {
		case "decorated_definition":
			ps.handleDecorated(st, classID, className)
		case "function_definition":
			ps.handleFunction(st, nil, className, classID, "")
		case "expression_statement":
			for j := uint32(0); j < st.NamedChildCount(); j++ {
				c := st.NamedChild(int(j))
				if c.Type() == "assignment" {
					ps.scanCalls(c, classID, "", className)
				}
			}
		default:
			ps.scanCalls(st, classID, "", className)
		}
	}
}

func (ps *parseState) handleFunction(fn *sitter.Node, decMeta map[string]string, className, parentContain, qualPrefix string) {
	name := firstFunctionOrClassName(fn, ps.content)
	if name == "" {
		return
	}
	qualified := name
	if qualPrefix != "" {
		qualified = qualPrefix + "." + name
	}
	var id string
	var ntype models.NodeType
	var bindKey string
	if className != "" {
		ntype = models.NodeMethod
		qn := className + "." + name
		id = ps.nodeID(qn)
		bindKey = qn
	} else {
		ntype = models.NodeFunction
		id = ps.nodeID(qualified)
		bindKey = qualified
	}
	md := defMetadata(name, decMeta)
	sig := strings.TrimSpace(headerBeforeBody(fn, ps.content))
	ps.addNode(models.Node{
		ID:        id,
		Name:      name,
		Type:      ntype,
		FilePath:  ps.filePath,
		Package:   ps.modKey,
		StartLine: 1 + int(fn.StartPoint().Row),
		EndLine:   1 + int(fn.EndPoint().Row),
		Signature: sig,
		Metadata:  md,
	})
	ps.addEdge(models.Edge{From: parentContain, To: id, Type: models.EdgeContains})
	ps.bind(bindKey, id)
	if className == "" {
		ps.bind(name, id)
	}

	body := suiteBlock(fn.ChildByFieldName("body"))
	ps.walkFunctionBody(body, id, qualified, className)
}

func (ps *parseState) walkFunctionBody(body *sitter.Node, fnID, qualName, enclosingClass string) {
	if body == nil {
		return
	}
	ps.scanCalls(body, fnID, qualName, enclosingClass)
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		st := body.NamedChild(int(i))
		switch st.Type() {
		case "decorated_definition":
			ps.handleDecoratedInFunction(st, fnID, qualName)
		case "function_definition":
			ps.handleFunction(st, nil, "", fnID, qualName)
		case "class_definition":
			ps.handleClass(st, nil, fnID)
		default:
			ps.scanCalls(st, fnID, qualName, enclosingClass)
		}
	}
}

func (ps *parseState) handleDecoratedInFunction(dec *sitter.Node, parentFn, qualPrefix string) {
	var decTexts []string
	var def *sitter.Node
	for i := uint32(0); i < dec.NamedChildCount(); i++ {
		ch := dec.NamedChild(int(i))
		switch ch.Type() {
		case "decorator":
			if t := decoratorExpression(ch, ps.content); t != "" {
				decTexts = append(decTexts, t)
			}
		case "class_definition", "function_definition":
			def = ch
		}
	}
	var decMeta map[string]string
	if len(decTexts) > 0 {
		decMeta = map[string]string{"decorators": strings.Join(decTexts, "\n")}
	}
	if def == nil {
		return
	}
	switch def.Type() {
	case "function_definition":
		ps.handleFunction(def, decMeta, "", parentFn, qualPrefix)
	case "class_definition":
		ps.handleClass(def, decMeta, parentFn)
	}
}

func (ps *parseState) scanCalls(n *sitter.Node, callerID, callerQual, classCtx string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "function_definition", "class_definition", "decorated_definition":
		return
	}
	if n.Type() == "call" {
		ps.emitCallEdge(n, callerID, callerQual, classCtx)
	}
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		ps.scanCalls(n.NamedChild(int(i)), callerID, callerQual, classCtx)
	}
}

func (ps *parseState) emitCallEdge(call *sitter.Node, callerID, callerQual, classCtx string) {
	rawFn := call.ChildByFieldName("function")
	if rawFn == nil && call.NamedChildCount() > 0 {
		rawFn = call.NamedChild(0)
	}
	if rawFn == nil {
		return
	}
	fn := peelPrimary(rawFn)
	targets := callTargetKeys(fn, ps.content)
	var toID string
	for _, tk := range targets {
		toID = ps.resolveCall(tk, classCtx)
		if toID != "" {
			break
		}
	}
	if toID == "" && len(targets) > 0 {
		toID = ps.foreignID("call:" + targets[0])
	}
	if toID == "" {
		return
	}
	exprMeta := strings.TrimSpace(rawFn.Content(ps.content))
	ps.addEdge(models.Edge{
		From:     callerID,
		To:       toID,
		Type:     models.EdgeCalls,
		Metadata: map[string]string{"callee_expression": exprMeta},
	})
}

func (ps *parseState) resolveCall(key string, classCtx string) string {
	if key == "" {
		return ""
	}
	if id := ps.resolveSymbol(key, classCtx); id != "" {
		return id
	}
	if i := strings.LastIndexByte(key, '.'); i > 0 {
		suffix := key[i+1:]
		if id := ps.resolveSymbol(suffix, classCtx); id != "" {
			return id
		}
	}
	return ""
}

func callTargetKeys(fn *sitter.Node, src []byte) []string {
	switch fn.Type() {
	case "identifier":
		s := fn.Content(src)
		return []string{s}
	case "attribute":
		chain := attributeChain(fn, src)
		if len(chain) == 0 {
			return nil
		}
		full := strings.Join(chain, ".")
		return []string{full, chain[len(chain)-1]}
	default:
		return nil
	}
}

func attributeChain(n *sitter.Node, src []byte) []string {
	var parts []string
	cur := n
	for cur != nil && cur.Type() == "attribute" {
		attr := cur.ChildByFieldName("attribute")
		if attr == nil {
			break
		}
		parts = append([]string{attr.Content(src)}, parts...)
		cur = cur.ChildByFieldName("object")
	}
	if cur != nil && cur.Type() == "identifier" {
		parts = append([]string{cur.Content(src)}, parts...)
	}
	return parts
}

func peelPrimary(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	cur := n
	for cur.Type() == "parenthesized_expression" && cur.NamedChildCount() > 0 {
		cur = cur.NamedChild(0)
	}
	return cur
}

func primaryToNameHint(expr *sitter.Node, src []byte) string {
	x := peelPrimary(expr)
	if x == nil {
		return ""
	}
	if x.Type() == "identifier" {
		return strings.TrimSpace(x.Content(src))
	}
	return strings.TrimSpace(expr.Content(src))
}

func firstFunctionOrClassName(fn *sitter.Node, src []byte) string {
	if id := fn.ChildByFieldName("name"); id != nil {
		return strings.TrimSpace(id.Content(src))
	}
	for i := uint32(0); i < fn.NamedChildCount(); i++ {
		ch := fn.NamedChild(int(i))
		if ch.Type() == "identifier" {
			return strings.TrimSpace(ch.Content(src))
		}
	}
	return ""
}

func suiteBlock(body *sitter.Node) *sitter.Node {
	if body == nil {
		return nil
	}
	if body.Type() == "block" {
		return body
	}
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		ch := body.NamedChild(int(i))
		if ch.Type() == "block" {
			return ch
		}
	}
	return body
}

func headerBeforeBody(n *sitter.Node, src []byte) string {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		ch := n.NamedChild(int(i))
		if ch.Type() == "block" {
			return string(src[n.StartByte():ch.StartByte()])
		}
	}
	return n.Content(src)
}

func (ps *parseState) eachImportBinding(root *sitter.Node, ctx importContext) {
	var walk func(*sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "dotted_name":
			mod := strings.TrimSpace(x.Content(ps.content))
			ps.emitPlainImport(mod, ctx)
			return
		case "aliased_import":
			dn := x.ChildByFieldName("name")
			al := x.ChildByFieldName("alias")
			if dn == nil {
				return
			}
			mod := strings.TrimSpace(dn.Content(ps.content))
			bind := firstSegment(mod)
			if al != nil {
				bind = strings.TrimSpace(al.Content(ps.content))
			}
			ps.emitPlainImportAliased(mod, bind, ctx)
			return
		case "wildcard_import":
			ps.emitWildcardImport(ctx)
			return
		}
		for i := uint32(0); i < x.NamedChildCount(); i++ {
			walk(x.NamedChild(int(i)))
		}
	}
	walk(root)
}

var _ Parser = (*PythonParser)(nil)
