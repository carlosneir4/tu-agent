package extract

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/tu/tu-agent/internal/graph"
)

// ParseGo extracts FileFacts from one Go source file. Pure function.
// Package is set to the file's directory: in Go, one directory is one package,
// so this groups same-package files correctly for resolution.
func ParseGo(relPath string, src []byte) (*FileFacts, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return nil, fmt.Errorf("extract.ParseGo: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("extract.ParseGo: tree-sitter returned nil")
	}
	defer tree.Close()

	pkgDir := filepath.ToSlash(filepath.Dir(relPath))
	if pkgDir == "." {
		pkgDir = ""
	}
	f := &FileFacts{Meta: graph.FileMeta{Path: relPath, Language: "go", Status: "ok", Package: pkgDir}}
	f.Nodes = append(f.Nodes, graph.Node{ID: relPath, Kind: graph.KindFile, Name: relPath, Path: relPath, Language: "go"})

	isTest := strings.HasSuffix(relPath, "_test.go")
	seen := map[string]bool{relPath: true}
	root := tree.RootNode()
	for i := uint(0); i < root.NamedChildCount(); i++ {
		n := root.NamedChild(i)
		switch n.Kind() {
		case "import_declaration":
			collectGoImports(f, n, src)
		case "type_declaration":
			for j := uint(0); j < n.NamedChildCount(); j++ {
				if spec := n.NamedChild(j); spec.Kind() == "type_spec" {
					goTypeSpec(f, spec, src, relPath, seen, isTest)
				}
			}
		case "function_declaration":
			goFunc(f, n, src, relPath, seen, isTest)
		case "method_declaration":
			goMethod(f, n, src, relPath, seen, isTest)
		}
	}
	return f, nil
}

func collectGoImports(f *FileFacts, node *tree_sitter.Node, src []byte) {
	var visit func(*tree_sitter.Node)
	visit = func(nd *tree_sitter.Node) {
		if nd.Kind() == "import_spec" {
			if p := nd.ChildByFieldName("path"); p != nil {
				if imp := strings.Trim(p.Utf8Text(src), "\"`"); imp != "" {
					f.Meta.Imports = append(f.Meta.Imports, imp)
				}
			}
			return
		}
		for i := uint(0); i < nd.NamedChildCount(); i++ {
			visit(nd.NamedChild(i))
		}
	}
	visit(node)
}

func goTypeSpec(f *FileFacts, spec *tree_sitter.Node, src []byte, relPath string, seen map[string]bool, isTest bool) {
	nameNode := spec.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nameNode.Utf8Text(src)
	id := relPath + "::" + name
	if seen[id] {
		return
	}
	seen[id] = true
	kind := graph.KindClass
	if isTest {
		kind = graph.KindTest
	}
	f.Nodes = append(f.Nodes, graph.Node{
		ID: id, Kind: kind, Name: name, Path: relPath,
		Line: int(spec.StartPosition().Row) + 1, EndLine: int(spec.EndPosition().Row) + 1, Language: "go",
	})
	f.Contains = append(f.Contains, graph.Edge{From: relPath, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	if t := spec.ChildByFieldName("type"); t != nil {
		collectGoEmbeds(f, t, src, id)
	}
}

func collectGoEmbeds(f *FileFacts, typeNode *tree_sitter.Node, src []byte, fromID string) {
	switch typeNode.Kind() {
	case "struct_type":
		// The Go grammar wraps field declarations in a field_declaration_list
		// node; there is no "body" field name as in the Java grammar.
		for i := uint(0); i < typeNode.NamedChildCount(); i++ {
			fdl := typeNode.NamedChild(i)
			if fdl.Kind() != "field_declaration_list" {
				continue
			}
			for j := uint(0); j < fdl.NamedChildCount(); j++ {
				fd := fdl.NamedChild(j)
				if fd.Kind() != "field_declaration" || fd.ChildByFieldName("name") != nil {
					continue // named field, not an embed
				}
				if t := fd.ChildByFieldName("type"); t != nil {
					if tn := goTypeName(t, src); tn != "" {
						f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: graph.EdgeExtends, Name: tn, Line: int(fd.StartPosition().Row) + 1})
					}
				}
			}
		}
	case "interface_type":
		for i := uint(0); i < typeNode.NamedChildCount(); i++ {
			c := typeNode.NamedChild(i)
			if c.Kind() == "type_identifier" || c.Kind() == "qualified_type" || c.Kind() == "type_elem" {
				if tn := goTypeName(c, src); tn != "" {
					f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: graph.EdgeExtends, Name: tn, Line: int(c.StartPosition().Row) + 1})
				}
			}
		}
	}
}

