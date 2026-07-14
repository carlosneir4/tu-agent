package codegen_test

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func TestAggregateToDomains_RollupDropSelfAndUnmapped(t *testing.T) {
	edges := []codegen.Edge{
		{From: "w/Widget.java", To: "r/Renderer.java"}, // widgets -> render
		{From: "w/Widget.java", To: "w/Helper.java"},   // widgets -> widgets (self, drop)
		{From: "w/Widget.java", To: "x/External.java"}, // unmapped target (drop)
	}
	f2d := map[string]string{
		"w/Widget.java": "widgets", "w/Helper.java": "widgets", "r/Renderer.java": "render",
	}
	got := codegen.AggregateToDomains(edges, f2d)
	if len(got) != 1 || got[0].From != "widgets" || got[0].To != "render" {
		t.Fatalf("got %v, want [widgets->render]", got)
	}
}
