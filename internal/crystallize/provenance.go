package crystallize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// Marker tags a materialized SKILL.md as crystallize-managed, so materialization
// may overwrite it but never a hand-written skill.
const Marker = "tu-agent:crystallize"

// SourceHash is a stable hash over a cluster's member notes (topic key +
// revision), independent of order. It changes when a member is added, removed,
// or revised — the basis for staleness detection.
func SourceHash(members []memory.Observation) string {
	keys := make([]string, 0, len(members))
	for _, m := range members {
		keys = append(keys, fmt.Sprintf("%s@%d", m.TopicKey, m.Revision))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// ProvenanceLine is the marker comment embedded at the top of a generated
// SKILL.md body. It records the source hash so re-generation and staleness can
// be detected.
func ProvenanceLine(label string, members []memory.Observation) string {
	return fmt.Sprintf("<!-- %s source-hash=%s label=%s -->", Marker, SourceHash(members), label)
}

// ProvenanceCommentRe matches the entire crystallize provenance HTML comment
// (`<!-- tu-agent:crystallize ... -->`) so it can be rewritten in place with a
// fresh label= and source-hash=. It is the single canonical matcher for the
// full comment; consumers (e.g. reconcile's rename path) reference it rather
// than defining their own copy.
var ProvenanceCommentRe = regexp.MustCompile(`<!--\s*` + regexp.QuoteMeta(Marker) + `[^>]*-->`)

var sourceHashRe = regexp.MustCompile(`source-hash=([0-9a-f]+)`)

var labelRe = regexp.MustCompile(`label=([0-9A-Za-z-]+)`)

// ParseSourceHash extracts the source hash from content carrying a provenance
// line, or "" if none is present.
func ParseSourceHash(content string) string {
	if !strings.Contains(content, Marker) {
		return ""
	}
	m := sourceHashRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseLabel extracts the cluster label from content carrying a provenance
// line, or "" if none is present.
func ParseLabel(content string) string {
	if !strings.Contains(content, Marker) {
		return ""
	}
	m := labelRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// SkillStatus is a cluster's crystallization state.
type SkillStatus int

const (
	StatusNone    SkillStatus = iota // no skill record exists for this cluster
	StatusCurrent                    // a skill exists and matches the current notes
	StatusStale                      // a skill exists but the notes changed since
	// StatusOrphan: a skill record whose bound label matches no live cluster —
	// distinct from StatusStale, whose cluster still exists but whose members
	// changed.
	StatusOrphan
)

// RecordLabel returns the bound label of a skill record: its parsed provenance
// label, or the topic-derived name (TrimPrefix "skill/") for legacy records
// written without a label= field.
func RecordLabel(o memory.Observation) string {
	if l := ParseLabel(o.Content); l != "" {
		return l
	}
	return strings.TrimPrefix(o.TopicKey, "skill/")
}

// RecordStatus classifies a skill record against the live clusters keyed by
// label: StatusOrphan when its bound label matches no live cluster, else it
// delegates to Classify against the matched cluster.
func RecordStatus(o memory.Observation, byLabel map[string]Cluster) SkillStatus {
	label := RecordLabel(o)
	c, ok := byLabel[label]
	if !ok {
		return StatusOrphan
	}
	return Classify(c, ParseSourceHash(o.Content))
}

// Classify compares a cluster against the source hash stored in its skill record
// (storedHash == "" means no record exists).
func Classify(c Cluster, storedHash string) SkillStatus {
	switch {
	case storedHash == "":
		return StatusNone
	case storedHash == SourceHash(c.Members):
		return StatusCurrent
	default:
		return StatusStale
	}
}

// SkillName maps a cluster label to its skill name (the <name> in topic
// skill/<name> and in .claude/skills/<name>). It is the single source of truth
// for this mapping so the producer (generation) and consumers (status, nudge,
// materialize) never diverge. v1 is identity; keep all callers routed through
// here if the derivation ever changes.
func SkillName(label string) string { return label }

// SkillTopic returns the memory topic key for a cluster label's skill record.
func SkillTopic(label string) string { return "skill/" + SkillName(label) }

// MaterializeDecision reports whether materialization may write a file given its
// current bytes: write when the file is absent/whitespace-only or already
// crystallize-managed (a genuine, parseable provenance line — not merely a
// file that mentions the marker string in prose); never overwrite a file
// lacking a valid provenance line (a hand-written skill).
func MaterializeDecision(existing []byte) bool {
	return len(strings.TrimSpace(string(existing))) == 0 || ParseSourceHash(string(existing)) != ""
}
