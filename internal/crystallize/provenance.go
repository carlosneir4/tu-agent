package crystallize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/memory"
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

var sourceHashRe = regexp.MustCompile(`source-hash=([0-9a-f]+)`)

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

// SkillStatus is a cluster's crystallization state.
type SkillStatus int

const (
	StatusNone    SkillStatus = iota // no skill record exists for this cluster
	StatusCurrent                    // a skill exists and matches the current notes
	StatusStale                      // a skill exists but the notes changed since
)

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
// current bytes: write when the file is absent/empty or already crystallize-
// managed; never overwrite a file lacking the marker (a hand-written skill).
func MaterializeDecision(existing []byte) bool {
	return len(existing) == 0 || strings.Contains(string(existing), Marker)
}
