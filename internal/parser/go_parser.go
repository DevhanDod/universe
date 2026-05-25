package parser

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Universe/universe/internal/models"
)

type GoParser struct{}

func NewGoParser() *GoParser {
	return &GoParser{}
}

func (*GoParser) Language() string { return "go" }

func (*GoParser) SupportedExtensions() []string { return []string{".go"} }

func (g *GoParser) Parse(filePath string, content []byte) (*models.ParseResult, error) {
	if g == nil {
		return nil, errors.New("go parser is nil")
	}
	if len(content) == 0 {
		return nil, errors.New("empty content")
	}

	res := &models.ParseResult{
		FilePath: filePath,
		Language: g.Language(),
	}

	fset := token.NewFileSet()
	fileName := filepath.Base(filePath)
	testFile := strings.HasSuffix(fileName, "_test.go")

	file, err := parser.ParseFile(fset, filePath, content, parser.AllErrors)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
	}
	if file == nil || file.Name == nil {
		if file == nil {
			return res, nil
		}
		res.Errors = append(res.Errors, "missing package clause")
		return res, nil
	}

	pkgName := file.Name.Name
	pkgID := makeGoNodeID(pkgName, fileName, pkgName)
	fileNodeID := fmt.Sprintf("%s:%s", pkgName, fileName)
	totalLines := lineCount(content)
	fileStructure := buildFileStructure(fset, content, file, pkgName)
	typeIDs := make(map[string]string)

	pkgMeta := map[string]string{}
	if testFile {
		pkgMeta["is_test"] = "true"
	}
	if isGoExported(pkgName) {
		pkgMeta["exported"] = "true"
	}
	pkgPos := fset.Position(file.Package)
	pkgEnd := fset.Position(file.Name.End())
	res.Nodes = append(res.Nodes, models.Node{
		ID:        pkgID,
		Name:      pkgName,
		Type:      models.NodePackage,
		FilePath:  filePath,
		Package:   pkgName,
		StartLine: pkgPos.Line,
		EndLine:   pkgEnd.Line,
		Metadata:  pkgMeta,
	})

	fileMeta := map[string]string{
		"content":        string(content),
		"total_lines":    strconv.Itoa(totalLines),
		"file_structure": fileStructure,
	}
	if testFile {
		fileMeta["is_test"] = "true"
	}
	res.Nodes = append(res.Nodes, models.Node{
		ID:        fileNodeID,
		Name:      fileName,
		Type:      models.NodeFile,
		FilePath:  filePath,
		Package:   pkgName,
		StartLine: 1,
		EndLine:   totalLines,
		Metadata:  fileMeta,
	})

	for _, imp := range file.Imports {
		path := importPathString(imp)
		if path == "" {
			continue
		}
		localName := importLocalNameAST(imp, path)
		id := makeGoNodeID(pkgName, fileName, localName)
		meta := map[string]string{"path": path}
		if imp.Name != nil {
			switch imp.Name.Name {
			case ".":
				// keep path as local name; no alias key needed to match prior behavior
			case "_":
				// blank
			default:
				meta["alias"] = imp.Name.Name
			}
		}
		if isGoExported(localName) {
			meta["exported"] = "true"
		}
		if testFile {
			meta["is_test"] = "true"
		}
		start := fset.Position(imp.Pos())
		end := fset.Position(imp.End())
		res.Nodes = append(res.Nodes, models.Node{
			ID:        id,
			Name:      localName,
			Type:      models.NodeImport,
			FilePath:  filePath,
			Package:   pkgName,
			StartLine: start.Line,
			EndLine:   end.Line,
			Metadata:  meta,
		})
		res.Edges = append(res.Edges, models.Edge{
			From: pkgID,
			To:   id,
			Type: models.EdgeImports,
		})
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			typeName := ts.Name.Name
			if typeName == "" {
				continue
			}
			id := makeGoNodeID(pkgName, fileName, typeName)
			typeIDs[typeName] = id

			var nt models.NodeType
			switch ts.Type.(type) {
			case *ast.StructType:
				nt = models.NodeStruct
			case *ast.InterfaceType:
				nt = models.NodeInterface
			default:
				nt = models.NodeType_
			}

			meta := map[string]string{}
			if testFile {
				meta["is_test"] = "true"
			}
			if isGoExported(typeName) {
				meta["exported"] = "true"
			}
			if gd.Doc != nil {
				meta["doc_comment"] = gd.Doc.Text()
			}
			start := fset.Position(ts.Pos())
			end := fset.Position(ts.End())
			res.Nodes = append(res.Nodes, models.Node{
				ID:        id,
				Name:      typeName,
				Type:      nt,
				FilePath:  filePath,
				Package:   pkgName,
				StartLine: start.Line,
				EndLine:   end.Line,
				Signature: sliceText(content, fset, ts.Pos(), ts.End()),
				Metadata:  meta,
			})
			res.Edges = append(res.Edges, models.Edge{
				From: pkgID,
				To:   id,
				Type: models.EdgeContains,
			})
			res.Edges = append(res.Edges, models.Edge{
				From: fileNodeID,
				To:   id,
				Type: models.EdgeContains,
			})
		}
	}

	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fd.Name.Name
		if name == "" {
			continue
		}
		id := makeGoNodeID(pkgName, fileName, name)
		meta := map[string]string{}
		if testFile {
			meta["is_test"] = "true"
		}
		if isGoExported(name) {
			meta["exported"] = "true"
		}
		if fd.Doc != nil {
			meta["doc_comment"] = fd.Doc.Text()
		}

		meta["body"] = byteSlice(content, fset, fd.Pos(), fd.End())

		var nodeType models.NodeType
		var recvBase string
		if fd.Recv == nil {
			nodeType = models.NodeFunction
		} else {
			nodeType = models.NodeMethod
			recvBase = recvBaseTypeName(fd.Recv)
			if recvBase != "" {
				meta["receiver"] = recvBase
			}
		}

		start := fset.Position(fd.Pos())
		endAST := fd.Body
		var endLine int
		if endAST != nil {
			endLine = fset.Position(endAST.End()).Line
		} else {
			endLine = fset.Position(fd.Type.End()).Line
		}

		res.Nodes = append(res.Nodes, models.Node{
			ID:        id,
			Name:      name,
			Type:      nodeType,
			FilePath:  filePath,
			Package:   pkgName,
			StartLine: start.Line,
			EndLine:   endLine,
			Signature: funcSignatureBytes(content, fset, fd),
			Metadata:  meta,
		})

		res.Edges = append(res.Edges, models.Edge{
			From: fileNodeID,
			To:   id,
			Type: models.EdgeContains,
		})

		if nodeType == models.NodeMethod && recvBase != "" {
			if toID, ok := typeIDs[recvBase]; ok {
				res.Edges = append(res.Edges, models.Edge{
					From: id,
					To:   toID,
					Type: models.EdgeReceives,
				})
			}
		}

		if fd.Body != nil {
			recordCallsAST(res, fd.Body, pkgName, fileName, id)
		}
	}

	return res, nil
}

