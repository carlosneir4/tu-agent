// Package autolink derives links between memory observations and graph nodes by
// extracting type-like identifiers from note prose and resolving them against a
// unique-name index. It depends on neither the memory nor the graph package: the
// caller supplies the index and persists the result.
package autolink

import "regexp"

// pascalCase matches a capitalized identifier of at least 3 runes. Method names
// (lowercase first letter) and 1-2 char acronyms are deliberately excluded.
var pascalCase = regexp.MustCompile(`\b[A-Z][A-Za-z0-9]{2,}\b`)

// stoplist holds generic or library identifiers that are also class names but
// are almost never the subject of a note. Flat across languages: a term from one
// ecosystem is harmless in another (it just won't match a unique node there).
// Extend conservatively — over-listing drops legitimate links. Precise tuning is
// empirical: run `tu-agent memory relink`, eyeball the derived links, and add
// only words that prove to be noise.
var stoplist = map[string]bool{
	// Java / general
	"Jackson": true, "Package": true, "Feature": true, "Widgets": true,
	"Item": true, "String": true, "Object": true, "List": true, "Map": true,
	// React / TypeScript scaffolding
	"Props": true, "State": true, "Context": true, "Provider": true,
	"Component": true, "Element": true, "Ref": true, "Fragment": true,
	"Children": true,
	// Go scaffolding
	"Store": true, "Handler": true, "Server": true, "Client": true,
	"Config": true, "Service": true,
}

// Symbols returns the deduplicated PascalCase identifiers in content, in
// first-seen order, excluding stoplist entries.
func Symbols(content string) []string {
	matches := pascalCase.FindAllString(content, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if stoplist[m] || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// Resolve maps symbols to graph node IDs using index (name → node ID). index
// must contain only unique names; ambiguous names are the caller's job to omit.
// Results preserve input order and are deduplicated.
func Resolve(symbols []string, index map[string]string) []string {
	out := make([]string, 0, len(symbols))
	seen := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		id, ok := index[s]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
