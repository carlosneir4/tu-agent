package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Building from a subdirectory must anchor extraction at the repo root: the
// store keeps root-relative paths and nothing is mass-deleted.
func TestRunGraphBuildFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.go"), []byte("package main\nfunc Top() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := runGraphBuild(""); err != nil {
		t.Fatal(err)
	}
	// Now rebuild from the subdir: top.go must survive with its root-relative path.
	if err := os.Chdir(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := runGraphBuild(""); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	s, err := openGraphStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	files, err := s.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["top.go"]; !ok {
		t.Errorf("top.go lost after subdir rebuild; files = %v", files)
	}
}
