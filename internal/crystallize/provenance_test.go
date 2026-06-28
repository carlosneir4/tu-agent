package crystallize

import (
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/memory"
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
	if !MaterializeDecision([]byte("x <!-- tu-agent:crystallize ... --> y")) {
		t.Error("marked file -> overwrite allowed")
	}
	if MaterializeDecision([]byte("hand-written skill, no marker")) {
		t.Error("unmarked file -> do NOT clobber")
	}
}

func TestParseSourceHash_IgnoresForeignMarker(t *testing.T) {
	if got := ParseSourceHash("<!-- other-tool source-hash=abc123 -->"); got != "" {
		t.Errorf("source-hash without the crystallize Marker must parse to empty, got %q", got)
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
