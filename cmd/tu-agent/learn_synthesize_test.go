package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/graph/store"
)

func writeFileTree(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedConcept seeds one concept row into the graph store at the current working
// directory. Must be called after t.Chdir(root) so openGraphStore picks up the
// correct .tu-agent/graph.db.
func seedConcept(t *testing.T) {
	t.Helper()
	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "widget rendering", Content: "---\nname: widgets\ndescription: widget rendering\n---\n- core/src/main/java/Widget.java: the type\n"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	st.Close()
}

func TestPrepareSynthesisInputs_GraphAndDomainEdges(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "src/com/acme/widgets/Widget.java",
		"package com.acme.widgets;\nimport com.acme.render.Renderer;\npublic class Widget {}\n")
	writeFileTree(t, root, "src/com/acme/render/Renderer.java",
		"package com.acme.render;\npublic class Renderer {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\n## Key Files\n- src/com/acme/widgets/Widget.java\n"},
		{Name: "render", Description: "render", Content: "---\nname: render\ndescription: render\n---\n## Key Files\n- src/com/acme/render/Renderer.java\n"},
	}); err != nil {
		t.Fatalf("seed concepts: %v", err)
	}
	st.Close()

	project, domains, edges, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	if project == "" {
		t.Error("expected non-empty project name")
	}
	if len(domains) != 2 {
		t.Errorf("domains = %d, want 2", len(domains))
	}
	if len(edges) != 1 || edges[0].From != "widgets" || edges[0].To != "render" {
		t.Errorf("edges = %v, want [widgets->render]", edges)
	}
	// prepareSynthesisInputs writes skill-fingerprints.json as a side effect.
	if _, err := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); err != nil {
		t.Errorf("skill-fingerprints.json not written: %v", err)
	}
}

func TestPrepareSynthesisInputs_NoSkillsErrors(t *testing.T) {
	root := t.TempDir()

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	// No concepts seeded — store is empty. prepareSynthesisInputs must error.
	if _, _, _, err := prepareSynthesisInputs(".", "", false); err == nil {
		t.Error("expected error when no concepts present")
	}
}

func TestRunStatus_NoSkillsErrors(t *testing.T) {
	root := t.TempDir()

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	// No concepts in store — runStatus must error.
	if err := runStatus("."); err == nil {
		t.Error("expected error when no concepts present")
	}
}

func TestRunStatus_DetectsStaleAfterChange(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "src/com/acme/widgets/Widget.java",
		"package com.acme.widgets;\npublic class Widget {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\n## Key Files\n- src/com/acme/widgets/Widget.java\n"},
	}); err != nil {
		t.Fatalf("seed concepts: %v", err)
	}
	st.Close()

	// Baseline fingerprints via prepareSynthesisInputs.
	if _, _, _, err := prepareSynthesisInputs(".", "", false); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Mutate the Key File so the skill becomes stale.
	writeFileTree(t, root, "src/com/acme/widgets/Widget.java",
		"package com.acme.widgets;\npublic class Widget { int x; }\n")

	skills, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills: %v", err)
	}
	recorded, _ := codegen.LoadFingerprints(filepath.Join(root, ".tu-agent", "skill-fingerprints.json"))
	states, err := codegen.ComputeSkillStatus(root, skills, recorded)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Status != "stale" {
		t.Fatalf("states = %v, want [widgets stale]", states)
	}
}

func TestPrepareSynthesisInputs_SurfacesIndexError(t *testing.T) {
	root := t.TempDir()
	// One Java file so the scan phase succeeds.
	src := filepath.Join(root, "src", "main", "java", "com", "acme", "billing")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Invoice.java"),
		[]byte("package com.acme.billing;\npublic class Invoice {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// No concepts seeded in store → prepareSynthesisInputs errors on zero concepts.
	_, _, _, err := prepareSynthesisInputs(".", "", true)
	if err == nil {
		t.Fatal("expected error when concepts are missing")
	}
	if !strings.Contains(err.Error(), "no concepts found") {
		t.Errorf("error must say 'no concepts found', got: %v", err)
	}
}

func TestRunStatus_WarnsOnOrphanDirs(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")
	t.Chdir(root)
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}
	seedConcept(t) // so runStatus gets past the "no concepts" gate and reaches the orphan branch

	// An empty dir under .claude/skills/ is an orphan (no SKILL.md).
	if err := os.MkdirAll(filepath.Join(".claude", "skills", "video"), 0o755); err != nil {
		t.Fatal(err)
	}

	// End-to-end: runStatus must execute its orphan-detection branch without error.
	if err := runStatus("."); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	// And the helper it calls must actually flag the orphan.
	orphans, err := codegen.ListEmptySkillDirs(generatedSkillsDir("."))
	if err != nil {
		t.Fatalf("ListEmptySkillDirs: %v", err)
	}
	if len(orphans) != 1 || orphans[0] != "video" {
		t.Fatalf("orphans = %v, want [video]", orphans)
	}
}

// --- New store-sourced tests (Task 6) ---

func TestPrepareSynthesisInputs_FromStore(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}
	seedConcept(t)

	_, domains, _, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	if len(domains) != 1 || domains[0].Name != "widgets" {
		t.Errorf("domains from store = %+v, want [widgets]", domains)
	}
}

func TestRunStatus_FromStore(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, "core/src/main/java/Widget.java", "package core; class Widget {}")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}
	seedConcept(t)
	if err := runStatus("."); err != nil {
		t.Fatalf("runStatus from store: %v", err)
	}
}
