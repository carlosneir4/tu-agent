package main

// RED-phase tests for the crystallize marker/frontmatter and name-collision
// guard fix. Spec: .tu-agent/tdd/crystallize-marker-after-frontmatter-and/spec.md
// Feature: .tu-agent/tdd/crystallize-marker-after-frontmatter-and/features/crystallize-marker-and-name-guard.feature
//
// seedCluster (crystallize_gen_test.go) seeds a 3-note "checkout" cluster;
// memCrystallizeMin=3 lets that cluster clear the detection threshold.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/frontmatter"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// @s1: the materialized SKILL.md must open with the frontmatter delimiter,
// not the provenance marker, so Claude Code (which requires "---" on line 1)
// can parse it.
func TestSaveCrystallizedSkill_MarkerBelowFrontmatter(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	path, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nbody", 0)
	if err != nil {
		t.Fatalf("saveCrystallizedSkill: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	lines := strings.Split(string(got), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		first := ""
		if len(lines) > 0 {
			first = lines[0]
		}
		t.Errorf("first line of materialized SKILL.md = %q, want \"---\"\nfull content:\n%s", first, got)
	}
}

// @s2: a body whose frontmatter carries a name that diverges from the
// cluster label must be rewritten to the label, inside the frontmatter block.
func TestSaveCrystallizedSkill_RewritesDivergentName(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	path, err := saveCrystallizedSkill("checkout", "---\nname: something-else\n---\nbody", 0)
	if err != nil {
		t.Fatalf("saveCrystallizedSkill: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	fm, _, ok := frontmatter.Split(string(got))
	if !ok {
		t.Fatalf("materialized SKILL.md has no parseable leading frontmatter block:\n%s", got)
	}
	if !strings.Contains(fm, "name: checkout") {
		t.Errorf("frontmatter should contain %q, got frontmatter block:\n%s", "name: checkout", fm)
	}
	if strings.Contains(fm, "name: something-else") {
		t.Errorf("frontmatter should NOT still carry the divergent name, got frontmatter block:\n%s", fm)
	}
}

// @s3: a body whose frontmatter has no "name:" field at all must have one
// added, set to the cluster label.
func TestSaveCrystallizedSkill_AddsMissingName(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	path, err := saveCrystallizedSkill("checkout", "---\ndescription: handles checkout flow\n---\nbody", 0)
	if err != nil {
		t.Fatalf("saveCrystallizedSkill: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	fm, _, ok := frontmatter.Split(string(got))
	if !ok {
		t.Fatalf("materialized SKILL.md has no parseable leading frontmatter block:\n%s", got)
	}
	if !strings.Contains(fm, "name: checkout") {
		t.Errorf("frontmatter should contain %q (name added when missing), got frontmatter block:\n%s", "name: checkout", fm)
	}
}

// @s4: saving over a hand-written (non-crystallize-managed) SKILL.md at the
// target dir must be refused with an error naming the label, not silently
// preserved-but-recorded.
func TestSaveCrystallizedSkill_RefusesHandWrittenCollision(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	handWritten := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(handWritten), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(handWritten, []byte("# Checkout\n\nHand-written skill; do not overwrite.\n"), 0o644); err != nil {
		t.Fatalf("write hand-written skill: %v", err)
	}

	_, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nbody", 0)
	if err == nil {
		t.Fatal("expected saveCrystallizedSkill to refuse a hand-written-skill collision")
	}
	if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("collision error should name the label \"checkout\"; got: %v", err)
	}
}

// @s5: after a refused hand-written collision, no skill/checkout memory
// record must exist — the current top-of-function Upsert writes the record
// before the file collision is even inspected, so this test fails today even
// though @s4's error is separately asserted.
func TestSaveCrystallizedSkill_RefusedCollisionWritesNoRecord(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	handWritten := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(handWritten), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(handWritten, []byte("# Checkout\n\nHand-written skill; do not overwrite.\n"), 0o644); err != nil {
		t.Fatalf("write hand-written skill: %v", err)
	}

	if _, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nbody", 0); err == nil {
		t.Fatal("expected the collision save to be refused")
	}

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	obs, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if o.TopicKey == crystallize.SkillTopic("checkout") {
			t.Fatalf("refused collision must not upsert a skill/checkout record; found: %+v", o)
		}
	}
}

// @s6: re-crystallizing over a file that IS crystallize-managed (already
// carries the provenance marker) is not a collision — it must succeed and
// return the same materialized path, overwriting with the new content.
func TestSaveCrystallizedSkill_OverwritesManagedFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	if _, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nfirst generation", 0); err != nil {
		t.Fatalf("first (managed) save: %v", err)
	}

	path, err := saveCrystallizedSkill("checkout", "---\nname: checkout\n---\nsecond generation", 0)
	if err != nil {
		t.Fatalf("re-saving over a crystallize-managed file should succeed, got: %v", err)
	}
	want := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	if !strings.Contains(string(got), "second generation") {
		t.Errorf("expected the managed file to be overwritten with the second generation body, got:\n%s", got)
	}
}

// @s7: the MCP save path (handleCrystallizeSave) shares saveCrystallizedSkill,
// so it must inherit the same name→label rewrite.
func TestHandleCrystallizeSave_RewritesName(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	_, out, err := handleCrystallizeSave(context.Background(), nil,
		crystallizeSaveMCPInput{Label: "checkout", Body: "---\nname: wrong-name\n---\nbody"})
	if err != nil {
		t.Fatalf("handleCrystallizeSave: %v", err)
	}
	if !strings.Contains(out.Result, "checkout") {
		t.Errorf("expected the saved path in the result, got: %q", out.Result)
	}

	path := filepath.Join(generatedSkillsDir(repoRoot()), "checkout", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	fm, _, ok := frontmatter.Split(string(got))
	if !ok {
		t.Fatalf("materialized SKILL.md has no parseable leading frontmatter block:\n%s", got)
	}
	if !strings.Contains(fm, "name: checkout") {
		t.Errorf("frontmatter should contain %q, got frontmatter block:\n%s", "name: checkout", fm)
	}
}

// @s8: a body with no leading "---" frontmatter block at all falls back to
// prepending the provenance marker (the defensive path); the materialized
// file's first line must be the marker comment.
func TestSaveCrystallizedSkill_NoFrontmatterFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t)

	path, err := saveCrystallizedSkill("checkout", "plain text body with no frontmatter at all", 0)
	if err != nil {
		t.Fatalf("saveCrystallizedSkill: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	lines := strings.Split(string(got), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "<!-- "+crystallize.Marker) {
		first := ""
		if len(lines) > 0 {
			first = lines[0]
		}
		t.Errorf("first line = %q, want it to start with %q", first, "<!-- "+crystallize.Marker)
	}
}
