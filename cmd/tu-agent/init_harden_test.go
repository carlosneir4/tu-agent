package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir switches to dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestApplyHardening_CreatesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := applyHardening("go", "go", false, false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	path := filepath.Join(".claude", "settings.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings not written: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(first, &s); err != nil {
		t.Fatalf("settings.json invalid JSON: %v", err)
	}

	if err := applyHardening("go", "go", false, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("applyHardening not idempotent on settings.json")
	}

	gi, err := os.ReadFile(".gitignore")
	if err != nil || !strings.Contains(string(gi), ".tu-agent/graph.db") {
		t.Errorf(".gitignore not updated: %v", err)
	}
}

func TestApplyHardening_PrivateWritesGitInfoExclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := applyHardening("go", "go", true, false); err != nil {
		t.Fatalf("private run: %v", err)
	}

	ex, err := os.ReadFile(filepath.Join(".git", "info", "exclude"))
	if err != nil {
		t.Fatalf(".git/info/exclude not written: %v", err)
	}
	for _, want := range []string{".claude/", "CLAUDE.md", ".tu-agent/", "docs/superpowers/", "# >>> tu-agent (private) >>>"} {
		if !strings.Contains(string(ex), want) {
			t.Errorf(".git/info/exclude missing %q", want)
		}
	}

	// Private mode must NOT touch .gitignore (no Claude refs in committed files).
	if _, err := os.Stat(".gitignore"); err == nil {
		t.Error("private mode wrote .gitignore; it must only touch .git/info/exclude")
	}

	// Idempotent: a second run leaves the exclude unchanged (block not duplicated).
	if err := applyHardening("go", "go", true, false); err != nil {
		t.Fatalf("second private run: %v", err)
	}
	ex2, _ := os.ReadFile(filepath.Join(".git", "info", "exclude"))
	if string(ex2) != string(ex) {
		t.Error("private mode not idempotent on .git/info/exclude")
	}
	if strings.Count(string(ex2), "# >>> tu-agent (private) >>>") != 1 {
		t.Error("private block duplicated on second run")
	}
}

func TestApplyHardening_BacksUpAndPreserves(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	userJSON := `{"permissions":{"defaultMode":"acceptEdits"},"customKey":"keep"}`
	if err := os.WriteFile(filepath.Join(".claude", "settings.json"), []byte(userJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applyHardening("go", "go", false, false); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(".claude", "settings.json.bak")); err != nil {
		t.Error("expected settings.json.bak backup")
	}
	out, _ := os.ReadFile(filepath.Join(".claude", "settings.json"))
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if s["customKey"] != "keep" {
		t.Error("dropped user customKey")
	}
	if s["permissions"].(map[string]any)["defaultMode"] != "acceptEdits" {
		t.Error("overwrote user defaultMode")
	}

	// A second run must not clobber the original backup with hardened content.
	if err := applyHardening("go", "go", false, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	bak, err := os.ReadFile(filepath.Join(".claude", "settings.json.bak"))
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(bak) != userJSON {
		t.Errorf("backup not the original user content:\n got: %s\n want: %s", bak, userJSON)
	}
}
