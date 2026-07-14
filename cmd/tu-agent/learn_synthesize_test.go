package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/config"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
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

	project, domains, edges, _, err := prepareSynthesisInputs(".", "", false)
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
	// prepareSynthesisInputs is a read-only seam: it must NOT write
	// skill-fingerprints.json itself. Writing happens in the caller only
	// after synthesis succeeds (see TestRunSynthesize_Fingerprints*).
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); !os.IsNotExist(statErr) {
		t.Errorf("prepareSynthesisInputs must not write skill-fingerprints.json (stat err = %v)", statErr)
	}
}

// TestRunSynthesize_FingerprintsNotWrittenOnProviderFailure is the RED case for
// the fix: skill-fingerprints.json must not exist when synthesis fails, so a
// later 'tu-agent status' correctly flags skills as needing refresh rather than
// silently trusting fingerprints recorded before the architecture skill was
// (never) regenerated.
func TestRunSynthesize_FingerprintsNotWrittenOnProviderFailure(t *testing.T) {
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

	t.Setenv("TU_AGENT_NO_PROVIDER", "1")
	if err := runSynthesize(context.Background(), "", ""); err == nil {
		t.Fatal("expected error when provider calls are disabled")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); !os.IsNotExist(statErr) {
		t.Errorf("skill-fingerprints.json must not be written when synthesis fails (stat err = %v)", statErr)
	}
}

// TestRunSynthesize_FingerprintsWrittenAfterSuccess is the GREEN counterpart:
// once synthesis actually succeeds, the fingerprints must land on disk.
func TestRunSynthesize_FingerprintsWrittenAfterSuccess(t *testing.T) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "---\nname: architecture\ndescription: d\n---\n# Architecture Overview\nbody\n",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	origCfg := cfg
	cfg = config.Config{Providers: map[string]config.ProviderConfig{"local": {BaseURL: srv.URL}}}
	t.Cleanup(func() { cfg = origCfg })

	if err := runSynthesize(context.Background(), "", "local"); err != nil {
		t.Fatalf("runSynthesize: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); statErr != nil {
		t.Errorf("skill-fingerprints.json should be written after successful synthesis: %v", statErr)
	}
}

// TestRunSynthesize_FingerprintsNotWrittenOnEmptyOverview locks the F7-A review
// finding: when the model returns content that strips to empty (frontmatter
// only), persistArchitecture reports wrote=false and NOTHING is stored — so
// fingerprints must NOT be recorded, otherwise `tu-agent status` would falsely
// report the skills up-to-date against an overview that was never stored.
func TestRunSynthesize_FingerprintsNotWrittenOnEmptyOverview(t *testing.T) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					// Frontmatter only → normalizes to empty → wrote=false.
					"content": "---\nname: architecture\ndescription: d\n---\n",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	origCfg := cfg
	cfg = config.Config{Providers: map[string]config.ProviderConfig{"local": {BaseURL: srv.URL}}}
	t.Cleanup(func() { cfg = origCfg })

	if err := runSynthesize(context.Background(), "", "local"); err != nil {
		t.Fatalf("runSynthesize: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); !os.IsNotExist(statErr) {
		t.Errorf("skill-fingerprints.json must not be written when the overview is empty (stat err = %v)", statErr)
	}
	got, err := loadArchitecture()
	if err != nil {
		t.Fatalf("loadArchitecture: %v", err)
	}
	if got != "" {
		t.Errorf("nothing should be stored for an empty overview, loadArchitecture = %q", got)
	}
}

// TestMergedSynthesizeAndEnrich_StoresArchitecture confirms the merged
// synthesize+enrich path persists the architecture overview into the graph
// store (F7-A) — get_architecture / loadArchitecture returns it, stripped of
// frontmatter — and records fingerprints once the overview is stored.
func TestMergedSynthesizeAndEnrich_StoresArchitecture(t *testing.T) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "=== ARCHITECTURE ===\n# Architecture Overview\nbody\n=== PROJECT-CONTEXT ===\n## Coding Conventions\n- x\n",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	origCfg := cfg
	cfg = config.Config{Providers: map[string]config.ProviderConfig{"local": {BaseURL: srv.URL}}}
	t.Cleanup(func() { cfg = origCfg })

	prov, err := selectProvider(cfg, "synthesize", "local")
	if err != nil {
		t.Fatalf("selectProvider: %v", err)
	}

	mergedSynthesizeAndEnrich(context.Background(), root, prov, 0)

	got, err := loadArchitecture()
	if err != nil {
		t.Fatalf("loadArchitecture: %v", err)
	}
	if !strings.Contains(got, "# Architecture Overview") {
		t.Errorf("architecture overview not stored, loadArchitecture = %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); statErr != nil {
		t.Errorf("skill-fingerprints.json should be written after the overview is stored: %v", statErr)
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
	if _, _, _, _, err := prepareSynthesisInputs(".", "", false); err == nil {
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

	// Baseline fingerprints: prepareSynthesisInputs no longer writes them as a
	// side effect, so the test writes them itself (mirroring what runSynthesize
	// does after a successful synthesis).
	_, _, _, fps, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := fps.WriteJSON(filepath.Join(root, ".tu-agent", "skill-fingerprints.json")); err != nil {
		t.Fatalf("WriteJSON: %v", err)
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
	_, _, _, _, err := prepareSynthesisInputs(".", "", true)
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

	_, domains, _, _, err := prepareSynthesisInputs(".", "", false)
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

// --- F7-A: architecture overview stored in the graph store ---

func TestNormalizeArchitectureNarrative(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"frontmatter+marker", "---\nname: architecture\ndescription: d\n---\n<!-- tu-agent:generated -->\n# Architecture Overview\nbody", "# Architecture Overview\nbody"},
		{"frontmatter only", "---\nname: architecture\n---\n# Overview\n", "# Overview"},
		{"marker inline no frontmatter", "<!-- tu-agent:generated -->\n# Overview", "# Overview"},
		{"plain body", "# Overview\ntext", "# Overview\ntext"},
		{"whitespace only", "   \n\n", ""},
	}
	for _, c := range cases {
		if got := normalizeArchitectureNarrative(c.in); got != c.want {
			t.Errorf("%s: normalize(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestPersistAndLoadArchitecture_Roundtrip(t *testing.T) {
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

	// Frontmatter is stripped on the way in; get_architecture returns the body.
	wrote, err := persistArchitecture("---\nname: architecture\n---\n<!-- tu-agent:generated -->\n# Architecture Overview\ndomains\n")
	if err != nil {
		t.Fatalf("persistArchitecture: %v", err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true for non-empty content")
	}
	got, err := loadArchitecture()
	if err != nil {
		t.Fatalf("loadArchitecture: %v", err)
	}
	if got != "# Architecture Overview\ndomains" {
		t.Errorf("loadArchitecture = %q, want stripped body", got)
	}
}

func TestPersistArchitecture_EmptyIsNoOp(t *testing.T) {
	root := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	wrote, err := persistArchitecture("---\nname: architecture\n---\n")
	if err != nil {
		t.Fatalf("persistArchitecture: %v", err)
	}
	if wrote {
		t.Error("wrote = true, want false: frontmatter-only content is empty after normalization")
	}
	got, err := loadArchitecture()
	if err != nil {
		t.Fatalf("loadArchitecture: %v", err)
	}
	if got != "" {
		t.Errorf("loadArchitecture = %q, want empty (nothing persisted)", got)
	}
}
