package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

func TestFormatConflicts(t *testing.T) {
	rels := []memory.Relation{{FromID: "id-a", ToID: "id-b"}}
	byID := map[string]string{"id-a": "testing/pattern-a", "id-b": "testing/pattern-c"}
	out := formatConflicts(rels, byID)
	for _, want := range []string{"testing/pattern-a", "testing/pattern-c", "1 conflict"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatConflicts missing %q:\n%s", want, out)
		}
	}
	// Unresolved endpoint falls back to the raw id; empty -> explanatory line.
	if u := formatConflicts([]memory.Relation{{FromID: "x", ToID: "y"}}, map[string]string{}); !strings.Contains(u, "x") || !strings.Contains(u, "y") {
		t.Errorf("unresolved ids should show raw: %s", u)
	}
	if e := formatConflicts(nil, nil); !strings.Contains(e, "no conflicts") {
		t.Errorf("empty should explain none: %q", e)
	}
}

func TestMemoryConflictsCLI_ListsEdges(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memoryConflictsCmd.SetOut(nil) })

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	a, err := ms.Upsert("testing/pattern-a", "use pattern A", memory.UpsertOpts{Type: "testing"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ms.Upsert("testing/pattern-c", "use pattern C", memory.UpsertOpts{Type: "testing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Relate(a.ID, b.ID, "conflicts_with"); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	memoryConflictsCmd.SetOut(&buf)
	if err := memoryConflictsCmd.RunE(memoryConflictsCmd, nil); err != nil {
		t.Fatalf("memory conflicts: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "testing/pattern-a") || !strings.Contains(out, "testing/pattern-c") {
		t.Errorf("expected both topic keys in output:\n%s", out)
	}
}
