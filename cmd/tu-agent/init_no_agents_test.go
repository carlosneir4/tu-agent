package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RED-phase tests for feature `prepare-materializes-no-agents`.
//
// Surface under test: runInitSetup (cmd/tu-agent/init.go). Today the non-update
// path calls generateAgents, which materializes .claude/agents/<role>.md for the
// dev-flow roles, and the --update path (refreshArtifacts) rewrites those agents'
// `tools:` frontmatter line. GREEN removes both: prepare writes NO agents and
// --update leaves any existing agent byte-identical while still refreshing the
// CLAUDE.md knowledge block (D1=A coexistence).
//
// Helpers reused from the package (do not redefine here):
//   writeGoRepo(t, root)        — init_autolearn_test.go
//   seedConcept(t)              — learn_synthesize_test.go (skips auto-learn)
//   readYAML(t, path)           — overlay_lang_test.go
// seedConcept pre-populates the concept store so runInitSetup's auto-learn branch
// is skipped, keeping these tests hermetic and fast and isolating the assertions
// to the agent-materialization contract.
// ---------------------------------------------------------------------------

// devFlowAgentStems are the role files prepare used to materialize; GREEN writes
// none of them.
var devFlowAgentStems = []string{"developer", "qa", "architect", "pr-reviewer", "security-reviewer"}

// @s1 — a fresh prepare (no --update, no --augment-agents) writes CLAUDE.md and a
// hardened settings.json but NO dev-flow agent files.
func TestPrepareNoAgents_FreshWritesNoAgentFiles(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)
	seedConcept(t) // non-empty store => auto-learn is skipped

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go"}); err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Errorf("expected CLAUDE.md at repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err != nil {
		t.Errorf("expected .claude/settings.json (hardening must still run): %v", err)
	}
	for _, stem := range devFlowAgentStems {
		path := filepath.Join(root, ".claude", "agents", stem+".md")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("prepare must not materialize %s.md, stat err=%v", stem, err)
		}
	}
}

// @s2 — fresh prepare still seeds the deterministic tdd config (unchanged): a
// non-empty tdd.test_command and a non-empty tdd.language land in
// .tu-agent/config.yaml. This should pass today; it guards GREEN from regressing
// the deterministic setup.
func TestPrepareNoAgents_StillSeedsTddConfig(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)
	seedConcept(t)

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go"}); err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	m := readYAML(t, filepath.Join(root, ".tu-agent", "config.yaml"))
	tdd, ok := m["tdd"].(map[string]any)
	if !ok {
		t.Fatalf("config.yaml has no tdd section: %v", m)
	}
	if cmd, _ := tdd["test_command"].(string); strings.TrimSpace(cmd) == "" {
		t.Errorf("tdd.test_command must be non-empty, got %q", cmd)
	}
	if lang, _ := tdd["language"].(string); strings.TrimSpace(lang) == "" {
		t.Errorf("tdd.language must be non-empty, got %q", lang)
	}
}

// @s3 — --force (no --update) must not materialize agents either; CLAUDE.md is
// still (re)written.
func TestPrepareNoAgents_ForceWritesNoAgentFiles(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)
	seedConcept(t)

	// Precondition: no .claude/agents directory.
	if _, err := os.Stat(filepath.Join(root, ".claude", "agents")); !os.IsNotExist(err) {
		t.Fatalf("precondition: .claude/agents must not exist, stat err=%v", err)
	}

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go", Force: true}); err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "agents", "developer.md")); !os.IsNotExist(err) {
		t.Errorf("--force must not materialize developer.md, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Errorf("expected CLAUDE.md at repo root after --force: %v", err)
	}
}

// @s4 — prepare --update leaves an existing, hand-edited agent byte-identical
// (no tools-line rewrite) while still refreshing the CLAUDE.md knowledge block.
func TestPrepareNoAgents_UpdateLeavesAgentByteIdentical(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)
	seedConcept(t)

	// A CLAUDE.md with a tu-agent knowledge block plus out-of-block user content.
	claude := "# My project rules\nkeep me\n\n<!-- tu-agent:knowledge -->\nold block body\n<!-- /tu-agent:knowledge -->\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hand-edited developer agent whose tools line is STALE (missing mem_*).
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	developerPath := filepath.Join(agentsDir, "developer.md")
	agent := "---\nname: proj-developer\ndescription: \"d\"\ntools: Read, Write, Grep, Glob\n---\nYou are a senior developer.\n\n## Project Context\n- real enriched bullet about ServiceFoo\n"
	if err := os.WriteFile(developerPath, []byte(agent), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(developerPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go", Update: true}); err != nil {
		t.Fatalf("runInitSetup --update: %v", err)
	}

	after, err := os.ReadFile(developerPath)
	if err != nil {
		t.Fatalf("developer.md missing after --update: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("--update must leave developer.md byte-identical (no tools rewrite).\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// The knowledge block must still have been refreshed in place.
	gotClaude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotClaude), "# My project rules\nkeep me") {
		t.Error("out-of-block CLAUDE.md content was not preserved")
	}
	if strings.Contains(string(gotClaude), "old block body") {
		t.Error("knowledge block was not refreshed in place (stale body survived)")
	}
}

// @s5 — --update on a repo with no agents touches no agents and does not fail: it
// returns nil and never creates the .claude/agents directory.
func TestPrepareNoAgents_UpdateNoAgentsNoCreate(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)
	seedConcept(t)

	claude := "# rules\n\n<!-- tu-agent:knowledge -->\nold block body\n<!-- /tu-agent:knowledge -->\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInitSetup(context.Background(), initSetupOpts{Lang: "go", Update: true}); err != nil {
		t.Fatalf("runInitSetup --update must not fail when no agents exist: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "agents")); !os.IsNotExist(err) {
		t.Errorf("--update must not create .claude/agents, stat err=%v", err)
	}
}
