package memory

import (
	"strings"
	"testing"
)

// TestRelChunkPathPerSubsystemLayout pins the NEW committed-chunks location
// under share/ (dir-relayout @s4). RelChunkPath is the repo-relative,
// forward-slash git pathspec twin of memoryChunksDir, so it must move to
// ".tu-agent/share/memory/chunks/". RED against the current flat
// ".tu-agent/memory/chunks/". Forward slashes are asserted literally because
// RelChunkPath is built with path.Join by design (never filepath.Join).
func TestRelChunkPathPerSubsystemLayout(t *testing.T) {
	const wantPrefix = ".tu-agent/share/memory/chunks/"
	for _, author := range []string{"alice", "Bob Smith", ""} {
		got := RelChunkPath(author)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("RelChunkPath(%q) = %q, want prefix %q", author, got, wantPrefix)
		}
	}
}
