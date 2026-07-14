package codegen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListEmptySkillDirs(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "web"))
	writeFile(t, filepath.Join(root, "web", "SKILL.md"), "---\nname: web\n---\n# Web")
	mkdir(t, filepath.Join(root, "video")) // orphan, empty
	mkdir(t, filepath.Join(root, "stray"))
	writeFile(t, filepath.Join(root, "stray", "notes.txt"), "x") // no SKILL.md but not empty

	got, err := codegen.ListEmptySkillDirs(root)
	if err != nil {
		t.Fatalf("ListEmptySkillDirs: %v", err)
	}
	want := []string{"stray", "video"} // sorted; both lack SKILL.md
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ListEmptySkillDirs = %v, want %v", got, want)
	}
}

func TestPruneEmptySkillDirs(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "web"))
	writeFile(t, filepath.Join(root, "web", "SKILL.md"), "---\nname: web\n---\n# Web")
	mkdir(t, filepath.Join(root, "video")) // orphan, empty -> removed
	mkdir(t, filepath.Join(root, "stray")) // no SKILL.md but has a file -> preserved
	writeFile(t, filepath.Join(root, "stray", "notes.txt"), "x")

	removed, err := codegen.PruneEmptySkillDirs(root)
	if err != nil {
		t.Fatalf("PruneEmptySkillDirs: %v", err)
	}
	if len(removed) != 1 || removed[0] != "video" {
		t.Fatalf("removed = %v, want [video]", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "video")); !os.IsNotExist(err) {
		t.Error("video dir should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "web", "SKILL.md")); err != nil {
		t.Error("web skill must remain")
	}
	if _, err := os.Stat(filepath.Join(root, "stray", "notes.txt")); err != nil {
		t.Error("non-empty dir without SKILL.md must be preserved")
	}
}

func TestPruneEmptySkillDirs_MissingDir(t *testing.T) {
	removed, err := codegen.PruneEmptySkillDirs(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("want nil err for missing dir, got %v", err)
	}
	if removed != nil {
		t.Fatalf("want nil removed, got %v", removed)
	}
}
