package extract

import (
	"path/filepath"
	"testing"
)

// TestBuildLockPathPerSubsystemLayout pins the NEW graph build lock location
// under graph/ (dir-relayout @s2). It is the repo-relative twin of the store
// path, so the lock must move alongside graph.db. RED against the current flat
// ".tu-agent/graph.build.lock".
func TestBuildLockPathPerSubsystemLayout(t *testing.T) {
	const root = "/repo"
	got := buildLockPath(root)
	want := filepath.Join(root, ".tu-agent", "graph", "graph.build.lock")
	if got != want {
		t.Errorf("buildLockPath(%q) = %q, want %q", root, got, want)
	}
}
