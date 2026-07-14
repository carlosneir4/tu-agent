// internal/tool/jail_test.go
package tool_test

import (
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tool"
)

func TestConfinedPath_AllowsInsideRoot(t *testing.T) {
	got, err := tool.ConfinedPath("/project", "/project/src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/project/src/main.go" {
		t.Errorf("got %q, want /project/src/main.go", got)
	}
}

func TestConfinedPath_AllowsRoot(t *testing.T) {
	got, err := tool.ConfinedPath("/project", "/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/project" {
		t.Errorf("got %q, want /project", got)
	}
}

func TestConfinedPath_BlocksOutsideRoot(t *testing.T) {
	_, err := tool.ConfinedPath("/project", "/etc/passwd")
	if err == nil {
		t.Error("expected error for path outside root")
	}
}

func TestConfinedPath_BlocksTraversal(t *testing.T) {
	_, err := tool.ConfinedPath("/project", "/project/../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestConfinedPath_BlocksPartialPrefixMatch(t *testing.T) {
	_, err := tool.ConfinedPath("/project", "/projectevil/file")
	if err == nil {
		t.Error("expected error: /projectevil is not inside /project")
	}
}

func TestConfinedPath_EmptyRootPassthrough(t *testing.T) {
	got, err := tool.ConfinedPath("", "/any/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/any/path" {
		t.Errorf("got %q, want /any/path", got)
	}
}

func TestConfinedPath_RelativePathResolvedInsideRoot(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "sub", "file.go")
	got, err := tool.ConfinedPath(root, want)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
