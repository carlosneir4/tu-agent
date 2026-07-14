package codegen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func TestFingerprintKeyFiles_ChangesWithContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.java"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := codegen.FingerprintKeyFiles(root, []string{"a.java"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.java"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := codegen.FingerprintKeyFiles(root, []string{"a.java"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("fingerprint should change when file content changes")
	}
}

func TestFingerprintKeyFiles_StableAndMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.java"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, _ := codegen.FingerprintKeyFiles(root, []string{"a.java"})
	h2, _ := codegen.FingerprintKeyFiles(root, []string{"a.java"})
	if h1 != h2 {
		t.Error("fingerprint must be stable for unchanged files")
	}
	if _, err := codegen.FingerprintKeyFiles(root, []string{"missing.java"}); err != nil {
		t.Errorf("missing files must not error, got %v", err)
	}
}

func TestComputeSkillStatus_States(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "w.java"), []byte("widget"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []codegen.Skill{
		{Name: "widgets", Body: "## Key Files\n- w.java\n"},
		{Name: "brandnew", Body: "## Key Files\n- w.java\n"},
		{Name: "architecture", Body: "## Key Files\n- w.java\n"},
	}
	cur, _ := codegen.FingerprintKeyFiles(root, []string{"w.java"})
	recorded := codegen.SkillFingerprints{"widgets": cur, "brandnew": "OLD-HASH"}
	states, err := codegen.ComputeSkillStatus(root, skills, recorded)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range states {
		got[s.Name] = s.Status
	}
	if _, ok := got["architecture"]; ok {
		t.Error("architecture must be skipped")
	}
	if got["widgets"] != "up-to-date" {
		t.Errorf("widgets = %q, want up-to-date", got["widgets"])
	}
	if got["brandnew"] != "stale" {
		t.Errorf("brandnew = %q, want stale", got["brandnew"])
	}
}

func TestLoadFingerprints_MissingFileEmpty(t *testing.T) {
	fp, err := codegen.LoadFingerprints(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(fp) != 0 {
		t.Errorf("expected empty map, got %v", fp)
	}
}
