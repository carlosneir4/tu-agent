package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteChunkAtomicReplace verifies that WriteChunk replaces the target
// file via rename rather than mutating the existing inode in place. A hard
// link to the original file must keep observing the OLD bytes after a
// second WriteChunk call with different content — proof that the directory
// entry was swapped, not the file's contents truncated and rewritten.
func TestWriteChunkAtomicReplace(t *testing.T) {
	dir := t.TempDir()

	recsA := []ChunkRecord{{SyncID: "a", TopicKey: "decision/a", Content: "first version"}}
	chunkPath, written, err := WriteChunk(dir, "alice", recsA)
	if err != nil {
		t.Fatalf("WriteChunk(A): %v", err)
	}
	if !written {
		t.Fatalf("WriteChunk(A): want written=true")
	}
	bytesA, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("read chunk after first write: %v", err)
	}

	linkPath := filepath.Join(dir, "hardlink.jsonl.gz")
	if err := os.Link(chunkPath, linkPath); err != nil {
		t.Fatalf("os.Link: %v", err)
	}

	recsB := []ChunkRecord{{SyncID: "a", TopicKey: "decision/a", Content: "second version, totally different"}}
	_, written, err = WriteChunk(dir, "alice", recsB)
	if err != nil {
		t.Fatalf("WriteChunk(B): %v", err)
	}
	if !written {
		t.Fatalf("WriteChunk(B): want written=true (content differs)")
	}

	linkBytes, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("read hard link after second write: %v", err)
	}
	if !bytesEqual(linkBytes, bytesA) {
		t.Fatalf("hard link bytes changed after second WriteChunk; want the OLD inode preserved (rename replaced the dir entry), got a mutated inode (in-place truncate)")
	}

	newBytes, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("read chunk after second write: %v", err)
	}
	if bytesEqual(newBytes, bytesA) {
		t.Fatalf("chunk path bytes did not change after second WriteChunk with different content")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file after successful write: %s", e.Name())
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
