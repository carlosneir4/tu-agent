package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshArtifacts_UpdatesToolsAndBlockPreservingBody(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Enriched developer agent with a STALE tools line (missing mem_*).
	agent := "---\nname: proj-developer\ndescription: \"d\"\ntools: Read, Write, Grep, Glob\n---\nYou are a senior developer.\n\n## Project Context\n- real enriched bullet about ServiceFoo\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "developer.md"), []byte(agent), 0o644); err != nil {
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

	got, _ := os.ReadFile(filepath.Join(agentsDir, "developer.md"))
	if !strings.Contains(string(got), "mcp__tu-agent-graph__mem_save") {
		t.Error("developer tools line was not refreshed with mem_save")
	}
	if !strings.Contains(string(got), "## Project Context\n- real enriched bullet about ServiceFoo") {
		t.Error("enriched body was not preserved")
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
