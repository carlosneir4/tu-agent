package codegen

import (
	"fmt"
	"sort"
	"strings"
)

// BuildDomainContext renders a compact, deterministic structural header for
// one domain: its files with packages, then the dependency edges crossing the
// domain boundary. Inbound pairs (external class -> internal class) identify
// the domain's real entry points; Outbound lists the external classes the
// domain depends on. Pure function over already-computed scan results;
// typically a few hundred bytes per domain.
func BuildDomainContext(d Domain, files []SourceUnit, edges []Edge) string {
	inDomain := make(map[string]bool, len(d.Files))
	for _, p := range d.Files {
		inDomain[p] = true
	}
	meta := make(map[string]SourceUnit, len(files))
	for _, f := range files {
		meta[f.Path] = f
	}

	inbound := map[string]bool{}
	outbound := map[string]bool{}
	for _, e := range edges {
		switch {
		case !inDomain[e.From] && inDomain[e.To]:
			inbound[meta[e.From].FQN+" -> "+meta[e.To].FQN] = true
		case inDomain[e.From] && !inDomain[e.To]:
			outbound[meta[e.To].FQN] = true
		}
	}

	var sb strings.Builder
	sb.WriteString("Structural context (computed from the import graph — trust it over guesses):\nFiles:\n")
	paths := append([]string(nil), d.Files...)
	sort.Strings(paths)
	for _, p := range paths {
		if pkg := meta[p].Package; pkg != "" {
			fmt.Fprintf(&sb, "- %s (%s)\n", p, pkg)
		} else {
			fmt.Fprintf(&sb, "- %s\n", p)
		}
	}
	sb.WriteString("Inbound (external class -> class in this domain; these are the entry points):\n")
	writeSortedSet(&sb, inbound)
	sb.WriteString("Outbound (external classes this domain depends on):\n")
	writeSortedSet(&sb, outbound)
	return sb.String()
}

// writeSortedSet writes a set as sorted "- item" lines, or "- (none)".
func writeSortedSet(sb *strings.Builder, set map[string]bool) {
	if len(set) == 0 {
		sb.WriteString("- (none)\n")
		return
	}
	items := make([]string, 0, len(set))
	for s := range set {
		items = append(items, s)
	}
	sort.Strings(items)
	for _, s := range items {
		fmt.Fprintf(sb, "- %s\n", s)
	}
}
