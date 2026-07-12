// Package crystallize finds dense clusters of related memory observations so
// they can be consolidated into a project skill. It is deterministic and pure:
// clustering is by COHESION — the Jaccard similarity of each note's
// "specific" tokens (domain tokens drawn from the topic-key slug, title, and
// content, with stopwords and broad "umbrella" tokens removed) — so it needs
// no database or FTS index.
package crystallize

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/cluster"
	"github.com/tu/tu-agent/internal/memory"
)

// Cluster is a group of related observations dense enough to crystallize.
type Cluster struct {
	Label   string               // dominant specific token among members
	Members []memory.Observation // newest first
	Size    int
}

// cohesionThreshold is the minimum Jaccard similarity of two notes' specific-
// token sets for an edge to form between them in the note-similarity graph.
// The design doc's starting default was 0.15; validating against the
// existing "three short notes sharing one domain word" integration fixtures
// (cmd/tu-agent's seedCluster / materialize checkout fixtures) showed 0.15
// under-splits real short notes: a single genuinely shared word (e.g.
// "checkout") between two ~4-5-specific-token notes naturally lands in the
// 0.11-0.17 Jaccard range, below 0.15, and those notes must still cohere.
// 0.125 is the smallest adjustment that keeps every such fixture connected
// (each note reaches at least one same-Jaccard-or-higher neighbor) while
// every cross-theme pair the design requires to stay separate has exactly
// zero specific-token overlap (not a near-miss), so lowering the threshold
// does not risk merging them.
const cohesionThreshold = 0.125

// umbrellaCutoff is the document-frequency ratio (df/len(obs)) above which a
// token is considered an "umbrella" token — broad enough to be shared by many
// otherwise-unrelated notes — and is demoted out of every note's specific-token
// set. It only applies once the corpus is large enough to judge (see
// umbrellaMinCorpus): on a tiny corpus no token is demoted.
const umbrellaCutoff = 0.4

// umbrellaMinCorpus is the minimum filtered-observation count before umbrella
// demotion applies at all. Below it, no token is demoted: on a small corpus a
// 40%-of-notes ratio can come from one dense sub-cluster's own vocabulary
// rather than genuine cross-theme breadth (in a 7-note two-cluster corpus a
// token shared by a dominant 4-of-7 cluster trips 0.4 purely from corpus size —
// see TestDetect_RanksLargerFirstThenLabel), which would wrongly erase that
// cluster's own specific tokens.
const umbrellaMinCorpus = 8

// quantScale maps a Jaccard similarity in [0,1] onto an integer edge weight for
// the community primitive, which accumulates weights as exact integers so the
// partition is invariant under edge (map) iteration order.
const quantScale = 10000

// quantize maps a Jaccard similarity in [0,1] to an integer edge weight so the
// community primitive accumulates weights exactly (map-order-invariant).
func quantize(j float64) int { return int(math.Round(j * quantScale)) }

// stopTokens are structural words and note-type words that carry no domain
// signal, so they must never become a cluster label or drive clustering.
// Includes English structural words, note-type/generic memory vocabulary, and
// common Spanish function words (users write notes in both languages).
var stopTokens = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"when": true, "from": true, "into": true, "per": true, "are": true, "was": true,
	"its": true, "not": true, "but": true, "you": true, "use": true, "via": true,
	// note-type / generic memory vocabulary
	"bug": true, "pattern": true, "decision": true, "architecture": true,
	"testing": true, "reference": true, "gotcha": true, "test": true, "note": true,
	// workflow / process words (carry no domain signal)
	"feature": true, "run": true, "spec": true, "scope": true, "status": true,
	"task": true, "fix": true, "legacy": true, "null": true, "real": true,
	"only": true, "related": true, "new": true, "set": true, "one": true, "never": true,
	// Spanish structural words
	"que": true, "para": true, "con": true, "los": true, "las": true, "del": true,
	"una": true, "por": true, "como": true, "este": true, "esta": true, "esto": true,
	"pero": true, "desde": true, "sobre": true, "entre": true, "sin": true, "hay": true,
	"son": true, "más": true,
}

