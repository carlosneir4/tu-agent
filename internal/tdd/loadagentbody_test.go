package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// TestLoadAgentBody_FallbackToEmbeddedShell (@s3): with no materialized
// .claude/agents/developer.md, LoadAgentBody falls back to the embedded shell.
func TestLoadAgentBody_FallbackToEmbeddedShell(t *testing.T) {
	root := t.TempDir()
	shell, ok := codegen.GenericShell("developer")
	if !ok {
		t.Fatalf("GenericShell(\"developer\") not available")
	}
	want := stripFrontmatter(shell)

	got, err := LoadAgentBody(root, "developer")
	if err != nil {
		t.Fatalf("loadAgentBody = error %v, want fallback with no error", err)
	}
	if got != want {
		t.Errorf("loadAgentBody fallback = %q, want stripFrontmatter(GenericShell(\"developer\")) = %q", got, want)
	}
}

// TestLoadAgentBody_MaterializedFileWins (@s4): a materialized repo-level file
// wins over the embedded shell.
func TestLoadAgentBody_MaterializedFileWins(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const sentinel = "SENTINEL-materialized-body-line"
	content := "---\nname: x-developer\ntools: Read\n---\n" + sentinel + "\n"
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadAgentBody(root, "developer")
	if err != nil {
		t.Fatalf("loadAgentBody = error %v, want materialized body", err)
	}
	if !strings.Contains(got, sentinel) {
		t.Errorf("loadAgentBody = %q, want materialized body containing %q", got, sentinel)
	}
}

// TestLoadAgentBody_NonENOENTErrorSurfaces (@s5): a non-ENOENT read error still
// surfaces — fallback triggers ONLY on os.IsNotExist. Making the path a
// directory forces os.ReadFile to fail with a non-ENOENT error portably.
func TestLoadAgentBody_NonENOENTErrorSurfaces(t *testing.T) {
	root := t.TempDir()
	agentPath := filepath.Join(root, ".claude", "agents", "developer.md")
	if err := os.MkdirAll(agentPath, 0o755); err != nil {
		t.Fatalf("mkdir agent path as dir: %v", err)
	}

	if _, err := LoadAgentBody(root, "developer"); err == nil {
		t.Errorf("loadAgentBody with unreadable (directory) path = nil error, want non-nil (fallback only on os.IsNotExist)")
	}
}
