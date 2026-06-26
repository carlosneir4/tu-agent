package extract

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/tu/tu-agent/internal/graph"
)

// ParseTypeScript extracts FileFacts from one TypeScript source file. Pure
// function. The grammar is chosen by extension (.tsx -> TSX, else TypeScript);
// both emit Language "typescript" so AdapterFor resolves them identically.
// Package is the dotted module path from the file path (src/util/slug.ts ->
// src.util.slug), mirroring ParsePython.
func ParseTypeScript(relPath string, src []byte) (*FileFacts, error) {
	langPtr := tree_sitter_typescript.LanguageTypescript()
	if strings.HasSuffix(strings.ToLower(relPath), ".tsx") {
		langPtr = tree_sitter_typescript.LanguageTSX()
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(langPtr)); err != nil {
		return nil, fmt.Errorf("extract.ParseTypeScript: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("extract.ParseTypeScript: tree-sitter returned nil")
	}
	defer tree.Close()

	module := tsModule(relPath)
	f := &FileFacts{Meta: graph.FileMeta{Path: relPath, Language: "typescript", Status: "ok", Package: module}}
	f.Nodes = append(f.Nodes, graph.Node{ID: relPath, Kind: graph.KindFile, Name: relPath, Path: relPath, Language: "typescript"})

	isTest := isTSTestFile(relPath)
	seen := map[string]bool{relPath: true}
	walkTS(f, tree.RootNode(), src, relPath, "", relPath, seen, isTest, false)
	gn, gr, gc := scanGraphQL(relPath, src)
	f.Nodes = append(f.Nodes, gn...)
	f.Refs = append(f.Refs, gr...)
	f.Contains = append(f.Contains, gc...)
	return f, nil
}

// tsModule converts a repo-relative path to a dotted module name.
func tsModule(relPath string) string {
	p := filepath.ToSlash(relPath)
	for _, ext := range []string{".tsx", ".ts"} {
		if strings.HasSuffix(strings.ToLower(p), ext) {
			p = p[:len(p)-len(ext)]
			break
		}
	}
	return strings.ReplaceAll(p, "/", ".")
}

// isTSTestFile reports the vitest/jest conventions: *.test.ts(x),
// *.spec.ts(x), or any file under a __tests__/ directory.
func isTSTestFile(relPath string) bool {
	base := strings.ToLower(filepath.Base(relPath))
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	for seg := range strings.SplitSeq(filepath.ToSlash(relPath), "/") {
		if seg == "__tests__" {
			return true
		}
	}
	return false
}

// walkTS visits declarations. encClass qualifies nested names
// ("InvoiceService.total"), encID owns contains edges and call refs,
// exported carries the surrounding `export` keyword down to declarations.
func walkTS(f *FileFacts, node *tree_sitter.Node, src []byte, relPath, encClass, encID string, seen map[string]bool, isTest, exported bool) {
	switch node.Kind() {
	case "import_statement":
		collectTSImport(f, node, src, relPath)
		return
	case "call_expression":
		if nm := tsCallName(node, src); nm != "" {
			f.Refs = append(f.Refs, graph.Ref{FromID: encID, Kind: graph.EdgeCalls, Name: nm, Line: int(node.StartPosition().Row) + 1})
		}
		// no return — descend so calls nested in anonymous describe/it callbacks
		// attach to the same enclosing scope (the file node for top-level callbacks)
	case "export_statement":
		if decl := node.ChildByFieldName("declaration"); decl != nil {
			walkTS(f, decl, src, relPath, encClass, encID, seen, isTest, true)
		}
		return // `export { x }` re-export lists declare nothing
	case "class_declaration":
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
		if isTest {
			kind = graph.KindTest
		}
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: kind, Name: full, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "typescript",
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if c := node.NamedChild(i); c.Kind() == "class_heritage" {
				collectTSHeritage(f, c, src, id)
			}
		}
		if body := node.ChildByFieldName("body"); body != nil {
			for i := uint(0); i < body.NamedChildCount(); i++ {
				walkTS(f, body.NamedChild(i), src, relPath, full, id, seen, isTest, exported)
			}
		}
		return
	case "function_declaration":
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
		if isTest {
			kind = graph.KindTest
		}
		params, ret := tsSignature(node, src)
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: kind, Name: full, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "typescript",
			Params: params, ReturnType: ret, Exported: exported,
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		if body := node.ChildByFieldName("body"); body != nil {
			walkTS(f, body, src, relPath, full, id, seen, isTest, false)
		}
		return
	case "method_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		mname := nameNode.Utf8Text(src)
		full := mname
		if encClass != "" {
			full = encClass + "." + mname
		}
		id := relPath + "::" + full
		if seen[id] {
			return
		}
		seen[id] = true
		kind := graph.KindFunction
		if isTest {
			kind = graph.KindTest
		}
		params, ret := tsSignature(node, src)
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: kind, Name: full, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1, Language: "typescript",
			Params: params, ReturnType: ret,
			Exported: exported && tsMethodPublic(node, mname, src),
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		if body := node.ChildByFieldName("body"); body != nil {
			walkTS(f, body, src, relPath, full, id, seen, isTest, false)
		}
		return
	case "lexical_declaration", "variable_declaration":
		for i := uint(0); i < node.NamedChildCount(); i++ {
			d := node.NamedChild(i)
			if d.Kind() != "variable_declarator" {
				continue
			}
			nameNode := d.ChildByFieldName("name")
			value := d.ChildByFieldName("value")
			if nameNode == nil || value == nil || nameNode.Kind() != "identifier" {
				continue
			}
			vk := value.Kind()
			if vk != "arrow_function" && vk != "function_expression" && vk != "function" {
				continue // plain const, not a function — skip
			}
			fname := nameNode.Utf8Text(src)
			full := fname
			if encClass != "" {
				full = encClass + "." + fname
			}
			id := relPath + "::" + full
			if seen[id] {
				continue
			}
			seen[id] = true
			kind := graph.KindFunction
			if isTest {
				kind = graph.KindTest
			}
			params, ret := tsSignature(value, src)
			f.Nodes = append(f.Nodes, graph.Node{
				ID: id, Kind: kind, Name: full, Path: relPath,
				Line: int(d.StartPosition().Row) + 1, EndLine: int(d.EndPosition().Row) + 1, Language: "typescript",
				Params: params, ReturnType: ret, Exported: exported,
			})
			f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
			if body := value.ChildByFieldName("body"); body != nil {
				walkTS(f, body, src, relPath, full, id, seen, isTest, false)
			}
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walkTS(f, node.NamedChild(i), src, relPath, encClass, encID, seen, isTest, exported)
	}
}

// tsSignature reads parameters and return type. Single-param arrows
// (`x => …`) use the "parameter" field; the return_type text includes the
// leading ":" of the type annotation, which is stripped.
func tsSignature(node *tree_sitter.Node, src []byte) (params, ret string) {
	if p := node.ChildByFieldName("parameters"); p != nil {
		params = normalizeSig(p.Utf8Text(src))
	} else if p := node.ChildByFieldName("parameter"); p != nil {
		params = "(" + normalizeSig(p.Utf8Text(src)) + ")"
	}
	if r := node.ChildByFieldName("return_type"); r != nil {
		ret = normalizeSig(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.Utf8Text(src)), ":")))
	}
	return params, ret
}

