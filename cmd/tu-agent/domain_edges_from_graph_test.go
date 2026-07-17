package main

import (
	"os"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// P2 (part 3) — domain edges and key files come from the concept -> files link
// in the graph store (store.ConceptRow.Files), NOT from a "## Key Files" section
// of the concept card body. Concept bodies have carried no such section since
// concepts moved into graph.db, so every reader that still parses the body
// silently degrades to empty. These tests drive the real entrypoints
// (prepareSynthesisInputs, registerKnowledge) and seed member files exclusively
// through the store link, so they are RED until the readers switch to sk.Files.

const (
	widgetRel = "src/com/acme/widgets/Widget.java"
	renderRel = "src/com/acme/render/Renderer.java"
)

// seedWidgetRenderSources writes a widgets->render import pair and builds the
// file-level graph, mirroring TestPrepareSynthesisInputs_GraphAndDomainEdges.
// It chdir's into root for the duration of the test.
func seedWidgetRenderSources(t *testing.T, root string) {
	t.Helper()
	writeFileTree(t, root, widgetRel,
		"package com.acme.widgets;\nimport com.acme.render.Renderer;\npublic class Widget {}\n")
	writeFileTree(t, root, renderRel,
		"package com.acme.render;\npublic class Renderer {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}
}

// replaceConcepts wholesale-replaces the concept table (and member-file links)
// at the current working directory's graph store.
func replaceConcepts(t *testing.T, rows []store.ConceptRow) {
	t.Helper()
	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.ReplaceConcepts(rows); err != nil {
		t.Fatalf("seed concepts: %v", err)
	}
	st.Close()
}

// @s1: concepts whose bodies carry no key-file list still produce domain edges,
// because the member files come from the store's concept -> files link.
func TestDomainEdgesFromGraph_S1_EdgesFromStoreLink(t *testing.T) {
	root := t.TempDir()
	seedWidgetRenderSources(t, root)

	// No "## Key Files" in any body; member files linked only via ConceptRow.Files.
	replaceConcepts(t, []store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\nwidget rendering\n", Files: []string{widgetRel}},
		{Name: "render", Description: "render", Content: "---\nname: render\ndescription: render\n---\nrenders things\n", Files: []string{renderRel}},
	})

	_, _, edges, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("domain edge set is empty; want a non-empty set derived from the store link")
	}
	var found bool
	for _, e := range edges {
		if e.From == "widgets" && e.To == "render" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing domain edge widgets->render; got %+v", edges)
	}
}

// @s2: the domain edge set tracks the store link, not a fixed answer. Re-linking
// Renderer.java under "widgets" (so "render" owns no file) makes the widgets->render
// edge disappear and attributes no edge to "render".
func TestDomainEdgesFromGraph_S2_TracksStoreLink(t *testing.T) {
	root := t.TempDir()
	seedWidgetRenderSources(t, root)

	// Re-link Renderer.java as a member file of "widgets"; "render" owns nothing.
	replaceConcepts(t, []store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\nwidget rendering\n", Files: []string{widgetRel, renderRel}},
		{Name: "render", Description: "render", Content: "---\nname: render\ndescription: render\n---\nrenders things\n", Files: nil},
	})

	_, _, edges, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	for _, e := range edges {
		if e.From == "widgets" && e.To == "render" {
			t.Errorf("edge widgets->render must be absent once render owns no file; got %+v", edges)
		}
		if e.From == "render" || e.To == "render" {
			t.Errorf("no edge may be attributed to render (it owns no member file); got %+v", edges)
		}
	}
}

