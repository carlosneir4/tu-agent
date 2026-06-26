package extract

import (
	"fmt"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"github.com/tu/tu-agent/internal/graph"
)

// ParsePython extracts FileFacts from one Python source file. Pure function.
// Package is the dotted module path from the file path (pkg/mod.py -> pkg.mod),
// so `from pkg.mod import C` (normalized to pkg.mod.C) resolves against byFQN.
func ParsePython(relPath string, src []byte) (*FileFacts, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		return nil, fmt.Errorf("extract.ParsePython: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("extract.ParsePython: tree-sitter returned nil")
	}
	defer tree.Close()

	module := strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(relPath), ".py"), "/", ".")
	f := &FileFacts{Meta: graph.FileMeta{Path: relPath, Language: "python", Status: "ok", Package: module}}
	f.Nodes = append(f.Nodes, graph.Node{ID: relPath, Kind: graph.KindFile, Name: relPath, Path: relPath, Language: "python"})

	isTest := isPyTestFile(relPath)
	seen := map[string]bool{relPath: true}
	walkPy(f, tree.RootNode(), src, relPath, "", relPath, seen, isTest)
	return f, nil
}

func isPyTestFile(relPath string) bool {
	base := filepath.Base(relPath)
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}

func walkPy(f *FileFacts, node *tree_sitter.Node, src []byte, relPath, encClass, encID string, seen map[string]bool, isTest bool) {
	switch node.Kind() {
	case "import_statement":
		collectPyImport(f, node, src, false)
		return
	case "import_from_statement":
		collectPyImport(f, node, src, true)
		return
	case "class_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		name := nameNode.Utf8Text(src)
		full := name
		if encClass != "" {
			full = encClass + "." + name
		}
		id := relPath + "::" + full
		if seen[id] {
			return
		}
		seen[id] = true
		kind := graph.KindClass
		if isTest || strings.HasPrefix(name, "Test") {
			kind = graph.KindTest
		}
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: kind, Name: full, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "python",
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		if sup := node.ChildByFieldName("superclasses"); sup != nil {
			for i := uint(0); i < sup.NamedChildCount(); i++ {
				c := sup.NamedChild(i)
				if nm := pyBaseName(c, src); nm != "" {
					f.Refs = append(f.Refs, graph.Ref{FromID: id, Kind: graph.EdgeExtends, Name: nm, Line: int(c.StartPosition().Row) + 1})
				}
			}
		}
		if body := node.ChildByFieldName("body"); body != nil {
			for i := uint(0); i < body.NamedChildCount(); i++ {
				walkPy(f, body.NamedChild(i), src, relPath, full, id, seen, isTest)
			}
		}
		return
	case "function_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		fname := nameNode.Utf8Text(src)
		full := fname
		if encClass != "" {
			full = encClass + "." + fname
		}
		id := relPath + "::" + full
		if seen[id] {
			return
		}
		seen[id] = true
		kind := graph.KindFunction
		if isTest || strings.HasPrefix(fname, "test_") {
			kind = graph.KindTest
		}
		params, ret := "", ""
		if p := node.ChildByFieldName("parameters"); p != nil {
			params = normalizeSig(p.Utf8Text(src))
		}
		if r := node.ChildByFieldName("return_type"); r != nil {
			ret = normalizeSig(r.Utf8Text(src))
		}
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: kind, Name: full, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "python",
			Params: params, ReturnType: ret,
			Exported: !strings.HasPrefix(fname, "_"),
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		if body := node.ChildByFieldName("body"); body != nil {
			walkPyCalls(f, body, src, id)
			for i := uint(0); i < body.NamedChildCount(); i++ {
				walkPy(f, body.NamedChild(i), src, relPath, full, id, seen, isTest)
			}
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walkPy(f, node.NamedChild(i), src, relPath, encClass, encID, seen, isTest)
	}
}

// walkPyCalls records call refs in one function body, without descending into
// nested defs/classes (those own their own scope).
func walkPyCalls(f *FileFacts, node *tree_sitter.Node, src []byte, fromID string) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		switch c.Kind() {
		case "function_definition", "class_definition":
			continue
		case "call":
			if nm := pyCallName(c, src); nm != "" {
				f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: graph.EdgeCalls, Name: nm, Line: int(c.StartPosition().Row) + 1})
			}
		}
		walkPyCalls(f, c, src, fromID)
	}
}

func pyCallName(call *tree_sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier":
		return fn.Utf8Text(src)
	case "attribute":
		if a := fn.ChildByFieldName("attribute"); a != nil {
			return a.Utf8Text(src)
		}
	}
	return ""
}

func pyBaseName(n *tree_sitter.Node, src []byte) string {
	switch n.Kind() {
	case "identifier":
		return n.Utf8Text(src)
	case "attribute":
		if a := n.ChildByFieldName("attribute"); a != nil {
			return a.Utf8Text(src)
		}
	}
	return ""
}

func collectPyImport(f *FileFacts, node *tree_sitter.Node, src []byte, fromImport bool) {
	if !fromImport {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if name := pyDotted(node.NamedChild(i), src); name != "" {
				f.Meta.Imports = append(f.Meta.Imports, name)
			}
		}
		return
	}
	modNode := node.ChildByFieldName("module_name")
	mod := ""
	if modNode != nil {
		mod = pyDotted(modNode, src)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if modNode != nil && c.StartByte() == modNode.StartByte() && c.EndByte() == modNode.EndByte() {
			continue // the module name itself
		}
		nm := pyDotted(c, src)
		if nm == "" {
			continue
		}
		if mod != "" {
			f.Meta.Imports = append(f.Meta.Imports, mod+"."+nm)
		} else {
			f.Meta.Imports = append(f.Meta.Imports, nm)
		}
	}
}

func pyDotted(n *tree_sitter.Node, src []byte) string {
	switch n.Kind() {
	case "dotted_name", "identifier":
		return n.Utf8Text(src)
	case "aliased_import":
		if nm := n.ChildByFieldName("name"); nm != nil {
			return pyDotted(nm, src)
		}
	}
	return ""
}
