package codegen_test

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func TestParseKeyFiles_BulletsAndStop(t *testing.T) {
	body := "## Purpose\nstuff\n\n## Key Files\n" +
		"- core/Widget.java: the widget\n" +
		"- core/Render.java\n" +
		"- `core/Cache.java`: cache\n\n" +
		"## Patterns\n- not a file\n"
	got := codegen.ParseKeyFiles(body)
	want := []string{"core/Widget.java", "core/Render.java", "core/Cache.java"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildFileToDomain_CollisionFirstSortedWins(t *testing.T) {
	skills := []codegen.Skill{
		{Name: "zeta", Body: "## Key Files\n- shared/Util.java\n"},
		{Name: "alpha", Body: "## Key Files\n- shared/Util.java\n- a/A.java\n"},
	}
	m := codegen.BuildFileToDomain(skills)
	if m["shared/Util.java"] != "alpha" {
		t.Errorf("collision winner = %q, want alpha", m["shared/Util.java"])
	}
	if m["a/A.java"] != "alpha" {
		t.Errorf("a/A.java = %q, want alpha", m["a/A.java"])
	}
}
