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

func TestWriteArchitectureSkill_NoExistingFileWritesWithMarker(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "SKILL.md")
	content := "---\nname: architecture\ndescription: d\n---\n# Architecture Overview\nbody\n"

	wrote, err := writeArchitectureSkill(outPath, content)
	if err != nil {
		t.Fatalf("writeArchitectureSkill: %v", err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true for a fresh file")
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), architectureGeneratedMarker) {
		t.Errorf("written file missing marker:\n%s", got)
	}
	if !strings.HasPrefix(string(got), "---\n") {
		t.Errorf("marker must not precede the frontmatter delimiter:\n%s", got)
	}
}

func TestWriteArchitectureSkill_HandWrittenFileSurvives(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "SKILL.md")
	handWritten := "---\nname: architecture\ndescription: hand-edited\n---\n# My own overview\n"
	if err := os.WriteFile(outPath, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := writeArchitectureSkill(outPath, "---\nname: architecture\ndescription: d\n---\n# Regenerated\n")
	if err != nil {
		t.Fatalf("writeArchitectureSkill: %v", err)
	}
	if wrote {
		t.Error("wrote = true, want false: a hand-edited file (no marker) must not be overwritten")
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != handWritten {
		t.Errorf("hand-written file was modified:\n%s", got)
	}
}

func TestWriteArchitectureSkill_MarkedFileIsRewritten(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "SKILL.md")
	marked := "---\nname: architecture\ndescription: old\n---\n" + architectureGeneratedMarker + "\n# Old overview\n"
	if err := os.WriteFile(outPath, []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}

	newContent := "---\nname: architecture\ndescription: new\n---\n# New overview\n"
	wrote, err := writeArchitectureSkill(outPath, newContent)
	if err != nil {
		t.Fatalf("writeArchitectureSkill: %v", err)
	}
	if !wrote {
		t.Error("wrote = false, want true: a previously generated file must be regenerated")
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "New overview") {
		t.Errorf("file was not rewritten with new content:\n%s", got)
	}
	if !strings.Contains(string(got), architectureGeneratedMarker) {
		t.Errorf("rewritten file lost its marker:\n%s", got)
	}
}

func TestInjectArchitectureMarker_PlacedAfterFrontmatter(t *testing.T) {
	content := "---\nname: architecture\ndescription: d\n---\n# Architecture Overview\nbody\n"
	got := injectArchitectureMarker(content)
	want := "---\nname: architecture\ndescription: d\n---\n" + architectureGeneratedMarker + "\n# Architecture Overview\nbody\n"
	if got != want {
		t.Errorf("injectArchitectureMarker =\n%s\nwant\n%s", got, want)
	}
}

func TestInjectArchitectureMarker_Idempotent(t *testing.T) {
	content := "---\nname: architecture\ndescription: d\n---\n" + architectureGeneratedMarker + "\n# Architecture Overview\n"
	if got := injectArchitectureMarker(content); got != content {
		t.Errorf("injectArchitectureMarker should be a no-op when the marker is already present:\n%s", got)
	}
}

// TestPluginTemplateMarkerPlacement_ParsesAsGraphKnowledge pins the interop
// between the plugin's architecture-synthesizer.md template (Step 2) and
// codegen.ParseSkillContent, the entry point registerKnowledge (learn_graph.go)
// uses to load a SKILL.md into the graph knowledge layer. The marker MUST sit
// on its own line right after the closing "---", exactly where the binary's
// own injectArchitectureMarker places it — the file must still start with
// "---" or splitFrontmatter (and thus ParseSkillContent) rejects it, and the
// skill silently drops out of the graph knowledge layer (a slog.Warn, not a
// hard failure, so this would otherwise go unnoticed).
func TestPluginTemplateMarkerPlacement_ParsesAsGraphKnowledge(t *testing.T) {
	// Marker AFTER the closing frontmatter delimiter — what the plugin
	// template now instructs, byte-matching the binary's placement.
	afterFrontmatter := "---\n" +
		"name: architecture\n" +
		"description: Project architecture overview.\n" +
		"---\n" +
		architectureGeneratedMarker + "\n" +
		"# Architecture Overview\nbody\n"

	if !strings.HasPrefix(afterFrontmatter, "---\n") {
		t.Fatal("test fixture must start with the frontmatter delimiter")
	}
	skill, err := codegen.ParseSkillContent(afterFrontmatter)
	if err != nil {
		t.Fatalf("ParseSkillContent must accept marker-after-frontmatter placement: %v", err)
	}
	if skill.Name != "architecture" {
		t.Errorf("skill.Name = %q, want %q", skill.Name, "architecture")
	}
	// The marker-guard's own detection mechanism (writeArchitectureSkill,
	// injectArchitectureMarker) is a plain strings.Contains check — confirm
	// it still fires with the marker in the body rather than the frontmatter.
	if !strings.Contains(afterFrontmatter, architectureGeneratedMarker) {
		t.Error("marker-guard Contains check must detect the marker after frontmatter")
	}

	// Marker BEFORE the leading "---" — what the plugin template instructed
	// before this fix. The file no longer starts with "---", so
	// splitFrontmatter (via ParseSkillContent) must reject it: this is the
	// exact failure mode the review finding flagged, documented here so a
	// future edit to the template cannot silently regress it.
	beforeFrontmatter := architectureGeneratedMarker + "\n" +
		"---\n" +
		"name: architecture\n" +
		"description: Project architecture overview.\n" +
		"---\n" +
		"# Architecture Overview\nbody\n"

	if _, err := codegen.ParseSkillContent(beforeFrontmatter); err == nil {
		t.Error("ParseSkillContent must reject marker-before-frontmatter placement (this is why placement matters)")
	}
}