func goTypeName(n *tree_sitter.Node, src []byte) string {
	switch n.Kind() {
	case "type_identifier":
		return n.Utf8Text(src)
	case "qualified_type":
		if t := n.ChildByFieldName("name"); t != nil {
			return t.Utf8Text(src)
		}
	case "pointer_type", "generic_type", "type_elem":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if tn := goTypeName(n.NamedChild(i), src); tn != "" {
				return tn
			}
		}
	}
	return ""
}

// goSignature reads the parameter list and result of a function or method
// declaration. The receiver is not part of Params.
func goSignature(node *tree_sitter.Node, src []byte) (params, ret string) {
	if p := node.ChildByFieldName("parameters"); p != nil {
		params = normalizeSig(p.Utf8Text(src))
	}
	if r := node.ChildByFieldName("result"); r != nil {
		ret = normalizeSig(r.Utf8Text(src))
	}
	return params, ret
}

func goReceiverType(recv *tree_sitter.Node, src []byte) string {
	var out string
	var visit func(*tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if out != "" {
			return
		}
		if n.Kind() == "type_identifier" {
			out = n.Utf8Text(src)
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(recv)
	return out
}

// goExported reports whether the symbol's own name — the part after the
// last dot for "Type.Method" names — starts with an uppercase letter.
func goExported(name string) bool {
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func goFunc(f *FileFacts, node *tree_sitter.Node, src []byte, relPath string, seen map[string]bool, isTest bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nameNode.Utf8Text(src)
	id := relPath + "::" + name
	if seen[id] {
		return
	}
	seen[id] = true
	kind := graph.KindFunction
	if isTest && strings.HasPrefix(name, "Test") {
		kind = graph.KindTest
	}
	params, ret := goSignature(node, src)
	f.Nodes = append(f.Nodes, graph.Node{
		ID: id, Kind: kind, Name: name, Path: relPath,
		Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "go",
		Params: params, ReturnType: ret, Exported: goExported(name),
	})
	f.Contains = append(f.Contains, graph.Edge{From: relPath, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	if body := node.ChildByFieldName("body"); body != nil {
		walkGoCalls(f, body, src, id)
	}
}

func goMethod(f *FileFacts, node *tree_sitter.Node, src []byte, relPath string, seen map[string]bool, isTest bool) {
	nameNode := node.ChildByFieldName("name")
	recv := node.ChildByFieldName("receiver")
	if nameNode == nil || recv == nil {
		return
	}
	owner := goReceiverType(recv, src)
	if owner == "" {
		return
	}
	mname := owner + "." + nameNode.Utf8Text(src)
	id := relPath + "::" + mname
	if seen[id] {
		return
	}
	seen[id] = true
	kind := graph.KindFunction
	if isTest {
		kind = graph.KindTest
	}
	params, ret := goSignature(node, src)
	f.Nodes = append(f.Nodes, graph.Node{
		ID: id, Kind: kind, Name: mname, Path: relPath,
		Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "go",
		Params: params, ReturnType: ret, Exported: goExported(mname),
	})
	f.Contains = append(f.Contains, graph.Edge{From: relPath + "::" + owner, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	if body := node.ChildByFieldName("body"); body != nil {
		walkGoCalls(f, body, src, id)
	}
}

func walkGoCalls(f *FileFacts, node *tree_sitter.Node, src []byte, fromID string) {
	if node.Kind() == "call_expression" {
		if fn := node.ChildByFieldName("function"); fn != nil {
			name := ""
			switch fn.Kind() {
			case "identifier":
				name = fn.Utf8Text(src)
			case "selector_expression":
				if fld := fn.ChildByFieldName("field"); fld != nil {
					name = fld.Utf8Text(src)
				}
			}
			if name != "" {
				f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: graph.EdgeCalls, Name: name, Line: int(node.StartPosition().Row) + 1})
			}
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walkGoCalls(f, node.NamedChild(i), src, fromID)
	}
}
