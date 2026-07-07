package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/graph/store"
)

func writeSrc(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTestStore(t *testing.T, root string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(root, "graph.db"), ExtractorVersion)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// A scoped build must not delete store entries outside the scope, and must
// still delete in-scope entries whose file vanished.
func TestBuildScopedPreservesOutsideEntries(t *testing.T) {
	root := t.TempDir()
	writeSrc(t, root, "a/one.go", "package a\nfunc One() {}\n")
	writeSrc(t, root, "b/two.go", "package b\nfunc Two() {}\n")
	s := openTestStore(t, root)
	if _, err := Build(root, Extensions(), s); err != nil {
		t.Fatal(err)
	}
	// Remove an in-scope file, then scoped-build only "a".
	if err := os.Remove(filepath.Join(root, "a", "one.go")); err != nil {
		t.Fatal(err)
	}
	writeSrc(t, root, "a/three.go", "package a\nfunc Three() {}\n")
	res, err := BuildScoped(root, "a", Extensions(), s)
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["b/two.go"]; !ok {
		t.Errorf("scoped build deleted out-of-scope entry b/two.go")
	}
	if _, ok := files["a/one.go"]; ok {
		t.Errorf("scoped build kept deleted in-scope entry a/one.go")
	}
	if _, ok := files["a/three.go"]; !ok {
		t.Errorf("scoped build did not parse new in-scope file")
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", res.Deleted)
	}
}

func TestUnderScope(t *testing.T) {
	cases := []struct {
		rel, scope string
		want       bool
	}{
		{"a/one.go", "", true},
		{"a/one.go", "a", true},
		{"a/one.go", "a/one.go", true},
		{"ab/one.go", "a", false},
		{"b/two.go", "a", false},
	}
	for _, c := range cases {
		if got := underScope(c.rel, c.scope); got != c.want {
			t.Errorf("underScope(%q,%q) = %v, want %v", c.rel, c.scope, got, c.want)
		}
	}
}
