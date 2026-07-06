package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/subagent"
	"github.com/tu/tu-agent/internal/tdd"
)

func TestTddCheckMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := validateTddAgents(root)
	if len(missing) != 5 {
		t.Fatalf("empty repo should miss all 5 roles, got %v", missing)
	}
}

func TestTddOverlayCmd(t *testing.T) {
	var buf bytes.Buffer
	tddOverlayCmd.SetOut(&buf)
	if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{"architect"}); err != nil {
		t.Fatalf("overlay architect: %v", err)
	}
	if !strings.Contains(buf.String(), "@s1") {
		t.Fatalf("architect overlay should contain @s1, got %q", buf.String())
	}
	if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{"nope"}); err == nil {
		t.Fatal("unknown stage should error")
	}
}

func TestTddCheckPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"analyst", "architect", "developer", "pr-reviewer", "scribe"} {
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte("---\nname: x\n---\nb\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if m := validateTddAgents(root); len(m) != 0 {
		t.Fatalf("all roles present but reported missing: %v", m)
	}
}

func TestTddOverlayAllStages(t *testing.T) {
	for _, stage := range []string{"analyst", "architect", "craftsman", "judge", "scribe"} {
		var buf bytes.Buffer
		tddOverlayCmd.SetOut(&buf)
		if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{stage}); err != nil {
			t.Fatalf("overlay %s: %v", stage, err)
		}
		if strings.TrimSpace(buf.String()) == "" {
			t.Fatalf("overlay %s produced empty output", stage)
		}
	}
}

func TestTddOverlaySandwichStages(t *testing.T) {
	tw, ok := tddOverlay("test-writer")
	if !ok || !strings.Contains(tw, "NO production") {
		t.Fatalf("test-writer overlay must contain %q, got ok=%v %q", "NO production", ok, tw)
	}
	impl, ok := tddOverlay("implementer")
	if !ok || !strings.Contains(impl, "do NOT modify") {
		t.Fatalf("implementer overlay must contain %q, got ok=%v %q", "do NOT modify", ok, impl)
	}
}

func TestComposeStagePromptSandwich(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both sandwich stages map to the developer.md role, same as craftsman.
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tw, err := composeStagePrompt(root, "test-writer", tddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt(test-writer): %v", err)
	}
	if !strings.Contains(tw, "DEV-BODY") || !strings.Contains(tw, "NO production") {
		t.Fatalf("test-writer prompt must join body + RED overlay, got: %q", tw)
	}
	impl, err := composeStagePrompt(root, "implementer", tddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt(implementer): %v", err)
	}
	if !strings.Contains(impl, "DEV-BODY") || !strings.Contains(impl, "do NOT modify") {
		t.Fatalf("implementer prompt must join body + GREEN overlay, got: %q", impl)
	}
}

func TestTddStageDefsSubstitutesBaseDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// tddStageDefs loads every non-analyst role; give them all a body.
	for _, role := range []string{"architect", "developer", "pr-reviewer", "scribe"} {
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defs, err := tddStageDefs(root, ".tu-agent/tdd/ABC-1-x")
	if err != nil {
		t.Fatalf("tddStageDefs: %v", err)
	}
	var arch *subagent.Definition
	for _, d := range defs {
		if strings.Contains(d.SystemPrompt, tdd.TddDirToken) {
			t.Errorf("stage %s still contains unsubstituted token", d.Name)
		}
		if d.Name == "architect" {
			arch = d
		}
	}
	if arch == nil || !strings.Contains(arch.SystemPrompt, ".tu-agent/tdd/ABC-1-x/spec.md") {
		t.Errorf("architect def missing substituted base dir")
	}
}

func TestComposeStagePrompt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// craftsman maps to developer.md
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := composeStagePrompt(root, "craftsman", tddRelBase("", "x"))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(out, "DEV-BODY") || !strings.Contains(out, "tu-agent TDD task") {
		t.Fatalf("compose must join body + overlay, got: %q", out)
	}
	if _, err := composeStagePrompt(root, "bogus", tddRelBase("", "x")); err == nil {
		t.Fatal("unknown stage must error")
	}
	if _, err := composeStagePrompt(root, "architect", tddRelBase("", "x")); err == nil {
		t.Fatal("missing agent file must error")
	}
}

func TestPromptRelBase(t *testing.T) {
	// explicit base wins, ignores ticket/desc
	if got := promptRelBase(".tu-agent/tdd/EXPLICIT", "ABC-1", []string{"user", "login"}); got != ".tu-agent/tdd/EXPLICIT" {
		t.Errorf("explicit base = %q", got)
	}
	// no base -> derive from ticket + desc
	if got := promptRelBase("", "ABC-1", []string{"user", "login"}); got != tddRelBase("ABC-1", slugify("user login")) {
		t.Errorf("derived base = %q", got)
	}
}

func TestComposeStagePromptSubstitutesBaseDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "architect.md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := composeStagePrompt(root, "architect", ".tu-agent/tdd/ABC-1-x")
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}
	if strings.Contains(out, tdd.TddDirToken) {
		t.Errorf("token not substituted")
	}
	if !strings.Contains(out, ".tu-agent/tdd/ABC-1-x/spec.md") {
		t.Errorf("base dir not applied")
	}
}
