package main

import (
	"context"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestHandleMemConflicts_ListsEdges(t *testing.T) {
	t.Chdir(t.TempDir())
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ms.Upsert("testing/pattern-a", "A", memory.UpsertOpts{Type: "testing"})
	b, _ := ms.Upsert("testing/pattern-c", "C", memory.UpsertOpts{Type: "testing"})
	if _, err := ms.Relate(a.ID, b.ID, "conflicts_with"); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	_, out, err := handleMemConflicts(context.Background(), nil, memConflictsMCPInput{})
	if err != nil {
		t.Fatalf("handleMemConflicts: %v", err)
	}
	if !strings.Contains(out.Result, "testing/pattern-a") || !strings.Contains(out.Result, "testing/pattern-c") {
		t.Errorf("expected both topic keys:\n%s", out.Result)
	}
}

func TestMemConflictsInMCPToolNames(t *testing.T) {
	t.Chdir(t.TempDir())
	if !servedToolNames(t)["mem_conflicts"] {
		t.Error("newMCPServer does not serve mem_conflicts")
	}
}