func lineCount(content []byte) int {
	if len(content) == 0 {
		return 1
	}
	n := bytesCountNewlines(content)
	return n + 1
}

func bytesCountNewlines(b []byte) int {
	c := 0
	for _, ch := range b {
		if ch == '\n' {
			c++
		}
	}
	return c
}

func byteSlice(content []byte, fset *token.FileSet, start, end token.Pos) string {
	bp := fset.Position(start).Offset
	ep := fset.Position(end).Offset
	if bp < 0 || ep > len(content) || ep < bp {
		return ""
	}
	return string(content[bp:ep])
}

func buildFileStructure(fset *token.FileSet, content []byte, file *ast.File, pkgName string) string {
	var sections []string
	k := 1

	pkgPos := fset.Position(file.Package)
	sections = append(sections, fmt.Sprintf("%d. package %s (line %d)", k, pkgName, pkgPos.Line))
	k++

	if len(file.Imports) > 0 {
		importPaths := make([]string, 0, len(file.Imports))
		for _, imp := range file.Imports {
			if path := importPathString(imp); path != "" {
				importPaths = append(importPaths, path)
			}
		}
		firstLn := fset.Position(file.Imports[0].Pos()).Line
		lastLn := fset.Position(file.Imports[len(file.Imports)-1].End()).Line
		var linePart string
		if firstLn == lastLn {
			linePart = fmt.Sprintf("line %d", firstLn)
		} else {
			linePart = fmt.Sprintf("lines %d-%d", firstLn, lastLn)
		}
		sections = append(sections, fmt.Sprintf("%d. imports: %s (%s)", k, strings.Join(importPaths, ", "), linePart))
		k++
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sec := summarizeFuncSection(fset, content, d, k)
			k++
			sections = append(sections, sec)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Name.Name == "" {
					continue
				}
				sec := summarizeTypeSection(fset, ts, k)
				k++
				sections = append(sections, sec)
			}
		}
	}

	return strings.Join(sections, " | ")
}

