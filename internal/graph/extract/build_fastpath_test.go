package extract

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Second build with untouched files re-parses nothing; a content change
// re-parses exactly that file; an mtime-only touch stays Unchanged and
// refreshes the stored stat so the next run takes the fast path again.
func TestBuildFastPath(t *testing.T) {
	root := t.TempDir()
	writeSrc(t, root, "a/one.go", "package a\nfunc One() {}\n")
	writeSrc(t, root, "b/two.go", "package b\nfunc Two() {}\n")
	s := openTestStore(t, root)
	if _, err := Build(root, Extensions(), s); err != nil {
		t.Fatal(err)
	}

	res, err := Build(root, Extensions(), s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Parsed != 0 || res.Unchanged != 2 {
		t.Errorf("no-op build: Parsed=%d Unchanged=%d, want 0/2", res.Parsed, res.Unchanged)
	}

	// mtime-only touch: content identical -> Unchanged, stat refreshed.
	one := filepath.Join(root, "a", "one.go")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(one, future, future); err != nil {
		t.Fatal(err)
	}
	if res, err = Build(root, Extensions(), s); err != nil {
		t.Fatal(err)
	}
	if res.Parsed != 0 {
		t.Errorf("mtime touch re-parsed: Parsed=%d", res.Parsed)
	}
	files, err := s.Files()
	if err != nil {
		t.Fatal(err)
	}
	if files["a/one.go"].MtimeNS != future.UnixNano() {
		t.Errorf("stored mtime not refreshed after touch")
	}

	// Content change -> exactly one re-parse.
	writeSrc(t, root, "a/one.go", "package a\nfunc One() {}\nfunc Extra() {}\n")
	if res, err = Build(root, Extensions(), s); err != nil {
		t.Fatal(err)
	}
	if res.Parsed != 1 {
		t.Errorf("content change: Parsed=%d, want 1", res.Parsed)
	}
}