// tsMethodPublic reports whether a method is part of the class's public
// surface: not a constructor, no private/protected modifier, not a #-private
// or _-prefixed name.
func tsMethodPublic(node *tree_sitter.Node, name string, src []byte) bool {
	if name == "constructor" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "_") {
		return false
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		if c := node.Child(i); c != nil && c.Kind() == "accessibility_modifier" {
			if mod := c.Utf8Text(src); mod == "private" || mod == "protected" {
				return false
			}
		}
	}
	return true
}

// collectTSImport normalizes a relative ES import to dotted module names.
// Named imports: module.Name. Default, namespace, and side-effect imports: module.*
// (the local alias of a default import is arbitrary, so wildcard is the honest fact).
// Bare specifiers (npm packages) are skipped — they cannot resolve in-repo.
func collectTSImport(f *FileFacts, node *tree_sitter.Node, src []byte, relPath string) {
	srcNode := node.ChildByFieldName("source")
	if srcNode == nil {
		return
	}
	spec := strings.Trim(srcNode.Utf8Text(src), "\"'`")
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return
	}
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		if s, ok := strings.CutSuffix(spec, ext); ok {
			spec = s
			break
		}
	}
	module := strings.ReplaceAll(path.Join(path.Dir(filepath.ToSlash(relPath)), spec), "/", ".")

	named := false
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if c.Kind() != "import_clause" {
			continue
		}
		for j := uint(0); j < c.NamedChildCount(); j++ {
			cc := c.NamedChild(j)
			switch cc.Kind() {
			case "named_imports":
				for k := uint(0); k < cc.NamedChildCount(); k++ {
					s := cc.NamedChild(k)
					if s.Kind() != "import_specifier" {
						continue
					}
					if nm := s.ChildByFieldName("name"); nm != nil {
						f.Meta.Imports = append(f.Meta.Imports, module+"."+nm.Utf8Text(src))
						named = true
					}
				}
			case "identifier", "namespace_import":
				f.Meta.Imports = append(f.Meta.Imports, module+".*")
				named = true
			}
		}
	}
	if !named {
		f.Meta.Imports = append(f.Meta.Imports, module+".*") // side-effect import
	}
}

