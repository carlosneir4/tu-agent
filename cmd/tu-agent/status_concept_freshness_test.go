package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// A3 — status derives concept freshness from the graph store, with no
// dependence on generated SKILL.md files. The old gate (conceptStatus,
// learn_synthesize.go) stat'ed .claude/skills/<concept>/SKILL.md first and
// returned "new" whenever that file was missing, regardless of the concept's
// linked files or hash state — which is every concept, since concepts moved
// into graph.db and nothing writes that file anymore. These scenarios run
// with NO SKILL.md anywhere on disk: that absence is the load-bearing
// condition that makes @s1/@s2/@s4 red against the current gate.

// seedRepoNoSkill builds a temp repo, parses it into the graph (recording a
// sha256 per file), and links each concept to its member files in the store —
// mirrors seedLearnedRepo (status_from_graph_test.go) but deliberately never
// materializes a generated SKILL.md for any concept.
func seedRepoNoSkill(t *testing.T, members map[string][]string) string {
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
	return root
}

// @s1 — Fresh learn reports every concept up-to-date without any SKILL.md.
func TestRunStatus_FreshLearnAllUpToDateNoSkillMD(t *testing.T) {
	members := map[string][]string{
		"widgets": {"core/src/main/java/Widget.java"},
		"render":  {"core/src/main/java/Renderer.java"},
	}
	root := seedRepoNoSkill(t, members)

	got := parseStatuses(statusReport(t, root))
	for name := range members {
		if got[name] != "up-to-date" {
			t.Errorf("%s = %q with no SKILL.md on disk, want %q", name, got[name], "up-to-date")
		}
	}
}

// @s2 — Editing a member file flips only its concept to stale.
func TestRunStatus_EditFlipsOnlyThatConceptStaleNoSkillMD(t *testing.T) {
	billingMember := "core/src/main/java/Billing.java"
	members := map[string][]string{
		"billing": {billingMember},
		"auth":    {"core/src/main/java/Auth.java"},
	}
	root := seedRepoNoSkill(t, members)

	writeFileTree(t, root, billingMember, "package core;\npublic class Billing { int added; }\n")

	got := parseStatuses(statusReport(t, root))
	if got["billing"] != "stale" {
		t.Errorf("billing = %q after its member file changed, want %q", got["billing"], "stale")
	}
	if got["auth"] != "up-to-date" {
		t.Errorf("auth = %q, want %q", got["auth"], "up-to-date")
	}
}

// @s3 — A concept with zero linked files reports new.
func TestRunStatus_OrphanConceptZeroFilesReportsNew(t *testing.T) {
	root := seedRepoNoSkill(t, map[string][]string{
		"core":   {"core/src/main/java/Core.java"},
		"orphan": nil,
	})

	got := parseStatuses(statusReport(t, root))["orphan"]
	if got != "new" {
		t.Errorf("orphan = %q, want %q", got, "new")
	}
}

// @s4 — Refresh advice counts concepts (not "skills") and names the learn
// command as the remediation.
func TestRunStatus_AdviceNamesLearnCommandNoSkillMD(t *testing.T) {
	member := "core/src/main/java/Invoice.java"
	root := seedRepoNoSkill(t, map[string][]string{"billing": {member}})

	writeFileTree(t, root, member, "package core;\npublic class Invoice { int added; }\n")

	report := statusReport(t, root)
	if !strings.Contains(report, "1 concept(s) need refresh") {
		t.Errorf("advice summary missing %q; got:\n%s", "1 concept(s) need refresh", report)
	}
	if strings.Contains(report, "skill(s) need refresh") {
		t.Errorf("advice summary still says %q; got:\n%s", "skill(s) need refresh", report)
	}
	if !strings.Contains(report, "tu-agent learn") {
		t.Errorf("advice line missing remediation command %q; got:\n%s", "tu-agent learn", report)
	}
}
