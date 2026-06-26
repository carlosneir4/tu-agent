package extract

import (
	"regexp"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// gqlTagRe matches a `gql` or `graphql` tag immediately preceding a backtick.
// \b prevents matching inside another identifier (e.g. "mygraphql`").
var gqlTagRe = regexp.MustCompile("\\b(?:gql|graphql)[ \t\r\n]*`")

// gqlTag is one tagged template body plus the byte offset where it starts.
type gqlTag struct {
	body   string
	offset int
}

// gqlTagBodies returns the body of every gql/graphql tagged template in s.
func gqlTagBodies(s string) []gqlTag {
	var out []gqlTag
	for _, loc := range gqlTagRe.FindAllStringIndex(s, -1) {
		bodyStart := loc[1] // just past the opening backtick
		end := matchBacktick(s, bodyStart)
		if end < 0 {
			continue
		}
		out = append(out, gqlTag{body: s[bodyStart:end], offset: bodyStart})
	}
	return out
}

// matchBacktick returns the index of the backtick that closes the template
// literal whose body starts at i, tracking ${ ... } nesting so a backtick inside
// an interpolation does not terminate early. Returns -1 if unterminated.
func matchBacktick(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // skip the escaped character
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				depth++
				i++
			}
		case '}':
			if depth > 0 {
				depth--
			}
		case '`':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

var (
	gqlFragmentRe  = regexp.MustCompile(`\bfragment\s+([A-Za-z_]\w*)\s+on\s+([A-Za-z_]\w*)`)
	gqlOperationRe = regexp.MustCompile(`\b(query|mutation|subscription)\s+([A-Za-z_]\w*)`)
	gqlSpreadRe    = regexp.MustCompile(`\.\.\.\s*([A-Za-z_]\w*)`)
	gqlInterpRe    = regexp.MustCompile(`\$\{\s*([A-Za-z_]\w*)`)
	gqlCommentRe   = regexp.MustCompile(`#[^\n]*`)
)

// scanGQLBody extracts fragment/operation definition nodes and composition refs
// from one GraphQL body. baseLine is the 1-based source line of the body start,
// used to set node/ref lines. Pure and total.
func scanGQLBody(relPath, body string, baseLine int) (nodes []graph.Node, refs []graph.Ref) {
	clean := gqlCommentRe.ReplaceAllString(body, "") // keeps newlines, drops "# ..."

	type defSpan struct {
		id          string
		open, close int
	}
	var defs []defSpan

	lineAt := func(off int) int { return baseLine + strings.Count(clean[:off], "\n") }

	for _, m := range gqlFragmentRe.FindAllStringSubmatchIndex(clean, -1) {
		name, onType := clean[m[2]:m[3]], clean[m[4]:m[5]]
		open := indexByteFrom(clean, '{', m[1])
		if open < 0 {
			continue
		}
		// The "::gql::" segment namespaces GraphQL def IDs away from TS symbol IDs
		// (relPath::Name), so a fragment and a same-named TS binding in one file
		// do not collide on a single node ID.
		id := relPath + "::gql::" + name
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindGraphQLFragment, Name: name,
			Path: relPath, Language: "graphql", ReturnType: onType, Line: lineAt(m[0])})
		defs = append(defs, defSpan{id, open, matchBrace(clean, open)})
	}
	for _, m := range gqlOperationRe.FindAllStringSubmatchIndex(clean, -1) {
		opKind, name := clean[m[2]:m[3]], clean[m[4]:m[5]]
		open := indexByteFrom(clean, '{', m[1])
		if open < 0 {
			continue
		}
		id := relPath + "::gql::" + name
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindGraphQLOperation, Name: name,
			Path: relPath, Language: "graphql", ReturnType: opKind, Line: lineAt(m[0])})
		defs = append(defs, defSpan{id, open, matchBrace(clean, open)})
	}

	attribute := func(pos int, name string) {
		if name == "" || name == "on" {
			return
		}
		for _, d := range defs {
			if d.close > d.open && pos > d.open && pos < d.close {
				refs = append(refs, graph.Ref{FromID: d.id, Kind: graph.EdgeSpreads, Name: name, Line: lineAt(pos)})
				return
			}
		}
	}
	for _, m := range gqlSpreadRe.FindAllStringSubmatchIndex(clean, -1) {
		attribute(m[0], clean[m[2]:m[3]])
	}
	for _, m := range gqlInterpRe.FindAllStringSubmatchIndex(clean, -1) {
		attribute(m[0], clean[m[2]:m[3]])
	}
	return nodes, refs
}