func summarizeFuncSection(fset *token.FileSet, content []byte, fd *ast.FuncDecl, idx int) string {
	name := fd.Name.Name
	a, b := fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
	linePart := fmt.Sprintf("(lines %d-%d)", a, b)

	var headline string
	if fd.Recv == nil {
		headline = fmt.Sprintf("func %s %s", name, linePart)
	} else {
		recv := strings.TrimSpace(stringFormatRecv(content, fset, fd.Recv))
		headline = fmt.Sprintf("method %s on %s %s", name, recv, linePart)
	}
	callsParts := calleeSummary(fd.Body)
	if len(callsParts) == 0 {
		return fmt.Sprintf("%d. %s", idx, headline)
	}
	return fmt.Sprintf("%d. %s: %s", idx, headline, strings.Join(callsParts, ", "))
}

func stringFormatRecv(content []byte, fset *token.FileSet, fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 || fl.List[0].Type == nil {
		return ""
	}
	return sliceText(content, fset, fl.List[0].Type.Pos(), fl.List[0].Type.End())
}

func calleeSummary(body *ast.BlockStmt) []string {
	if body == nil {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := calleeExprString(call.Fun)
		if callee == "" || seen[callee] {
			return true
		}
		seen[callee] = true
		out = append(out, "calls "+callee)
		return true
	})
	return out
}

func summarizeTypeSection(fset *token.FileSet, ts *ast.TypeSpec, idx int) string {
	linePart := fmt.Sprintf("(lines %d-%d)", fset.Position(ts.Pos()).Line, fset.Position(ts.End()).Line)
	name := ts.Name.Name

	switch t := ts.Type.(type) {
	case *ast.StructType:
		fields := structFieldNames(t)
		if len(fields) == 0 {
			return fmt.Sprintf("%d. struct %s %s", idx, name, linePart)
		}
		return fmt.Sprintf("%d. struct %s %s: fields %s", idx, name, linePart, strings.Join(fields, ", "))
	case *ast.InterfaceType:
		methods := interfaceMethodNames(t)
		if len(methods) == 0 {
			return fmt.Sprintf("%d. interface %s %s", idx, name, linePart)
		}
		return fmt.Sprintf("%d. interface %s %s: methods %s", idx, name, linePart, strings.Join(methods, ", "))
	default:
		return fmt.Sprintf("%d. type %s %s", idx, name, linePart)
	}
}