// Detect groups observations into crystallizable clusters of at least
// minSize, using cohesion: two notes join the same cluster only when the
// Jaccard similarity of their specific-token sets (domain tokens minus
// stopwords minus umbrella tokens) is at least cohesionThreshold. Clusters are
// the modularity-maximizing communities of that note-similarity graph, so dense
// sub-communities separate rather than single-linkage chaining every theme into
// one blob through a few bridging notes; a note with no qualifying edge to any
// other note is a loose note and is left out of every cluster (not
// force-assigned to the nearest one). Each cluster is labeled by
// its most-common specific token among members, alphabetical tie-break — never
// a stopword or umbrella token, since both are excluded from the specific set.
//
// Detect is pure, in-memory, and deterministic: notes are processed in a fixed
// order (TopicKey, then Title, then original index) so the same input always
// yields the same clusters, members, and ordering regardless of input order.
//
// Skill records (Type == "skill") are excluded from clustering: they are the
// output of crystallization, not candidate input notes.
func Detect(obs []memory.Observation, minSize int) []Cluster {
	if minSize < 1 {
		minSize = 1
	}
	// Filter out skill records — they are crystallization output, not input.
	// This happens before any document-frequency computation so skill notes
	// never count toward corpus size or umbrella-token detection.
	filtered := obs[:0:0]
	for _, o := range obs {
		if o.Type != "skill" {
			filtered = append(filtered, o)
		}
	}
	obs = filtered
	n := len(obs)
	if n == 0 {
		return nil
	}

	// Deterministic processing order: (TopicKey, Title, original index).
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if obs[ia].TopicKey != obs[ib].TopicKey {
			return obs[ia].TopicKey < obs[ib].TopicKey
		}
		if obs[ia].Title != obs[ib].Title {
			return obs[ia].Title < obs[ib].Title
		}
		return ia < ib
	})

	// Raw domain tokens (post stopword/length filter, pre-umbrella-demotion)
	// and their document frequency across the corpus.
	tokensOf := make([][]string, n)
	df := map[string]int{}
	for i, o := range obs {
		toks := domainTokens(o)
		tokensOf[i] = toks
		for _, t := range toks {
			df[t]++
		}
	}

	isUmbrella := func(string) bool { return false }
	if n >= umbrellaMinCorpus {
		isUmbrella = func(t string) bool {
			return float64(df[t])/float64(n) > umbrellaCutoff
		}
	}

	// Specific-token set per note: domain tokens minus umbrella tokens.
	// Umbrella demotion happens here, before similarity is computed, so an
	// umbrella-only note ends with an empty specific set.
	sets := make([]map[string]bool, n)
	for i, toks := range tokensOf {
		m := map[string]bool{}
		for _, t := range toks {
			if !isUmbrella(t) {
				m[t] = true
			}
		}
		sets[i] = m
	}

	// Note-similarity graph over the CANONICAL ranks: node ai (in [0,n)) is the
	// note obs[order[ai]]. Building over ranks rather than raw input indices
	// keeps the community partition invariant under input shuffle. An edge forms
	// when Jaccard(specific sets) >= threshold, weighted by the quantized
	// similarity so denser pairs pull harder.
	edges := make([]cluster.Edge, 0, n)
	for ai := range n {
		for bi := ai + 1; bi < n; bi++ {
			j := jaccard(sets[order[ai]], sets[order[bi]])
			if j < cohesionThreshold {
				continue
			}
			w := quantize(j)
			if w == 0 {
				continue // a zero-weight edge is a non-edge
			}
			edges = append(edges, cluster.Edge{A: ai, B: bi, Weight: w})
		}
	}

	// Modularity-maximizing communities over the note-similarity graph. Dense
	// sub-communities separate instead of single-linkage chaining every theme
	// into one blob. cluster.Communities is deterministic and returns canonical
	// ranks; convert each community back to original observation indices. A note
	// with no qualifying edge is returned as its own singleton community and is
	// left out once the minSize filter below runs (unless minSize is 1).
	comms := cluster.Communities(n, edges)
	components := make([][]int, 0, len(comms))
	for _, c := range comms {
		comp := make([]int, len(c))
		for k, rank := range c {
			comp[k] = order[rank]
		}
		components = append(components, comp)
	}

	// Build each qualifying component, then assign labels. The label is a
	// LOAD-BEARING key: downstream storage addresses a single cluster by it
	// (crystallize.SkillName/SkillTopic, and the status map in
	// FormatWithStatus), so labels MUST be unique across clusters. The old
	// token-keyed grouping guaranteed uniqueness by construction; cohesion
	// clustering does not — two disconnected components can share their
	// most-common specific token — so uniqueness is enforced here.
	type pending struct {
		members  []memory.Observation
		ranked   []string // specific tokens, most-common first (alpha tie-break)
		firstKey string   // smallest member TopicKey — deterministic order key
	}
	pend := make([]pending, 0, len(components))
	for _, comp := range components {
		if len(comp) < minSize {
			continue
		}
		members := make([]memory.Observation, len(comp))
		tokenCount := map[string]int{}
		firstKey := ""
		for k, idx := range comp {
			members[k] = obs[idx]
			if firstKey == "" || obs[idx].TopicKey < firstKey {
				firstKey = obs[idx].TopicKey
			}
			for t := range sets[idx] {
				tokenCount[t]++
			}
		}
		sort.SliceStable(members, func(a, b int) bool {
			return members[a].UpdatedAt.After(members[b].UpdatedAt)
		})
		pend = append(pend, pending{members: members, ranked: rankedTokens(tokenCount), firstKey: firstKey})
	}

	// Assign labels in a deterministic order — larger clusters first (they earn
	// the stronger token), smallest member TopicKey breaking ties — giving each
	// cluster its highest-ranked specific token not already taken.
	sort.SliceStable(pend, func(a, b int) bool {
		if len(pend[a].members) != len(pend[b].members) {
			return len(pend[a].members) > len(pend[b].members)
		}
		return pend[a].firstKey < pend[b].firstKey
	})
	used := map[string]bool{}
	clusters := make([]Cluster, 0, len(pend))
	for _, p := range pend {
		label := uniqueLabel(p.ranked, used)
		used[label] = true
		clusters = append(clusters, Cluster{Label: label, Members: p.members, Size: len(p.members)})
	}
	sort.SliceStable(clusters, func(a, b int) bool {
		if clusters[a].Size != clusters[b].Size {
			return clusters[a].Size > clusters[b].Size
		}
		return clusters[a].Label < clusters[b].Label
	})
	return clusters
}