// matchBrace returns the index of the brace closing the block that opens at i
// (s[i] must be '{'), or -1 if unterminated.
func matchBrace(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// scanGraphQL extracts GraphQL definition nodes, composition refs, and file->def
// contains edges from every gql/graphql tag in src. Pure and total.
func scanGraphQL(relPath string, src []byte) (nodes []graph.Node, refs []graph.Ref, contains []graph.Edge) {
	s := string(src)
	for _, t := range gqlTagBodies(s) {
		baseLine := 1 + strings.Count(s[:t.offset], "\n")
		dn, dr := scanGQLBody(relPath, t.body, baseLine)
		for _, n := range dn {
			nodes = append(nodes, n)
			contains = append(contains, graph.Edge{From: relPath, To: n.ID, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
		}
		refs = append(refs, dr...)
	}
	return nodes, refs, contains
}

// ParseGraphQL extracts FileFacts from a .graphql file: a file node, SDL type
// nodes (scanGraphQLTypes), and any operation/fragment defs + composition refs
// (via scanGQLBody, which handles raw GraphQL content — not tagged templates).
// Pure and total.
func ParseGraphQL(relPath string, src []byte) (*FileFacts, error) {
	f := &FileFacts{Meta: graph.FileMeta{Path: relPath, Language: "graphql", Status: "ok"}}
	f.Nodes = append(f.Nodes, graph.Node{ID: relPath, Kind: graph.KindFile, Name: relPath, Path: relPath, Language: "graphql"})

	tn, tc := scanGraphQLTypes(relPath, src)
	f.Nodes = append(f.Nodes, tn...)
	f.Contains = append(f.Contains, tc...)

	// A .graphql file IS the document body; scan it directly rather than looking
	// for gql`` tagged templates (which is the TS/JS path).
	dn, dr := scanGQLBody(relPath, string(src), 1)
	for _, n := range dn {
		f.Nodes = append(f.Nodes, n)
		f.Contains = append(f.Contains, graph.Edge{From: relPath, To: n.ID, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	}
	f.Refs = append(f.Refs, dr...)
	return f, nil
}

// indexByteFrom returns the index of the first b at or after from, or -1.
func indexByteFrom(s string, b byte, from int) int {
	if from < 0 || from >= len(s) {
		return -1
	}
	j := strings.IndexByte(s[from:], b)
	if j < 0 {
		return -1
	}
	return from + j
}

// gqlTypeDefRe matches a top-of-line SDL type definition keyword + name. The
// required whitespace before the name avoids matching a field named `type:`
// (followed by ':') or a bare enum value (no following name). `extend type X`
// does not match because the line starts with `extend`, not the keyword.
var gqlTypeDefRe = regexp.MustCompile(`(?m)^\s*(type|interface|union|enum|input|scalar)\s+([A-Za-z_]\w*)`)

// scanGraphQLTypes extracts named SDL type definitions from a GraphQL schema as
// KindGraphQLType nodes (the SDL keyword in ReturnType) plus a file->type
// contains edge each. Pure and total; fields are not modeled.
func scanGraphQLTypes(relPath string, src []byte) (nodes []graph.Node, contains []graph.Edge) {
	s := string(src)
	for _, m := range gqlTypeDefRe.FindAllStringSubmatchIndex(s, -1) {
		keyword, name := s[m[2]:m[3]], s[m[4]:m[5]]
		id := relPath + "::" + name
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindGraphQLType, Name: name,
			Path: relPath, Language: "graphql", ReturnType: keyword, Line: 1 + strings.Count(s[:m[0]], "\n")})
		contains = append(contains, graph.Edge{From: relPath, To: id, Kind: graph.EdgeContains, Confidence: graph.ConfExact})
	}
	return nodes, contains
}
