package crystallize

// RED-phase tests for @feature:orphan-detection-status (scenarios @s1..@s3).
// They reference production API the implementer will ADD to provenance.go and
// therefore FAIL TO COMPILE until it exists (correct RED):
//   - StatusOrphan SkillStatus   — a new enum value AFTER StatusStale.
//   - RecordLabel(o)             — a skill record's bound label (parsed label,
//                                  else legacy TrimPrefix of "skill/" topic).
//   - RecordStatus(o, byLabel)   — classify a record against live clusters keyed
//                                  by label: StatusOrphan when the label matches
//                                  no live cluster, else Classify(...).
//
// A skill record here is a Type=="skill" observation whose Content carries a
// crystallize provenance line (ProvenanceLine embeds the source-hash + label).
// Fixtures are generic and fictional (acme-*) per repo §9.

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// skillRec builds a skill record: Type "skill", topic skill/<name>, and content
// of frontmatter + provenance line (source-hash over members, plus label) +
// prose. The stored source-hash is SourceHash(members), so passing a cluster's
// live members yields a [current] match and passing changed members a [stale]
// mismatch.
func skillRec(name, label string, members []memory.Observation) memory.Observation {
	content := "---\nname: " + name + "\n---\n" +
		ProvenanceLine(label, members) + "\n" +
		"prose for the " + label + " skill\n"
	return memory.Observation{
		Type:     "skill",
		TopicKey: "skill/" + name,
		Content:  content,
		Revision: 1,
	}
}

// @s1 — ORPHAN is distinguished from STALE. A record bound to a live cluster
// whose members changed is [stale]; a record bound to a label with NO live
// cluster is [orphan]. The two states are distinct.
func TestRecordStatus_OrphanDistinguishedFromStale(t *testing.T) {
	checkout := Cluster{
		Label:   "acme-checkout",
		Members: []memory.Observation{ob("gotcha/acme-checkout-1", 1), ob("testing/acme-checkout-2", 1)},
		Size:    2,
	}
	billing := Cluster{
		Label:   "acme-billing",
		Members: []memory.Observation{ob("decision/acme-billing-1", 1), ob("gotcha/acme-billing-2", 1)},
		Size:    2,
	}
	byLabel := map[string]Cluster{
		checkout.Label: checkout,
		billing.Label:  billing,
	}

	// Record A: bound to the live acme-checkout cluster but its stored hash was
	// computed over an OLDER member revision, so the cluster changed since.
	oldMembers := []memory.Observation{ob("gotcha/acme-checkout-1", 1), ob("testing/acme-checkout-2", 2)}
	recStale := skillRec("acme-checkout", "acme-checkout", oldMembers)
	if got := RecordStatus(recStale, byLabel); got != StatusStale {
		t.Errorf("record bound to a live-but-changed cluster: RecordStatus = %v, want StatusStale", got)
	}

	// Record B: bound to acme-deprecated, which is NOT a live cluster label.
	recOrphan := skillRec("acme-deprecated", "acme-deprecated", checkout.Members)
	if got := RecordStatus(recOrphan, byLabel); got != StatusOrphan {
		t.Errorf("record bound to a vanished cluster: RecordStatus = %v, want StatusOrphan", got)
	}

	if StatusStale == StatusOrphan {
		t.Error("StatusStale and StatusOrphan must be distinct states")
	}
}

// @s2 — the join binds by the PARSED label, not the topic key. A record stored
// under a curated topic name that differs from its provenance label still keys
// to the live cluster named by the label (here matching its hash → current), not
// to a phantom cluster named after the topic.
func TestRecordStatus_BindsByParsedLabelNotTopicKey(t *testing.T) {
	checkout := Cluster{
		Label:   "acme-checkout",
		Members: []memory.Observation{ob("gotcha/acme-checkout-1", 1), ob("testing/acme-checkout-2", 1)},
		Size:    2,
	}
	byLabel := map[string]Cluster{checkout.Label: checkout}

	// Curated topic name (my-curated-name) != label (acme-checkout); stored hash
	// matches the live cluster's members.
	rec := skillRec("my-curated-name", "acme-checkout", checkout.Members)

	if got := RecordStatus(rec, byLabel); got == StatusOrphan {
		t.Fatalf("record keyed by curated topic name would be orphan; it must key by label acme-checkout: got %v", got)
	}
	if got := RecordStatus(rec, byLabel); got != StatusCurrent {
		t.Errorf("hash matches the live cluster: RecordStatus = %v, want StatusCurrent", got)
	}
}

// @s3 — legacy fallback. A record with NO label= in its provenance derives its
// bound label from the topic key (TrimPrefix "skill/"); a record WITH a label=
// returns the parsed label.
func TestRecordLabel_LegacyFallbackAndParsed(t *testing.T) {
	legacy := memory.Observation{
		Type:     "skill",
		TopicKey: "skill/legacy-theme",
		Content:  "<!-- " + Marker + " source-hash=deadbeef -->\nold body without a label\n",
		Revision: 1,
	}
	if got := RecordLabel(legacy); got != "legacy-theme" {
		t.Errorf("legacy record (no label=): RecordLabel = %q, want %q (TrimPrefix of skill/ topic)", got, "legacy-theme")
	}

	withLabel := skillRec("my-curated-name", "acme-widget", []memory.Observation{ob("gotcha/acme-widget-1", 1)})
	if got := RecordLabel(withLabel); got != "acme-widget" {
		t.Errorf("record with label=: RecordLabel = %q, want %q (the parsed label, not the topic name)", got, "acme-widget")
	}
}