// jaccard returns |a∩b| / |a∪b| for two token sets. Two empty sets (or one
// empty set) have zero similarity — never a divide-by-zero.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, big := a, b
	if len(a) > len(b) {
		small, big = b, a
	}
	inter := 0
	for t := range small {
		if big[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// rankedTokens orders a cluster's specific tokens by membership count
// descending, then alphabetically — the cluster's label-preference order. The
// result is independent of map iteration order (a total order breaks every
// tie).
func rankedTokens(counts map[string]int) []string {
	toks := make([]string, 0, len(counts))
	for t := range counts {
		toks = append(toks, t)
	}
	sort.Slice(toks, func(a, b int) bool {
		if counts[toks[a]] != counts[toks[b]] {
			return counts[toks[a]] > counts[toks[b]]
		}
		return toks[a] < toks[b]
	})
	return toks
}

// uniqueLabel returns the highest-ranked specific token not already used by an
// earlier cluster, so no two clusters share a label. If every candidate is
// taken — or the cluster has no specific tokens at all (a degenerate empty set,
// only reachable when the caller passes minSize < 2) — it appends the smallest
// free numeric suffix to the primary token, or to "cluster" when there are no
// tokens (e.g. "checkout-2", "cluster-2"). Deterministic given the candidate
// order and the used set.
func uniqueLabel(ranked []string, used map[string]bool) string {
	for _, t := range ranked {
		if !used[t] {
			return t
		}
	}
	base := "cluster"
	if len(ranked) > 0 {
		base = ranked[0]
	}
	for i := 2; ; i++ {
		if cand := fmt.Sprintf("%s-%d", base, i); !used[cand] {
			return cand
		}
	}
}

// domainTokens returns the deduped lowercase domain tokens of one note: the
// topic-key slug (after dropping the leading "type/" segment) plus title and
// content words, with stop/structural words, all-digit tokens, and tokens
// shorter than 3 removed. Tokens that merely contain digits ("oauth2", "v2")
// are kept — only bare numbers are dropped.
func domainTokens(o memory.Observation) []string {
	key := o.TopicKey
	if i := strings.IndexByte(key, '/'); i >= 0 {
		key = key[i+1:]
	}
	var raw []string
	raw = append(raw, splitTokens(key)...)
	raw = append(raw, splitTokens(o.Title)...)
	raw = append(raw, splitTokens(o.Content)...)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.ToLower(t)
		if len(t) < 3 || stopTokens[t] || isAllDigits(t) || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// isAllDigits reports whether t is non-empty and every rune is a decimal digit.
// A bare number carries no domain signal, so it is dropped like a stopword;
// tokens that merely contain digits ("oauth2") return false and are kept.
func isAllDigits(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func splitTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
}

// Format renders clusters as a human-readable list for the CLI and MCP tool.
func Format(clusters []Cluster) string {
	if len(clusters) == 0 {
		return "no crystallizable clusters (need more related notes on one topic)\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d crystallizable cluster(s):\n\n", len(clusters))
	for _, c := range clusters {
		fmt.Fprintf(&b, "[%d notes] %s\n", c.Size, c.Label)
		for _, m := range c.Members {
			key := m.TopicKey
			if key == "" {
				key = m.Title
			}
			fmt.Fprintf(&b, "  %s\n", key)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatWithStatus renders clusters like Format but annotates each with its
// crystallization status ([skill], [stale], or [none]) from the status map
// (keyed by cluster label). Missing entries render as [none].
func FormatWithStatus(clusters []Cluster, status map[string]SkillStatus) string {
	if len(clusters) == 0 {
		return "no crystallizable clusters (need more related notes on one topic)\n"
	}
	tag := func(s SkillStatus) string {
		switch s {
		case StatusCurrent:
			return "[skill]"
		case StatusStale:
			return "[stale]"
		default:
			return "[none]"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d crystallizable cluster(s):\n\n", len(clusters))
	for _, c := range clusters {
		fmt.Fprintf(&b, "%s [%d notes] %s\n", tag(status[c.Label]), c.Size, c.Label)
		for _, m := range c.Members {
			key := m.TopicKey
			if key == "" {
				key = m.Title
			}
			fmt.Fprintf(&b, "  %s\n", key)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
