package extract

import (
	"testing"

	"github.com/tu/tu-agent/internal/graph"
)

func TestGqlTagBodies(t *testing.T) {
	src := "const a = graphql`fragment F on User { id }`\n" +
		"const b = gql`query Q { me }`\n" +
		"const c = `not a gql tag`\n" +
		"const d = mygraphql`should not match`\n"
	tags := gqlTagBodies(src)
	if len(tags) != 2 {
		t.Fatalf("want 2 gql tags, got %d: %+v", len(tags), tags)
	}
	if tags[0].body != "fragment F on User { id }" {
		t.Errorf("tag0 body = %q", tags[0].body)
	}
	if tags[1].body != "query Q { me }" {
		t.Errorf("tag1 body = %q", tags[1].body)
	}
	// offset is the byte index where the body starts: len("const a = graphql`") = 18.
	if tags[0].offset != 18 {
		t.Errorf("tag0 offset = %d, want 18", tags[0].offset)
	}
}

func TestGqlTagBodies_InterpolationDepth(t *testing.T) {
	// A backtick inside a ${...} interpolation must not terminate the body early.
	src := "graphql`query Q { ...${cond ? `a` : `b`} }`"
	tags := gqlTagBodies(src)
	if len(tags) != 1 {
		t.Fatalf("want 1 tag, got %d", len(tags))
	}
	if want := "query Q { ...${cond ? `a` : `b`} }"; tags[0].body != want {
		t.Errorf("interpolated body wrong:\n got %q\nwant %q", tags[0].body, want)
	}
}

func TestScanGQLBody(t *testing.T) {
	body := "fragment ArticleCard_article on Article {\n  id\n  ...Author_fields\n  # ...Commented_out\n}"
	nodes, refs := scanGQLBody("src/a.ts", body, 1)
	if len(nodes) != 1 || nodes[0].Kind != graph.KindGraphQLFragment ||
		nodes[0].Name != "ArticleCard_article" || nodes[0].ReturnType != "Article" ||
		nodes[0].ID != "src/a.ts::gql::ArticleCard_article" {
		t.Fatalf("fragment node wrong: %+v", nodes)
	}
	if len(refs) != 1 || refs[0].Kind != graph.EdgeSpreads ||
		refs[0].Name != "Author_fields" || refs[0].FromID != "src/a.ts::gql::ArticleCard_article" {
		t.Fatalf("spread ref wrong: %+v", refs)
	}
}

func TestScanGQLBody_OperationsAndInterp(t *testing.T) {
	body := "query Feed {\n  ...List_items\n  ...${UserFragment}\n  ...${x.y}\n}"
	nodes, refs := scanGQLBody("src/q.ts", body, 1)
	if len(nodes) != 1 || nodes[0].Kind != graph.KindGraphQLOperation ||
		nodes[0].Name != "Feed" || nodes[0].ReturnType != "query" {
		t.Fatalf("operation node wrong: %+v", nodes)
	}
	// List_items (spread) + UserFragment (simple interp); ${x.y} captures leading "x".
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	if len(refs) != 3 {
		t.Fatalf("want 3 refs (List_items, UserFragment, x), got %d: %v", len(refs), names)
	}
}

func TestScanGQLBody_InlineFragmentNotASpread(t *testing.T) {
	body := "fragment F on Node {\n  ... on User { name }\n}"
	_, refs := scanGQLBody("src/f.ts", body, 1)
	if len(refs) != 0 {
		t.Fatalf("inline fragment must not produce a spread ref: %+v", refs)
	}
}

func TestScanGraphQLTypes(t *testing.T) {
	src := []byte("scalar Date\n" +
		"enum Color { RED }\n" +
		"interface Node { id: ID }\n" +
		"union Media = Photo | Video\n" +
		"input Filter { q: String }\n" +
		"type Article implements Node {\n  id: ID\n  type: String\n}\n" +
		"extend type Article {\n  extra: String\n}\n")
	nodes, contains := scanGraphQLTypes("schema.graphql", src)

	got := map[string]string{} // name -> keyword
	for _, n := range nodes {
		if n.Kind != graph.KindGraphQLType {
			t.Fatalf("unexpected kind %q", n.Kind)
		}
		got[n.Name] = n.ReturnType
	}
	want := map[string]string{
		"Date": "scalar", "Color": "enum", "Node": "interface",
		"Media": "union", "Filter": "input", "Article": "type",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d type nodes %v, want %d %v", len(got), got, len(want), want)
	}
	for name, kw := range want {
		if got[name] != kw {
			t.Errorf("%s: keyword %q, want %q", name, got[name], kw)
		}
	}
	// "extend type Article" must NOT create a second Article node, and the field
	// `type: String` must NOT be captured as a type.
	count := 0
	for _, n := range nodes {
		if n.Name == "Article" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Article node count = %d, want 1 (no extend duplicate)", count)
	}
	if len(contains) != len(nodes) {
		t.Errorf("want one contains edge per type node: %d edges, %d nodes", len(contains), len(nodes))
	}
	for _, e := range contains {
		if e.From != "schema.graphql" || e.Kind != graph.EdgeContains || e.Confidence != graph.ConfExact {
			t.Errorf("bad contains edge: %+v", e)
		}
	}
}

func TestParseGraphQL(t *testing.T) {
	src := []byte("type Article { id: ID }\n" +
		"fragment ArticleCard_article on Article { id }\n")
	f, err := ParseGraphQL("schema.graphql", src)
	if err != nil {
		t.Fatal(err)
	}
	var sawFile, sawType, sawFragment bool
	for _, n := range f.Nodes {
		switch {
		case n.Kind == graph.KindFile && n.ID == "schema.graphql":
			sawFile = true
		case n.Kind == graph.KindGraphQLType && n.Name == "Article":
			sawType = true
		case n.Kind == graph.KindGraphQLFragment && n.Name == "ArticleCard_article":
			sawFragment = true
		}
	}
	if !sawFile || !sawType || !sawFragment {
		t.Fatalf("ParseGraphQL missing nodes: file=%v type=%v fragment=%v\n%+v", sawFile, sawType, sawFragment, f.Nodes)
	}
	if f.Meta.Language != "graphql" {
		t.Errorf("Meta.Language = %q, want graphql", f.Meta.Language)
	}
}

func TestParserForGraphQL(t *testing.T) {
	if parserFor("packages/x/schema.graphql") == nil {
		t.Error(".graphql must have a registered parser")
	}
}