func structFieldNames(st *ast.StructType) []string {
	if st.Fields == nil {
		return nil
	}
	var names []string
	for _, f := range st.Fields.List {
		if len(f.Names) > 0 {
			for _, id := range f.Names {
				if id.Name != "" {
					names = append(names, id.Name)
				}
			}
			continue
		}
		emb := typeExprEmbeddedName(f.Type)
		if emb != "" {
			names = append(names, emb)
		}
	}
	return names
}

func typeExprEmbeddedName(expr ast.Expr) string {
	return strings.TrimSpace(typeExprStringForEmbedded(expr))
}

func typeExprStringForEmbedded(expr ast.Expr) string {
	expr = stripParen(expr)
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeExprStringForEmbedded(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + typeExprStringForEmbedded(t.X)
	default:
		return ""
	}
}

func interfaceMethodNames(iface *ast.InterfaceType) []string {
	if iface.Methods == nil {
		return nil
	}
	var names []string
	for _, f := range iface.Methods.List {
		for _, id := range f.Names {
			if id.Name != "" {
				names = append(names, id.Name)
			}
		}
	}
	return names
}

func makeGoNodeID(pkg, fileName, name string) string {
	return fmt.Sprintf("%s:%s:%s", pkg, fileName, name)
}

func isGoExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsLetter(r) && unicode.IsUpper(r)
}

func importPathString(spec *ast.ImportSpec) string {
	if spec.Path == nil {
		return ""
	}
	raw := spec.Path.Value
	if s, err := strconv.Unquote(raw); err == nil && s != "" {
		return s
	}
	return strings.TrimSpace(strings.Trim(raw, "`\""))
}

func importLocalNameAST(spec *ast.ImportSpec, path string) string {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 && i+1 < len(path) {
		base = path[i+1:]
	}
	if spec.Name == nil {
		return base
	}
	switch spec.Name.Name {
	case ".":
		return path
	case "_":
		return base
	default:
		return spec.Name.Name
	}
}

func recvBaseTypeName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 || fl.List[0].Type == nil {
		return ""
	}
	return typeExprBaseName(fl.List[0].Type)
}

func typeExprBaseName(expr ast.Expr) string {
	expr = stripParen(expr)
	switch t := expr.(type) {
	case *ast.StarExpr:
		return typeExprBaseName(t.X)
	case *ast.IndexExpr:
		return typeExprBaseName(t.X)
	case *ast.IndexListExpr:
		return typeExprBaseName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func stripParen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	return e
}

func sliceText(content []byte, fset *token.FileSet, start, end token.Pos) string {
	bp := fset.Position(start).Offset
	ep := fset.Position(end).Offset
	if bp < 0 || ep > len(content) || ep < bp {
		return ""
	}
	return strings.TrimSpace(string(content[bp:ep]))
}

func funcSignatureBytes(content []byte, fset *token.FileSet, fd *ast.FuncDecl) string {
	start := fd.Pos()
	var end token.Pos
	if fd.Body != nil {
		end = fd.Body.Pos()
	} else {
		end = fd.Type.End()
	}
	return sliceText(content, fset, start, end)
}

func recordCallsAST(res *models.ParseResult, body ast.Node, pkgName, fileName, fromID string) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := calleeExprString(call.Fun)
		if callee == "" {
			return true
		}
		res.Edges = append(res.Edges, models.Edge{
			From: fromID,
			To:   makeGoNodeID(pkgName, fileName, callee),
			Type: models.EdgeCalls,
			Metadata: map[string]string{
				"callee_expression": callee,
			},
		})
		return true
	})
}

func calleeExprString(fun ast.Expr) string {
	fun = stripParen(fun)
	switch f := fun.(type) {
	case *ast.Ident:
		return strings.TrimSpace(f.Name)
	case *ast.SelectorExpr:
		left := calleeExprString(f.X)
		right := strings.TrimSpace(f.Sel.Name)
		if left == "" {
			return right
		}
		return left + "." + right
	default:
		return ""
	}
}

var _ Parser = (*GoParser)(nil)