// @s3: each domain fact carries exactly the member files the store recorded.
func TestDomainEdgesFromGraph_S3_KeyFilesFromStore(t *testing.T) {
	root := t.TempDir()
	fileA := "src/com/acme/widgets/Widget.java"
	fileB := "src/com/acme/widgets/Panel.java"
	writeFileTree(t, root, fileA, "package com.acme.widgets;\npublic class Widget {}\n")
	writeFileTree(t, root, fileB, "package com.acme.widgets;\npublic class Panel {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	// Two member files linked in the store; body has no "## Key Files".
	replaceConcepts(t, []store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\nwidget rendering\n", Files: []string{fileA, fileB}},
	})

	_, domains, _, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	var kf []string
	var seen bool
	for _, d := range domains {
		if d.Name == "widgets" {
			seen = true
			kf = d.KeyFiles
		}
	}
	if !seen {
		t.Fatalf("widgets domain fact missing; got %+v", domains)
	}
	if len(kf) == 0 {
		t.Fatal("widgets key files are empty; want the two member files from the store")
	}
	// Exactly the two member files linked in the store (order per store: sorted by path).
	want := map[string]bool{fileA: true, fileB: true}
	if len(kf) != len(want) {
		t.Fatalf("widgets key files = %v, want exactly %v", kf, []string{fileA, fileB})
	}
	for _, f := range kf {
		if !want[f] {
			t.Errorf("unexpected key file %q; want only %v", f, []string{fileA, fileB})
		}
	}
}

// @s4: a "## Key Files" section in a body is ignored outright; the store link wins.
func TestDomainEdgesFromGraph_S4_BodyKeyFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeFileTree(t, root, widgetRel, "package com.acme.widgets;\npublic class Widget {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	const decoy = "src/com/acme/decoy/Decoy.java"
	// Body carries a "## Key Files" section listing ONLY the decoy path, which is
	// not a member file of any concept; the store link points at Widget.java.
	replaceConcepts(t, []store.ConceptRow{
		{
			Name:        "widgets",
			Description: "widgets",
			Content:     "---\nname: widgets\ndescription: widgets\n---\n## Key Files\n- " + decoy + "\n",
			Files:       []string{widgetRel},
		},
	})

	_, domains, edges, err := prepareSynthesisInputs(".", "", false)
	if err != nil {
		t.Fatalf("prepareSynthesisInputs: %v", err)
	}
	var kf []string
	for _, d := range domains {
		if d.Name == "widgets" {
			kf = d.KeyFiles
		}
	}
	var hasWidget, hasDecoy bool
	for _, f := range kf {
		if f == widgetRel {
			hasWidget = true
		}
		if f == decoy {
			hasDecoy = true
		}
	}
	if !hasWidget {
		t.Errorf("widgets key files must contain %q (the store link); got %v", widgetRel, kf)
	}
	if hasDecoy {
		t.Errorf("widgets key files must NOT contain the body decoy %q; got %v", decoy, kf)
	}
	// No domain edge may be attributed via the decoy path.
	for _, e := range edges {
		if e.From == decoy || e.To == decoy {
			t.Errorf("no domain edge may be attributed via the decoy path %q; got %+v", decoy, edges)
		}
	}
}

// @s5: knowledge registration records which files a concept documents, sourcing
// member files from the store link (not the body).
func TestDomainEdgesFromGraph_S5_DocumentsEdgeFromStore(t *testing.T) {
	root := t.TempDir()
	const widgetFile = "core/Widget.java"
	writeFileTree(t, root, widgetFile, "package core;\npublic class Widget {}\n")

	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	// Concept linked to Widget.java via the store; body has no "## Key Files".
	replaceConcepts(t, []store.ConceptRow{
		{Name: "widgets", Description: "widgets", Content: "---\nname: widgets\ndescription: widgets\n---\nwidget rendering\n", Files: []string{widgetFile}},
	})

	if err := registerKnowledge("."); err != nil {
		t.Fatalf("registerKnowledge: %v", err)
	}

	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	all, err := st.AllEdges()
	st.Close()
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	var docCount int
	var hasWidgetDoc bool
	for _, e := range all {
		if e.Kind == graph.EdgeDocuments {
			docCount++
			if e.From == "skill::widgets" && e.To == widgetFile {
				hasWidgetDoc = true
			}
		}
	}
	if !hasWidgetDoc {
		t.Errorf("missing documents edge skill::widgets -> %s; got %+v", widgetFile, all)
	}
	if docCount == 0 {
		t.Error("count of documents edges is 0; want > 0")
	}
}
