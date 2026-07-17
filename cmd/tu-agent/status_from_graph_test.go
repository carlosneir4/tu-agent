package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// P2 (part 2 of 2) — status computes concept staleness FROM THE GRAPH.
//
// A concept's fingerprint is a hash over its member files' sha256 (the graph's
// `files` table already records one per parsed file), compared against the
// current on-disk content. skill-fingerprints.json and Record/LoadFingerprints
// are removed.
//
// Every concept body seeded here deliberately has NO "## Key Files" section —
// that is the real state of concepts since they moved into graph.db, and it is
// the whole bug: ParseKeyFiles returns nothing, so the legacy fingerprint is
// sha256("") for every concept. Omitting the section keeps the body from being
// a usable fingerprint source, so the only way to satisfy these tests is to
// read the concept -> member-file link out of the store.

// sha256OfEmptyString is what the legacy fingerprinter produced for 339 of 339
// concepts: the hash of nothing, which never changes.
const sha256OfEmptyString = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// conceptCard renders a minimal concept card body — frontmatter plus prose,
// with no "## Key Files" section (see the note above).
func conceptCard(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\nProse about " + name + ", carrying no file list.\n"
}

// writeConceptSkill materializes the generated SKILL.md for a concept, so the
// concept counts as "having a generated skill" rather than being reported "new".
func writeConceptSkill(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(generatedSkillsDir(root), name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(conceptCard(name, name)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// statusReport runs the real status entrypoint against root and returns its
// rendered report. Driving runStatusTo (rather than asserting on internals)
// keeps these tests honest about behavior and independent of where the data
// files live on disk.
func statusReport(t *testing.T, root string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runStatusTo(&buf, root); err != nil {
		t.Fatalf("runStatusTo: %v", err)
	}
	return buf.String()
}

// parseStatuses extracts concept -> status from a status report. Report lines
// are "  <name padded> <status>"; summary/warning lines never match because
// they do not consist of exactly a name plus a known status label.
func parseStatuses(report string) map[string]string {
	known := map[string]bool{"up-to-date": true, "stale": true, "new": true}
	out := map[string]string{}
	for _, line := range strings.Split(report, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && known[f[1]] {
			out[f[0]] = f[1]
		}
	}
	return out
}

// seedLearnedRepo builds a temp repo, parses it into the graph (which records a
// sha256 per file), links each concept to its member files in the store, and
// materializes each concept's generated skill. It returns the repo root, and
// leaves the process chdir'd there for the duration of the test.
//
// The graph is built BEFORE any mutation a test makes, so the sha256 the store
// holds is the "as learned" baseline the live on-disk content is compared to.
func seedLearnedRepo(t *testing.T, members map[string][]string) string {
	t.Helper()
	root := t.TempDir()
	for _, files := range members {
		for _, rel := range files {
			writeFileTree(t, root, rel, "package core;\npublic class "+
				strings.TrimSuffix(filepath.Base(rel), ".java")+" {}\n")
		}
	}
	t.Chdir(root)
	if err := runGraphBuild(""); err != nil {
		t.Fatalf("graph build: %v", err)
	}

	rows := make([]store.ConceptRow, 0, len(members))
	for name, files := range members {
		rows = append(rows, store.ConceptRow{
			Name: name, Description: name, Content: conceptCard(name, name), Files: files,
		})
	}
	st, err := openGraphStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.ReplaceConcepts(rows); err != nil {
		t.Fatalf("seed concepts: %v", err)
	}
	st.Close()

	for name := range members {
		writeConceptSkill(t, root, name)
	}
	return root
}

// @s1 — Changing a member file makes status report the concept as stale.
//
// The graph recorded Widget.java's sha256 at build time; rewriting the file
// afterwards means the live content no longer matches, so "widgets" is stale.
func TestRunStatus_StaleWhenMemberFileChanges(t *testing.T) {
	member := "core/src/main/java/Widget.java"
	root := seedLearnedRepo(t, map[string][]string{"widgets": {member}})

	if got := parseStatuses(statusReport(t, root))["widgets"]; got != "up-to-date" {
		t.Fatalf("precondition: widgets = %q before any edit, want %q", got, "up-to-date")
	}

	// Modify the member file's content on disk — the graph is NOT rebuilt, so
	// its recorded sha256 is now out of date w.r.t. what is on disk.
	writeFileTree(t, root, member, "package core;\npublic class Widget { int added; }\n")

	if got := parseStatuses(statusReport(t, root))["widgets"]; got != "stale" {
		t.Errorf("widgets = %q after its member file changed, want %q", got, "stale")
	}
}

// @s2 — An unchanged repo reports up-to-date, from a non-empty file set.
//
// The second assertion is the anti-regression heart of this feature and the
// thing that would have caught the sha256("") bug: for every concept, changing
// one of its member files must flip it to stale. A fingerprint computed over an
// empty file set passes the first assertion and FAILS this one, because nothing
// on disk can ever change the hash of nothing.
func TestRunStatus_UpToDateCoversAtLeastOneMemberFilePerConcept(t *testing.T) {
	members := map[string][]string{
		"widgets": {"core/src/main/java/Widget.java", "core/src/main/java/Knob.java"},
		"render":  {"core/src/main/java/Renderer.java"},
	}
	root := seedLearnedRepo(t, members)

	got := parseStatuses(statusReport(t, root))
	for name := range members {
		if got[name] != "up-to-date" {
			t.Errorf("unchanged repo: %s = %q, want %q", name, got[name], "up-to-date")
		}
	}

	// Prove the comparison actually covered >= 1 member file per concept: perturb
	// one member file at a time and require the owning concept to notice.
	for name, files := range members {
		probe := files[0]
		original, err := os.ReadFile(filepath.Join(root, probe))
		if err != nil {
			t.Fatal(err)
		}
		writeFileTree(t, root, probe, string(original)+"\n// perturbed\n")

		if s := parseStatuses(statusReport(t, root))[name]; s != "stale" {
			t.Errorf("%s = %q after member file %s changed, want %q — the fingerprint "+
				"does not cover any of this concept's member files", name, s, probe, "stale")
		}

		// Restore, and require the concept to go back to up-to-date, so the flip
		// above is attributable to this file's content and not to drift.
		if err := os.WriteFile(filepath.Join(root, probe), original, 0o644); err != nil {
			t.Fatal(err)
		}
		if s := parseStatuses(statusReport(t, root))[name]; s != "up-to-date" {
			t.Errorf("%s = %q after member file %s was restored, want %q", name, s, probe, "up-to-date")
		}
	}
}

// findFingerprintFiles returns every path under root named
// skill-fingerprints.json. Walking the whole repo rather than stat-ing one
// hardcoded path keeps this assertion valid across the .tu-agent re-layout.
func findFingerprintFiles(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "skill-fingerprints.json" {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return found
}

// @s3 — The fingerprints side file is no longer written.
func TestSynthesize_WritesNoFingerprintsFile(t *testing.T) {
	root := seedLearnedRepo(t, map[string][]string{"widgets": {"core/src/main/java/Widget.java"}})

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
	if found := findFingerprintFiles(t, root); len(found) != 0 {
		t.Errorf("synthesize wrote skill-fingerprints.json at %v; the side file is retired — "+
			"staleness comes from the graph", found)
	}
}

// @s4 — A leftover fingerprints file is irrelevant to status.
//
// This is the bug stated as a test: today the leftover file's sha256("") hashes
// match the sha256("") the legacy fingerprinter recomputes, so its mere presence
// flips concepts from "new" to "up-to-date". Once staleness comes from the
// graph, the file is inert data and the report must not move.
func TestRunStatus_IgnoresLeftoverFingerprintsFile(t *testing.T) {
	root := seedLearnedRepo(t, map[string][]string{
		"widgets": {"core/src/main/java/Widget.java"},
		"render":  {"core/src/main/java/Renderer.java"},
	})

	before := statusReport(t, root)

	// Drop a stale-format fingerprints file, hashes-of-nothing and all, at the
	// legacy location the retired writer used.
	leftover := filepath.Join(root, ".tu-agent", "skill-fingerprints.json")
	if err := os.MkdirAll(filepath.Dir(leftover), 0o755); err != nil {
		t.Fatal(err)
	}
	blob, err := json.MarshalIndent(map[string]string{
		"widgets": sha256OfEmptyString,
		"render":  sha256OfEmptyString,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, append(blob, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	if after := statusReport(t, root); after != before {
		t.Errorf("a leftover skill-fingerprints.json changed the status report.\nwithout it:\n%s\nwith it:\n%s",
			before, after)
	}
}
