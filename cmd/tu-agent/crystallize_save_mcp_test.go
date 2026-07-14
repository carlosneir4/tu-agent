package main

import (
	"context"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestHandleCrystallizeSave_StoresAndMaterializes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t) // from crystallize_gen_test.go

	_, out, err := handleCrystallizeSave(context.Background(), nil,
		crystallizeSaveMCPInput{Label: "checkout", Body: "---\nname: checkout\n---\nbody"})
	if err != nil {
		t.Fatalf("handleCrystallizeSave: %v", err)
	}
	if !strings.Contains(out.Result, "checkout") {
		t.Errorf("expected the saved path in the result, got: %q", out.Result)
	}
	ms, _ := memory.Open(memoryDBPath(repoRoot()))
	defer ms.Close()
	obs, _ := ms.List()
	found := false
	for _, o := range obs {
		if o.TopicKey == crystallize.SkillTopic("checkout") && o.Type == "skill" {
			found = true
		}
	}
	if !found {
		t.Error("crystallize_save did not store the skill/checkout record")
	}
}

func seedCluster4(t *testing.T) {
	t.Helper()
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
		{"reference/checkout-invoice", "reference", "checkout invoice number"},
	} {
		if _, err := ms.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestHandleCrystallizeSave_MinParity locks in the mem_clusters(min:3) parity
// fix: an agent that discovered a 4-note cluster via mem_clusters(min:3) must
// be able to save it, even though the package default (memCrystallizeMin) is
// 5 and would not have surfaced that cluster.
func TestHandleCrystallizeSave_MinParity(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 5 // package default; the 4-note cluster is below it
	seedCluster4(t)

	// Without min, re-detection at the default (5) does not surface the
	// 4-note cluster: same "no current cluster" error as an unknown label.
	_, _, err := handleCrystallizeSave(context.Background(), nil,
		crystallizeSaveMCPInput{Label: "checkout", Body: "---\nname: checkout\n---\nbody"})
	if err == nil {
		t.Fatal("expected an error saving a 4-note cluster at the default min (5)")
	}
	if !strings.Contains(err.Error(), "no current cluster labeled") {
		t.Errorf("expected a no-such-cluster error, got: %v", err)
	}

	// With min:3 (matching what mem_clusters(min:3) showed the agent), it
	// must succeed.
	_, out, err := handleCrystallizeSave(context.Background(), nil,
		crystallizeSaveMCPInput{Label: "checkout", Body: "---\nname: checkout\n---\nbody", Min: 3})
	if err != nil {
		t.Fatalf("handleCrystallizeSave with min:3: %v", err)
	}
	if !strings.Contains(out.Result, "checkout") {
		t.Errorf("expected the saved path in the result, got: %q", out.Result)
	}
	ms, _ := memory.Open(memoryDBPath(repoRoot()))
	defer ms.Close()
	obs, _ := ms.List()
	found := false
	for _, o := range obs {
		if o.TopicKey == crystallize.SkillTopic("checkout") && o.Type == "skill" {
			found = true
		}
	}
	if !found {
		t.Error("crystallize_save with min:3 did not store the skill/checkout record")
	}
}

func TestCrystallizeSaveInMCPToolNames(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["crystallize_save"] {
		t.Error("newMCPServer does not serve crystallize_save")
	}
}
