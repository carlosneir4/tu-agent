package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func seedCluster(t *testing.T) {
	t.Helper()
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
}

func TestSaveCrystallizedSkill_StoresRecordWithProvenanceAndMaterializes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	path, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nbody", 0)
	if err != nil {
		t.Fatalf("saveCrystallizedSkill: %v", err)
	}

	// File materialized at the expected path, carrying the provenance marker.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	if want := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if !strings.Contains(string(got), crystallize.Marker) {
		t.Errorf("materialized file missing provenance marker:\n%s", got)
	}
	if !strings.Contains(string(got), "name: checkout") {
		t.Errorf("materialized file missing the body")
	}

	// Record stored at skill/<label> with type skill and a matching source hash.
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	obs, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	var rec *memory.Observation
	for i := range obs {
		if obs[i].TopicKey == crystallize.SkillTopic("checkout") {
			rec = &obs[i]
		}
	}
	if rec == nil {
		t.Fatal("no skill/checkout record stored")
	}
	if rec.Type != "skill" {
		t.Errorf("record type = %q, want skill", rec.Type)
	}
	// Its provenance hash must classify the live cluster as current.
	clusters := crystallize.Detect(obs, memCrystallizeMin)
	var cc *crystallize.Cluster
	for i := range clusters {
		if clusters[i].Label == "checkout" {
			cc = &clusters[i]
		}
	}
	if cc == nil {
		t.Fatal("checkout cluster not detected")
	}
	if crystallize.Classify(*cc, crystallize.ParseSourceHash(rec.Content)) != crystallize.StatusCurrent {
		t.Error("stored skill should classify the cluster as current")
	}
}

func TestSaveCrystallizedSkill_UnknownLabelErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t) // a "checkout" cluster exists; "nonexistent" does not
	_, err := saveCrystallizedSkill("nonexistent", "body", 0)
	if err == nil {
		t.Fatal("expected an error for an unknown cluster label")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should name the missing label; got: %v", err)
	}
}

func TestMemoryCrystallizeCLI_UnknownLabelErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })
	memCrystallizeMin = 3
	seedCluster(t) // a "checkout" cluster exists

	memoryCrystallizeCmd.SetOut(new(strings.Builder))
	err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected an error for an unknown cluster label")
	}
	if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("error should list available labels (checkout); got: %v", err)
	}
}