func tsCallName(call *tree_sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier":
		return fn.Utf8Text(src)
	case "member_expression":
		if p := fn.ChildByFieldName("property"); p != nil {
			return p.Utf8Text(src)
		}
	}
	return ""
}

// collectTSHeritage emits extends/implements refs from a class_heritage node.
func collectTSHeritage(f *FileFacts, h *tree_sitter.Node, src []byte, fromID string) {
	for i := uint(0); i < h.NamedChildCount(); i++ {
		c := h.NamedChild(i)
		kind := graph.EdgeExtends
		if c.Kind() == "implements_clause" {
			kind = graph.EdgeImplements
		}
		for j := uint(0); j < c.NamedChildCount(); j++ {
			if nm := tsTypeName(c.NamedChild(j), src); nm != "" {
				f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: kind, Name: nm, Line: int(c.StartPosition().Row) + 1})
			}
		}
	}
}

func tsTypeName(n *tree_sitter.Node, src []byte) string {
	switch n.Kind() {
	case "identifier", "type_identifier":
		return n.Utf8Text(src)
	case "member_expression":
		if p := n.ChildByFieldName("property"); p != nil {
			return p.Utf8Text(src)
		}
	case "generic_type":
		if nm := n.ChildByFieldName("name"); nm != nil {
			return tsTypeName(nm, src)
		}
	}
	return ""
}

// tsTestSources returns the source files a TS test file conventionally covers:
// the same-dir sibling minus the .test/.spec segment, and for __tests__/ files,
// also the same-named file in the parent directory.
func tsTestSources(testPath string) []string {
	p := filepath.ToSlash(testPath)
	dir, base := path.Dir(p), path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stripped := false
	for _, marker := range []string{".test", ".spec"} {
		if s, ok := strings.CutSuffix(stem, marker); ok {
			stem, stripped = s, true
			break
		}
	}
	name := stem + ext
	var out []string
	if stripped {
		out = append(out, path.Join(dir, name))
	}
	if path.Base(dir) == "__tests__" {
		out = append(out, path.Join(path.Dir(dir), name))
	}
	return out
}
