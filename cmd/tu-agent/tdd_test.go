package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/subagent"
)

func TestStripFrontmatter(t *testing.T) {
	in := "---\nname: x\ntools: Read\n---\nBODY line 1\nBODY line 2\n"
	if got := stripFrontmatter(in); got != "BODY line 1\nBODY line 2\n" {
		t.Fatalf("stripFrontmatter = %q", got)
	}
	if got := stripFrontmatter("no frontmatter\n"); got != "no frontmatter\n" {
		t.Fatalf("passthrough = %q", got)
	}
}

func TestValidateTddAgents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only architect present: the other four are missing.
	if err := os.WriteFile(filepath.Join(dir, "architect.md"), []byte("---\nname: a\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := validateTddAgents(root)
	for _, want := range []string{"analyst", "developer", "pr-reviewer", "scribe"} {
		if !slices.Contains(missing, want) {
			t.Errorf("expected %q in missing, got %v", want, missing)
		}
	}
	if slices.Contains(missing, "architect") {
		t.Errorf("architect present but reported missing: %v", missing)
	}
}

func TestTddStageDefsComposesBodyAndOverlay(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"architect", "developer", "pr-reviewer", "scribe"} {
		body := "---\nname: " + role + "\n---\nKNOWLEDGE-" + role + "\n"
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defs, err := tddStageDefs(root)
	if err != nil {
		t.Fatalf("tddStageDefs: %v", err)
	}
	byName := map[string]*subagent.Definition{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	if byName["analyst"] != nil {
		t.Error("analyst runs in the foreground, not via the dispatcher")
	}
	arch := byName["architect"]
	if arch == nil || !strings.Contains(arch.SystemPrompt, "KNOWLEDGE-architect") ||
		!strings.Contains(arch.SystemPrompt, "tu-agent TDD task") {
		t.Fatalf("architect def must compose body + overlay: %+v", arch)
	}
	craft := byName["craftsman"]
	if craft == nil || !strings.Contains(craft.SystemPrompt, "KNOWLEDGE-developer") {
		t.Fatalf("craftsman def must load the developer body: %+v", craft)
	}
	if !slices.Contains(craft.ToolSubset, "write_file") {
		t.Errorf("craftsman must grant write_file")
	}
}

func TestTddCmdRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "tdd" {
			found = true
		}
	}
	if !found {
		t.Fatal("tdd command not registered on rootCmd")
	}
}

func TestResolveTestRunner(t *testing.T) {
	tests := []struct {
		name        string
		testCommand string
		writeGoMod  bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "explicit config command",
			testCommand: "echo ok",
			writeGoMod:  false,
			wantErr:     false,
		},
		{
			name:        "go autodetect when go.mod present",
			testCommand: "",
			writeGoMod:  true,
			wantErr:     false,
		},
		{
			name:        "fail fast when no command and no go.mod",
			testCommand: "",
			writeGoMod:  false,
			wantErr:     true,
			errContains: "tdd.test_command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.writeGoMod {
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
					t.Fatalf("write go.mod: %v", err)
				}
			}
			cfg := config.Config{Tdd: config.TddConfig{TestCommand: tt.testCommand}}

			runner, err := resolveTestRunner(cfg, root)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runner == nil {
				t.Fatalf("expected non-nil runner")
			}
		})
	}
}
