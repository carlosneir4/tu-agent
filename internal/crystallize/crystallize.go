// Package crystallize finds dense clusters of related memory observations so
// they can be consolidated into a project skill. It is deterministic and pure:
// clustering is by shared domain tokens drawn from each note's topic-key slug,
// title, and content (the in-memory equivalent of an FTS keyword overlap), so
// it needs no database or FTS index.
package crystallize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/memory"
)

// Cluster is a group of related observations dense enough to crystallize.
type Cluster struct {
	Label   string               // dominant domain token
	Members []memory.Observation // newest first
	Size    int
}

// stopTokens are structural words and note-type words that carry no domain
// signal, so they must never become a cluster label.
var stopTokens = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"when": true, "from": true, "into": true, "per": true, "are": true, "was": true,
	"its": true, "not": true, "but": true, "you": true, "use": true, "via": true,
	// note-type / generic memory vocabulary
	"bug": true, "pattern": true, "decision": true, "architecture": true,
	"testing": true, "reference": true, "gotcha": true, "test": true, "note": true,
}

// Detect groups observations into crystallizable clusters of at least minSize.
// Clusters are disjoint: each note is assigned to its single strongest domain
// token (the valid token it shares with the most other notes). Token selection
// ties are broken by source priority (slug before title before content);
// cluster ranking ties are broken by label for determinism.
//
// Skill records (Type == "skill") are excluded from clustering: they are the
// output of crystallization, not candidate input notes.
func Detect(obs []memory.Observation, minSize int) []Cluster {
	if minSize < 1 {
		minSize = 1
	}
	// Filter out skill records — they are crystallization output, not input.
	filtered := obs[:0:0]
	for _, o := range obs {
		if o.Type != "skill" {
			filtered = append(filtered, o)
		}
	}
	obs = filtered
	tokensOf := make([][]string, len(obs))
	df := map[string]int{}
	for i, o := range obs {
		toks := domainTokens(o)
		tokensOf[i] = toks
		for _, t := range toks {
			df[t]++
		}
	}
	// A token present in more than 60% of all notes is too generic to be a
	// domain signal; only apply this once the corpus is big enough to judge.
	cutoff := 0
	if len(obs) >= 5 {
		cutoff = (len(obs) * 6) / 10
	}
	valid := func(t string) bool {
		if df[t] < 2 {
			return false // shared with no other note — cannot cluster
		}
		if cutoff > 0 && df[t] > cutoff {
			return false // ubiquitous — not distinguishing
		}
		return true
	}
	byToken := map[string][]memory.Observation{}
	for i, o := range obs {
		best, bestN := "", 0
		for _, t := range tokensOf[i] {
			if !valid(t) {
				continue
			}
			// First valid token wins on a DF tie; tokens are ordered slug→title→content.
			if df[t] > bestN {
				best, bestN = t, df[t]
			}
		}
		if best == "" {
			continue
		}
		byToken[best] = append(byToken[best], o)
	}
	clusters := make([]Cluster, 0, len(byToken))
	for tok, members := range byToken {
		if len(members) < minSize {
			continue
		}
		sort.SliceStable(members, func(a, b int) bool {
			return members[a].UpdatedAt.After(members[b].UpdatedAt)
		})
		clusters = append(clusters, Cluster{Label: tok, Members: members, Size: len(members)})
	}
	sort.SliceStable(clusters, func(a, b int) bool {
		if clusters[a].Size != clusters[b].Size {
			return clusters[a].Size > clusters[b].Size
		}
		return clusters[a].Label < clusters[b].Label
	})
	return clusters
}

// domainTokens returns the deduped lowercase domain tokens of one note: the
// topic-key slug (after dropping the leading "type/" segment) plus title and
// content words, with stop/structural words and tokens shorter than 3 removed.
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
		if len(t) < 3 || stopTokens[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
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
