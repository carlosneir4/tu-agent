package extract

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"

	"github.com/tu/tu-agent/internal/graph"
)

// ParseJava extracts FileFacts from one Java source file. Pure function:
// no project context, no I/O — resolution happens later.
func ParseJava(relPath string, src []byte) (*FileFacts, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_java.Language())); err != nil {
		return nil, fmt.Errorf("extract.ParseJava: %w", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	f := &FileFacts{Meta: graph.FileMeta{Path: relPath, Language: "java", Status: "ok"}}
	fileNode := graph.Node{ID: relPath, Kind: graph.KindFile, Name: relPath, Path: relPath, Language: "java"}
	f.Nodes = append(f.Nodes, fileNode)

	root := tree.RootNode()
	_ = root.ToSexp() // debug: will be logged in test if needed
	seenIDs := map[string]bool{relPath: true}
	walkJava(f, root, src, relPath, "", relPath, seenIDs, false)

	// Test detection: by class name convention or JUnit/TestNG import.
	testish := hasTestImport(f.Meta.Imports)
	for i, n := range f.Nodes {
		if n.Kind == graph.KindClass && (testish || isTestClassName(n.Name)) {
			f.Nodes[i].Kind = graph.KindTest
		}
	}
	return f, nil
}

func hasTestImport(imports []string) bool {
	for _, imp := range imports {
		if strings.HasPrefix(imp, "org.junit") || strings.HasPrefix(imp, "org.testng") {
			return true
		}
	}
	return false
}

func isTestClassName(name string) bool {
	return strings.HasSuffix(name, "Test") || strings.HasSuffix(name, "Tests") ||
		strings.HasSuffix(name, "IT") || strings.HasPrefix(name, "Test")
}

func walkJava(f *FileFacts, node *tree_sitter.Node, src []byte, relPath, encClass, encID string, seenIDs map[string]bool, encIface bool) {
	switch node.Kind() {
	case "package_declaration":
		for i := uint(0); i < node.NamedChildCount(); i++ {
			c := node.NamedChild(i)
			if c.Kind() == "scoped_identifier" || c.Kind() == "identifier" {
				f.Meta.Package = c.Utf8Text(src)
			}
		}
		return
	case "import_declaration":
		text := strings.TrimSpace(node.Utf8Text(src))
		if strings.Contains(text, " static ") {
			return
		}
		imp := strings.TrimSuffix(strings.TrimPrefix(text, "import"), ";")
		imp = strings.ReplaceAll(strings.TrimSpace(imp), " ", "")
		if imp != "" {
			f.Meta.Imports = append(f.Meta.Imports, imp)
		}
		return
	case "class_declaration", "interface_declaration", "enum_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		name := nameNode.Utf8Text(src)
		fullName := name
		if encClass != "" {
			fullName = encClass + "." + name
		}
		id := relPath + "::" + fullName
		if seenIDs[id] {
			return
		}
		seenIDs[id] = true
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: graph.KindClass, Name: fullName, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1,
			Language: "java",
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})

		if sc := node.ChildByFieldName("superclass"); sc != nil {
			if tn := typeName(sc, src); tn != "" {
				f.Refs = append(f.Refs, graph.Ref{FromID: id, Kind: graph.EdgeExtends, Name: tn, Line: int(sc.StartPosition().Row) + 1})
			}
		}
		if ifs := node.ChildByFieldName("interfaces"); ifs != nil {
			collectTypeRefs(f, ifs, src, id, graph.EdgeImplements)
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			c := node.NamedChild(i)
			if c.Kind() == "extends_interfaces" {
				collectTypeRefs(f, c, src, id, graph.EdgeExtends)
			}
		}
		if body := node.ChildByFieldName("body"); body != nil {
			for i := uint(0); i < body.NamedChildCount(); i++ {
				walkJava(f, body.NamedChild(i), src, relPath, fullName, id, seenIDs, node.Kind() == "interface_declaration")
			}
		}
		return
	case "method_declaration", "constructor_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil || encClass == "" {
			return
		}
		mname := encClass + "." + nameNode.Utf8Text(src)
		id := relPath + "::" + mname
		if seenIDs[id] {
			return
		}
		seenIDs[id] = true
		params, ret := "", ""
		if p := node.ChildByFieldName("parameters"); p != nil {
			params = normalizeSig(p.Utf8Text(src))
		}
		if retNode := node.ChildByFieldName("type"); retNode != nil { // nil for constructors
			ret = normalizeSig(retNode.Utf8Text(src))
		}
		f.Nodes = append(f.Nodes, graph.Node{
			ID: id, Kind: graph.KindFunction, Name: mname, Path: relPath,
			Line: int(node.StartPosition().Row) + 1, EndLine: int(node.EndPosition().Row) + 1,
			Language: "java", Params: params, ReturnType: ret,
			Exported: encIface || javaHasModifier(node, src, "public"),
		})
		f.Contains = append(f.Contains, graph.Edge{From: encID, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		if body := node.ChildByFieldName("body"); body != nil {
			walkInvocations(f, body, src, id)
		}
		return
	case "field_declaration":
		// Spring field injection: @Autowired type is an implicit dependency.
		if encClass == "" || !hasAnnotation(node, src, "Autowired") {
			return
		}
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil {
			return
		}
		if tn := typeName(typeNode, src); tn != "" {
			f.Refs = append(f.Refs, graph.Ref{
				FromID: encID, // the enclosing class node
				Kind:   graph.EdgeImports,
				Name:   tn,
				Line:   int(node.StartPosition().Row) + 1,
			})
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walkJava(f, node.NamedChild(i), src, relPath, encClass, encID, seenIDs, encIface)
	}
}

func walkInvocations(f *FileFacts, node *tree_sitter.Node, src []byte, fromID string) {
	if node.Kind() == "method_invocation" {
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			recv := ""
			// Capture the receiver only when it is a simple identifier
			// (LOGGER, result, builder). Chained/expression receivers stay
			// empty; resolution falls back to name-only as before.
			if obj := node.ChildByFieldName("object"); obj != nil && obj.Kind() == "identifier" {
				recv = obj.Utf8Text(src)
			}
			f.Refs = append(f.Refs, graph.Ref{
				FromID: fromID, Kind: graph.EdgeCalls,
				Name: nameNode.Utf8Text(src), Recv: recv, Line: int(node.StartPosition().Row) + 1,
			})
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		walkInvocations(f, node.NamedChild(i), src, fromID)
	}
}

func typeName(node *tree_sitter.Node, src []byte) string {
	switch node.Kind() {
	case "type_identifier", "scoped_type_identifier":
		return node.Utf8Text(src)
	case "generic_type":
		if node.NamedChildCount() > 0 {
			return typeName(node.NamedChild(0), src)
		}
		return ""
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if tn := typeName(node.NamedChild(i), src); tn != "" {
			return tn
		}
	}
	return ""
}

// hasAnnotation reports whether node's modifiers child contains @name.
// Substring match tolerates forms like @Autowired(required=false).
func hasAnnotation(node *tree_sitter.Node, src []byte, name string) bool {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if c.Kind() == "modifiers" {
			if strings.Contains(c.Utf8Text(src), "@"+name) {
				return true
			}
		}
	}
	return false
}

// javaHasModifier reports whether node's modifiers child contains mod as a
// whole word (annotations like @Override are separate tokens and never match).
func javaHasModifier(node *tree_sitter.Node, src []byte, mod string) bool {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if c.Kind() != "modifiers" {
			continue
		}
		if slices.Contains(strings.Fields(c.Utf8Text(src)), mod) {
			return true
		}
	}
	return false
}

func collectTypeRefs(f *FileFacts, node *tree_sitter.Node, src []byte, fromID string, kind graph.EdgeKind) {
	if node.Kind() == "type_identifier" || node.Kind() == "scoped_type_identifier" {
		f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: kind, Name: node.Utf8Text(src), Line: int(node.StartPosition().Row) + 1})
		return
	}
	if node.Kind() == "generic_type" {
		if tn := typeName(node, src); tn != "" {
			f.Refs = append(f.Refs, graph.Ref{FromID: fromID, Kind: kind, Name: tn, Line: int(node.StartPosition().Row) + 1})
		}
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		collectTypeRefs(f, node.NamedChild(i), src, fromID, kind)
	}
}
