package codegen_test

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// BuildFileToDomain maps each member file to its owning skill, sourcing the
// files from Skill.Files (the store's concept->files link), NOT from a
// "## Key Files" body section. On collision the first skill by sorted Name wins.
func TestBuildFileToDomain_CollisionFirstSortedWins(t *testing.T) {
	skills := []codegen.Skill{
		{Name: "zeta", Files: []string{"shared/Util.java"}},
		{Name: "alpha", Files: []string{"shared/Util.java", "a/A.java"}},
	}
	m := codegen.BuildFileToDomain(skills)
	if m["shared/Util.java"] != "alpha" {
		t.Errorf("collision winner = %q, want alpha", m["shared/Util.java"])
	}
	if m["a/A.java"] != "alpha" {
		t.Errorf("a/A.java = %q, want alpha", m["a/A.java"])
	}
}
