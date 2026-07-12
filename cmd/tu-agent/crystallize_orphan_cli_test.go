package main

import (
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

// seedOrphanSkill stores a skill record whose provenance label matches NO live
// cluster, so `memory crystallize` must count it as an orphan. name is the
// curated topic segment (skill/<name>); label is the bound provenance label.
func seedOrphanSkill(t *testing.T, name, label string) {
	t.Helper()
	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	content := crystallize.ProvenanceLine(label, []memory.Observation{{TopicKey: "reference/" + label, Revision: 1}}) +
		"\n---\nname: " + name + "\n---\nbody\n"
	if _, err := ms.Upsert("skill/"+name, content, memory.UpsertOpts{Type: "skill"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}
}

// @s4 — `memory crystallize` prints a trailing orphan-count line for skill
// records bound to no live cluster, and those orphans never render as cluster
// rows. A store with zero orphans prints no such line.
func TestMemoryCrystallizeCLI_OrphanCountLine(t *testing.T) {
	t.Run("orphans present", func(t *testing.T) {
		t.Chdir(t.TempDir())
		memCrystallizeMin = 3
		t.Cleanup(func() { memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })

		seedCluster(t) // one live "checkout" cluster (3 notes)
		seedOrphanSkill(t, "acme-orphan-one", "acme-ghost-one")
		seedOrphanSkill(t, "acme-orphan-two", "acme-ghost-two")

		var buf strings.Builder
		memoryCrystallizeCmd.SetOut(&buf)
		if err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, nil); err != nil {
			t.Fatalf("memory crystallize: %v", err)
		}
		out := buf.String()

		if !strings.Contains(out, "2 orphaned skill record(s)") {
			t.Errorf("output missing trailing orphan-count line %q:\n%s", "2 orphaned skill record(s)", out)
		}
		// The live checkout cluster is still reported.
		if !strings.Contains(out, "checkout") {
			t.Errorf("output missing the live checkout cluster:\n%s", out)
		}
		// Orphans must NOT appear as cluster header rows ([status] [N notes] label)
		// nor as indented cluster member lines.
		for _, ghost := range []string{"acme-ghost-one", "acme-ghost-two"} {
			if strings.Contains(out, "notes] "+ghost) {
				t.Errorf("orphan label %q rendered as a cluster header row:\n%s", ghost, out)
			}
		}
		for _, topic := range []string{"skill/acme-orphan-one", "skill/acme-orphan-two"} {
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "  ") && strings.Contains(line, topic) {
					t.Errorf("orphan topic %q rendered as a cluster member row: %q", topic, line)
				}
			}
		}
	})

	t.Run("no orphans", func(t *testing.T) {
		t.Chdir(t.TempDir())
		memCrystallizeMin = 3
		t.Cleanup(func() { memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })

		seedCluster(t) // one live cluster, zero skill records

		var buf strings.Builder
		memoryCrystallizeCmd.SetOut(&buf)
		if err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, nil); err != nil {
			t.Fatalf("memory crystallize: %v", err)
		}
		if out := buf.String(); strings.Contains(out, "orphaned skill record") {
			t.Errorf("no skill records seeded, but output reports orphans:\n%s", out)
		}
	})
}
