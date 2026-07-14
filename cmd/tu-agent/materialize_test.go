package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestMemoryMaterialize_WritesAndGuards(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	body := "<!-- " + crystallize.Marker + " source-hash=abc label=checkout -->\n---\nname: checkout\n---\nbody\n"
	if _, err := ms.Upsert("skill/checkout", body, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { memMaterializeQuiet = false; memoryMaterializeCmd.SetOut(nil) })
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	path := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected materialized skill at %s: %v", path, err)
	}
	if string(got) != body {
		t.Errorf("materialized content mismatch:\n%s", got)
	}

	// Idempotent: a second run keeps it identical.
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != body {
		t.Error("second materialize changed the file")
	}

	// A hand-written skill WITHOUT the marker is never clobbered.
	hand := filepath.Join(generatedSkillsDir(repoRoot()), "handmade", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(hand), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hand, []byte("hand-written, no marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	ms2, _ := memory.Open(memoryDBPath(repoRoot()))
	_, _ = ms2.Upsert("skill/handmade", "<!-- "+crystallize.Marker+" -->\nGENERATED", memory.UpsertOpts{Type: "skill"})
	ms2.Close()
	if err := memoryMaterializeCmd.RunE(memoryMaterializeCmd, nil); err != nil {
		t.Fatal(err)
	}
	preserved, _ := os.ReadFile(hand)
	if string(preserved) != "hand-written, no marker" {
		t.Errorf("materialize clobbered an unmarked hand-written skill: %q", preserved)
	}
}

func TestMemoryCrystallizeNudge_SilentWhenNoClusters(t *testing.T) {
	t.Chdir(t.TempDir())
	// No notes at all -> no clusters -> nudge prints nothing.
	memCrystallizeNudge = true
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeNudge = false; memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })
	var buf bytes.Buffer
	memoryCrystallizeCmd.SetOut(&buf)
	if err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, nil); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("nudge should be silent with no clusters, got: %q", buf.String())
	}
}

func TestMemoryCrystallizeNudge_SilentWhenAllCurrent(t *testing.T) {
	t.Chdir(t.TempDir())
	// Seed 3 notes that share the domain token "checkout" -> one cluster.
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

	memCrystallizeNudge = true
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeNudge = false; memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })
	var buf bytes.Buffer
	memoryCrystallizeCmd.SetOut(&buf)
	if err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, nil); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("nudge should be silent when all clusters are current, got: %q", buf.String())
	}
}
