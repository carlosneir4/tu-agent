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
	// Member files come from the store's concept->files link (ConceptRow.Files);
	// no body carries a "## Key Files" section, which cannot exist in production
	// since concepts moved into graph.db.
	if err := st.ReplaceConcepts([]store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\nwidget rendering\n", Files: []string{"src/com/acme/widgets/Widget.java"}},
		{Name: "render", Description: "render", Content: "---\nname: render\ndescription: render\n---\nrenders things\n", Files: []string{"src/com/acme/render/Renderer.java"}},
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
}

// TestRunSynthesize_ErrorsWhenProviderUnavailable locks the error path: synthesis
// needs a model call, so with provider calls disabled runSynthesize must fail
// loudly rather than report success over a stale architecture overview.
func TestRunSynthesize_ErrorsWhenProviderUnavailable(t *testing.T) {
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
}

// TestRunSynthesize_EmptyOverviewStoresNothing locks the F7-A review finding:
// when the model returns content that strips to empty (frontmatter only),
// persistArchitecture reports wrote=false and NOTHING is stored, so the store
// keeps its old/absent overview rather than a blank one.
func TestRunSynthesize_EmptyOverviewStoresNothing(t *testing.T) {
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
// frontmatter.
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
