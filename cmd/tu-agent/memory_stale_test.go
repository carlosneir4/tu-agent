package main

import (
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

type fakeNodes struct {
	count int
	live  map[string]bool
}

func (f fakeNodes) NodeCount() (int, error) { return f.count, nil }
func (f fakeNodes) ExistingNodeIDs(ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if f.live[id] {
			out[id] = true
		}
	}
	return out, nil
}

func TestStaleNodeRefs(t *testing.T) {
	ms, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()

	o1, err := ms.Upsert("decision/x", "body", memory.UpsertOpts{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	o2, err := ms.Upsert("decision/y", "body", memory.UpsertOpts{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// o1 links to one live node and one deleted node; o2 links only to a live node.
	// Also test obs↔obs filtering: o1 links to o2 (observation).
	for _, rel := range []struct{ from, to string }{
		{o1.ID, "svc.go::Live"},
		{o1.ID, "svc.go::Gone"},
		{o2.ID, "svc.go::Live"},
		{o1.ID, o2.ID},
	} {
		if _, err := ms.Relate(rel.from, rel.to, "documents"); err != nil {
			t.Fatal(err)
		}
	}
	obs := []memory.Observation{o1, o2}

	gs := fakeNodes{count: 10, live: map[string]bool{"svc.go::Live": true}}
	got := staleNodeRefs(ms, gs, obs)
	if got[o1.ID] != 1 {
		t.Errorf("o1 stale = %d, want 1 (svc.go::Gone missing)", got[o1.ID])
	}
	if got[o2.ID] != 0 {
		t.Errorf("o2 stale = %d, want 0", got[o2.ID])
	}

	// Degrade: nil checker and empty graph both yield no annotations.
	if staleNodeRefs(ms, nil, obs) != nil {
		t.Error("nil checker should yield nil")
	}
	if r := staleNodeRefs(ms, fakeNodes{count: 0}, obs); r != nil {
		t.Errorf("empty graph should yield nil, got %v", r)
	}
}
