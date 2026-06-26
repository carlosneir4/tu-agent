package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSkillPrune(t *testing.T) {
	root := t.TempDir()
	skills := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(skills, "video"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skills, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "web", "SKILL.md"),
		[]byte("---\nname: web\n---\n# Web"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSkillPrune(root); err != nil {
		t.Fatalf("runSkillPrune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skills, "video")); !os.IsNotExist(err) {
		t.Error("empty video dir should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(skills, "web", "SKILL.md")); err != nil {
		t.Error("web skill must remain")
	}
}
