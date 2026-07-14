package main

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// TestCrystallizeNeeds_NoNotes guards the zero case: an empty store has no
// clusters, so nothing needs crystallizing.
func TestCrystallizeNeeds_NoNotes(t *testing.T) {
	t.Chdir(t.TempDir())
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	needs, err := crystallizeNeeds(repoRoot())
	if err != nil {
		t.Fatalf("crystallizeNeeds: %v", err)
	}
	if needs != 0 {
		t.Errorf("needs = %d, want 0", needs)
	}
}

// TestCrystallizeNeeds_OneClusterNeedsCrystallizing seeds a cluster with no
// skill record — it should count as one "needs" cluster.
func TestCrystallizeNeeds_OneClusterNeedsCrystallizing(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := ms.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	needs, err := crystallizeNeeds(repoRoot())
	if err != nil {
		t.Fatalf("crystallizeNeeds: %v", err)
	}
	if needs != 1 {
		t.Errorf("needs = %d, want 1", needs)
	}
}

// TestCrystallizeNeeds_CurrentClusterDoesNotCount mirrors
// TestMemoryCrystallizeNudge_SilentWhenAllCurrent's fixture: a cluster with a
// matching, current skill record must not count toward needs.
func TestCrystallizeNeeds_CurrentClusterDoesNotCount(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	a, err := ms.Upsert("decision/checkout-tax", "apply tax during checkout per region", memory.UpsertOpts{Type: "decision"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ms.Upsert("architecture/checkout-flow", "checkout flow overview", memory.UpsertOpts{Type: "architecture"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ms.Upsert("gotcha/checkout-null", "checkout panics when cart empty", memory.UpsertOpts{Type: "gotcha"})
	if err != nil {
		t.Fatal(err)
	}
	members := []memory.Observation{a, b, c}
	label := "checkout"
	skillContent := crystallize.ProvenanceLine(label, members) + "\n"
	if _, err := ms.Upsert(crystallize.SkillTopic(label), skillContent, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	needs, err := crystallizeNeeds(repoRoot())
	if err != nil {
		t.Fatalf("crystallizeNeeds: %v", err)
	}
	if needs != 0 {
		t.Errorf("needs = %d, want 0 (skill record is current)", needs)
	}
}
