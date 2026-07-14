package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F7-C: prepare no longer owns .claude/agents, so --update (refreshArtifacts)
// refreshes ONLY the CLAUDE.md knowledge block and leaves any existing agent
// byte-identical — the agent-tools rewrite loop is gone.
func TestRefreshArtifacts_RefreshesBlockLeavesAgentUntouched(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A developer agent with a STALE tools line (missing mem_*): --update must
	// NOT touch it anymore.
	agent := "---\nname: proj-developer\ndescription: \"d\"\ntools: Read, Write, Grep, Glob\n---\nYou are a senior developer.\n\n## Project Context\n- real enriched bullet about ServiceFoo\n"
	agentPath := filepath.Join(agentsDir, "developer.md")
	if err := os.WriteFile(agentPath, []byte(agent), 0o644); err != nil {
		t.Fatal(err)
	}
	// CLAUDE.md with a knowledge block and out-of-block user content.
	claude := "# My project rules\nkeep me\n\n<!-- tu-agent:knowledge -->\nold block body\n<!-- /tu-agent:knowledge -->\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := refreshArtifacts(root); err != nil {
		t.Fatalf("refreshArtifacts: %v", err)
	}

	got, _ := os.ReadFile(agentPath)
	if string(got) != agent {
		t.Errorf("developer.md must be byte-identical after --update, got:\n%s", got)
	}
	gotClaude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(gotClaude), "# My project rules\nkeep me") {
		t.Error("out-of-block CLAUDE.md content was not preserved")
	}
	if strings.Contains(string(gotClaude), "old block body") {
		t.Error("knowledge block was not refreshed")
	}
}

func TestRefreshArtifacts_NoClaudeMd_NoCreate(t *testing.T) {
	root := t.TempDir()
	if err := refreshArtifacts(root); err != nil {
		t.Fatalf("refreshArtifacts on empty root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("refreshArtifacts must not create CLAUDE.md when absent")
	}
}

func TestRunInitSetup_UpdateAndForceMutuallyExclusive(t *testing.T) {
	err := runInitSetup(context.TODO(), initSetupOpts{Update: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want mutual-exclusion error, got %v", err)
	}
}
