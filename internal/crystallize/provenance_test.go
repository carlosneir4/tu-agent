package crystallize

import (
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func ob(topic string, rev int) memory.Observation {
	return memory.Observation{TopicKey: topic, Revision: rev, UpdatedAt: time.Now()}
}

func TestSourceHash_StableAndRevisionSensitive(t *testing.T) {
	a := []memory.Observation{ob("testing/x", 1), ob("gotcha/y", 2)}
	b := []memory.Observation{ob("gotcha/y", 2), ob("testing/x", 1)} // reordered
	if SourceHash(a) != SourceHash(b) {
		t.Error("hash must be order-independent")
	}
	c := []memory.Observation{ob("testing/x", 1), ob("gotcha/y", 3)} // rev bumped
	if SourceHash(a) == SourceHash(c) {
		t.Error("hash must change when a member revision changes")
	}
}

func TestProvenanceLine_RoundTrip(t *testing.T) {
	members := []memory.Observation{ob("testing/x", 1)}
	line := ProvenanceLine("checkout", members)
	if got := ParseSourceHash(line); got != SourceHash(members) {
		t.Errorf("ParseSourceHash=%q want %q", got, SourceHash(members))
	}
	if ParseSourceHash("no marker here") != "" {
		t.Error("missing marker should parse to empty hash")
	}
}

func TestClassify(t *testing.T) {
	members := []memory.Observation{ob("testing/x", 1)}
	c := Cluster{Label: "checkout", Members: members, Size: 1}
	if Classify(c, "") != StatusNone {
		t.Error("no stored hash -> StatusNone")
	}
	if Classify(c, SourceHash(members)) != StatusCurrent {
		t.Error("matching hash -> StatusCurrent")
	}
	if Classify(c, "deadbeef") != StatusStale {
		t.Error("mismatched hash -> StatusStale")
	}
}

func TestMaterializeDecision(t *testing.T) {
	if !MaterializeDecision(nil) {
		t.Error("absent file -> write")
	}
	if !MaterializeDecision([]byte("  \n\t")) {
		t.Error("whitespace-only file -> write")
	}
	genuine := "x " + ProvenanceLine("checkout", []memory.Observation{ob("testing/x", 1)}) + " y"
	if !MaterializeDecision([]byte(genuine)) {
		t.Error("file with a genuine provenance line -> overwrite allowed")
	}
	if MaterializeDecision([]byte("hand-written skill, no marker")) {
		t.Error("unmarked file -> do NOT clobber")
	}
}

// TestMaterializeDecision_RejectsSubstringMentionWithoutProvenance is the
// RED case for the fix: a hand-written skill that merely mentions the marker
// string in prose (e.g. documenting the crystallize format) has no valid
// provenance line and must NOT be classified as crystallize-managed —
// otherwise it could be silently overwritten.
func TestMaterializeDecision_RejectsSubstringMentionWithoutProvenance(t *testing.T) {
	prose := "This skill documents our conventions. Note: generated skills use the " +
		Marker + " marker in a provenance comment, but this file is hand-written."
	if MaterializeDecision([]byte(prose)) {
		t.Error("prose merely mentioning the marker (no valid provenance line) must NOT be treated as crystallize-managed")
	}
}

func TestParseSourceHash_IgnoresForeignMarker(t *testing.T) {
	if got := ParseSourceHash("<!-- other-tool source-hash=abc123 -->"); got != "" {
		t.Errorf("source-hash without the crystallize Marker must parse to empty, got %q", got)
	}
}

// TestProvenanceCommentRe_OwnsFullComment documents that crystallize owns the
// canonical matcher for the whole provenance comment (moved here from
// reconcile): it matches a genuine ProvenanceLine output in full and rejects an
// unrelated HTML comment that lacks the crystallize Marker.
func TestProvenanceCommentRe_OwnsFullComment(t *testing.T) {
	members := []memory.Observation{ob("testing/x", 1)}
	line := ProvenanceLine("checkout", members)
	if got := ProvenanceCommentRe.FindString(line); got != line {
		t.Errorf("ProvenanceCommentRe must match the full provenance comment: got %q want %q", got, line)
	}
	if ProvenanceCommentRe.MatchString("<!-- other-tool label=checkout -->") {
		t.Error("ProvenanceCommentRe must NOT match an HTML comment lacking the crystallize Marker")
	}
}

func TestSkillNameAndTopic(t *testing.T) {
	if SkillName("checkout") != "checkout" {
		t.Errorf("SkillName identity expected")
	}
	if SkillTopic("checkout") != "skill/checkout" {
		t.Errorf("SkillTopic = %q, want skill/checkout", SkillTopic("checkout"))
	}
}
