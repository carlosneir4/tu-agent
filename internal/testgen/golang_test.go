package testgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles creates relative-path files (with parent dirs) under root.
func writeFiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoAdapterDetect(t *testing.T) {
	a := &GoAdapter{}
	root := t.TempDir()
	if err := a.Detect(root); err == nil {
		t.Fatal("Detect on empty dir: want error, got nil")
	}
	writeFiles(t, root, "go.mod")
	if err := a.Detect(root); err != nil {
		t.Fatalf("Detect with go.mod: %v", err)
	}
}

func TestGoAdapterTestPath(t *testing.T) {
	a := &GoAdapter{}
	tgt := Target{Name: "Save", Path: "internal/store/store.go", Language: "go"}
	root := t.TempDir()
	got, err := a.TestPath(root, tgt)
	if err != nil || got != "internal/store/store_test.go" {
		t.Fatalf("free path: got %q, %v", got, err)
	}
	writeFiles(t, root, "internal/store/store_test.go")
	got, err = a.TestPath(root, tgt) // still conventional even when it exists
	if err != nil || got != "internal/store/store_test.go" {
		t.Fatalf("existing: got %q, %v", got, err)
	}
}

func TestGoAdapterRunCommand(t *testing.T) {
	a := &GoAdapter{}
	tgt := Target{Name: "Store.Save", Path: "internal/store/store.go", Language: "go"}
	argv, err := a.RunCommand(t.TempDir(), "internal/store/store_test.go", tgt)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "test", "-run", "^TestStoreSave_Gen", "./internal/store"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("RunCommand = %v, want %v", argv, want)
	}
}

func TestGoAdapterPromptFragment(t *testing.T) {
	a := &GoAdapter{}
	tgt := Target{Name: "Store.Save", Path: "internal/store/store.go", Language: "go"}
	frag := a.PromptFragment(tgt, "internal/store/store_test.go")
	for _, want := range []string{"TestStoreSave_Gen", "internal/store/store_test.go", "testing"} {
		if !strings.Contains(frag, want) {
			t.Errorf("PromptFragment missing %q:\n%s", want, frag)
		}
	}
}

func TestGoPromptFragment_coverage(t *testing.T) {
	a := &GoAdapter{}
	frag := a.PromptFragment(Target{Name: "Foo.Bar", Path: "x/foo.go", Language: "go"}, "x/foo_test.go")
	for _, want := range []string{"branches", "fake"} {
		if !strings.Contains(frag, want) {
			t.Errorf("Go prompt missing coverage guidance %q", want)
		}
	}
}
