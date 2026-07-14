// Package extract turns source files into graph nodes, refs, and contains
// edges (parse phase), then resolves refs project-wide into edges (resolve
// phase, resolve.go).
package extract

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

// ExtractorVersion participates in the store's auto-rebuild check.
// Bump whenever parsing or resolution output changes shape or semantics.
const ExtractorVersion = "v8-graphql"

// FileFacts is everything the parse phase learns from one file.
type FileFacts struct {
	Meta     graph.FileMeta
	Nodes    []graph.Node
	Refs     []graph.Ref
	Contains []graph.Edge
}

// Parser turns one source file into FileFacts. Pure: no project context, no I/O.
type Parser func(relPath string, src []byte) (*FileFacts, error)

// parsers maps a lowercased file extension (with dot) to its Parser.
var parsers = map[string]Parser{
	".graphql": ParseGraphQL,
	".java":    ParseJava,
	".go":      ParseGo,
	".py":      ParsePython,
	".ts":      ParseTypeScript,
	".tsx":     ParseTypeScript,
}

// Extensions returns every registered source extension, sorted.
func Extensions() []string {
	out := make([]string, 0, len(parsers))
	for ext := range parsers {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// parserFor returns the Parser for relPath's extension, or nil if none.
func parserFor(relPath string) Parser {
	return parsers[strings.ToLower(filepath.Ext(relPath))]
}

// normalizeSig collapses every whitespace run in a signature fragment to a
// single space, so multi-line declarations serialize stably.
func normalizeSig(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
