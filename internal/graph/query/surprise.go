package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// domainOf derives a coarse domain key: the first `depth` segments of the package
// (split on "." or "/", lowercased), falling back to the path when the package is
// empty. Empty package and path → "external". This is a deterministic proxy for
// "concept", decoupled from codegen.
func domainOf(pkg, path string, depth int) string {
	src := pkg
	if src == "" {
		src = path
	}
	if src == "" {
		return "external"
	}
	segs := strings.FieldsFunc(strings.ToLower(src), func(r rune) bool {
		return r == '.' || r == '/'
	})
	if len(segs) == 0 {
		return "external"
	}
	if depth < 1 {
		depth = 1
	}
	if len(segs) > depth {
		segs = segs[:depth]
	}
	return strings.Join(segs, "/")
}

// domainOfID resolves a node ID to its domain. Unknown IDs and external nodes are
// "external" and are never scored as surprising. The node's package is looked up by
// path in pkgByPath (empty when no FileMeta was supplied), with a path fallback.
func (g *Graph) domainOfID(id string, depth int) string {
	n, ok := g.nodes[id]
	if !ok || n.Kind == graph.KindExternal {
		return "external"
	}
	return domainOf(g.pkgByPath[n.Path], n.Path, depth)
}

// SurpriseConfig tunes surprise scoring. Zero values normalize to the defaults
// below inside ComputeSurprising.
type SurpriseConfig struct {
	DomainDepth    int     // package/path segments that define a domain (default 2)
	Threshold      float64 // a cross-domain pair with share < Threshold is surprising (default 0.10)
	MinDomainEdges int     // min cross-domain out-edges a source domain needs to qualify (default 5)
}

// SurprisingEdge is a rare cross-domain dependency surfaced in an impact result.
type SurprisingEdge struct {
	FromID, ToID         string
	FromName, ToName     string
	FromDomain, ToDomain string
	Score                float64 // 1 - share(FromDomain->ToDomain), 0..1
}

// ComputeSurprising scores the cross-domain edges within the blast radius (the
// target plus result.Hits) against graph-global domain-pair frequencies, and
// returns the surprising ones sorted by Score desc then From,To. Deterministic.
func (g *Graph) ComputeSurprising(targetID string, result *ImpactResult, cfg SurpriseConfig) []SurprisingEdge {
	depth := cfg.DomainDepth
	if depth < 1 {
		depth = 2
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = 0.10
	}
	minEdges := cfg.MinDomainEdges
	if minEdges <= 0 {
		minEdges = 5
	}

	// Graph-global frequency table over all traversable edges.
	count := map[string]map[string]int{}
	crossOut := map[string]int{}
	for _, edges := range g.fwd {
		for _, e := range edges {
			a := g.domainOfID(e.From, depth)
			b := g.domainOfID(e.To, depth)
			if a == b || a == "external" || b == "external" {
				continue
			}
			if count[a] == nil {
				count[a] = map[string]int{}
			}
			count[a][b]++
			crossOut[a]++
		}
	}

	// Candidate edges: those whose endpoints are both within the blast radius.
	inSet := map[string]bool{targetID: true}
	for _, h := range result.Hits {
		inSet[h.Node.ID] = true
	}

	seen := map[string]bool{}
	var out []SurprisingEdge
	for id := range inSet {
		for _, e := range g.fwd[id] {
			if !inSet[e.To] {
				continue
			}
			a := g.domainOfID(e.From, depth)
			b := g.domainOfID(e.To, depth)
			if a == b || a == "external" || b == "external" {
				continue
			}
			if crossOut[a] < minEdges {
				continue
			}
			share := float64(count[a][b]) / float64(crossOut[a])
			if share >= threshold {
				continue
			}
			key := e.From + "\x00" + e.To
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SurprisingEdge{
				FromID: e.From, ToID: e.To,
				FromName: g.nodes[e.From].Name, ToName: g.nodes[e.To].Name,
				FromDomain: a, ToDomain: b,
				Score: 1 - share,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].FromID != out[j].FromID {
			return out[i].FromID < out[j].FromID
		}
		return out[i].ToID < out[j].ToID
	})
	return out
}

// formatSurprisingSection renders the surprising-edges block (no header), capped.
func formatSurprisingSection(edges []SurprisingEdge, maxResults int) string {
	n := len(edges)
	if maxResults > 0 && n > maxResults {
		n = maxResults
	}
	var sb strings.Builder
	sb.WriteString("⚠ Cross-domain dependencies (surprising):\n")
	for _, e := range edges[:n] {
		sb.WriteString(fmt.Sprintf("  %s::%s → %s::%s   [surprise: %.2f]\n",
			e.FromDomain, e.FromName, e.ToDomain, e.ToName, e.Score))
	}
	return sb.String()
}

// FormatSurprising renders only the surprising-edges section (for --surprising-only).
func FormatSurprising(sourceID string, result *ImpactResult, maxResults int) string {
	if result == nil || len(result.SurprisingEdges) == 0 {
		return fmt.Sprintf("No surprising cross-domain dependencies for `%s`.\n", sourceID)
	}
	return fmt.Sprintf("## Surprising dependencies of `%s`\n\n%s",
		sourceID, formatSurprisingSection(result.SurprisingEdges, maxResults))
}
