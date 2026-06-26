package codegen

import "sort"

// Edge is a directed dependency: From depends on To. Endpoints are repo-relative
// file paths in the file-level graph, or domain names after AggregateToDomains.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
}

// AggregateToDomains maps file-level edges to domain-level edges via
// fileToDomain. Self-edges (same domain) and edges with an unmapped endpoint
// are dropped; the result is deduplicated and sorted.
func AggregateToDomains(edges []Edge, fileToDomain map[string]string) []Edge {
	seen := make(map[Edge]bool)
	var out []Edge
	for _, e := range edges {
		fd, ok1 := fileToDomain[e.From]
		td, ok2 := fileToDomain[e.To]
		if !ok1 || !ok2 || fd == td {
			continue
		}
		de := Edge{From: fd, To: td}
		if !seen[de] {
			seen[de] = true
			out = append(out, de)
		}
	}
	sortEdges(out)
	return out
}
