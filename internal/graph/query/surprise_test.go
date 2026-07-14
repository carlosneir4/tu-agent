package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

func TestDomainOf(t *testing.T) {
	cases := []struct {
		name      string
		pkg, path string
		depth     int
		want      string
	}{
		{"dot package depth 2", "com.acme.billing", "", 2, "com/acme"},
		{"slash package depth 2", "internal/graph/query", "", 2, "internal/graph"},
		{"depth 1", "com.acme.billing", "", 1, "com"},
		{"path fallback when no package", "", "internal/tool/bash.go", 2, "internal/tool"},
		{"no package no path", "", "", 2, "external"},
		{"depth below 1 clamps to 1", "a.b.c", "", 0, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domainOf(tc.pkg, tc.path, tc.depth); got != tc.want {
				t.Errorf("domainOf(%q,%q,%d) = %q, want %q", tc.pkg, tc.path, tc.depth, got, tc.want)
			}
		})
	}
}

func TestDomainOfID(t *testing.T) {
	nodes := []graph.Node{
		{ID: "x/Foo", Kind: graph.KindFunction, Name: "Foo", Path: "x/foo.go"},
		{ID: "ext", Kind: graph.KindExternal, Name: "Ext"},
	}
	g := NewGraph(nodes, nil, WithPackages(map[string]string{"x/foo.go": "com.acme.billing"}))
	if got := g.domainOfID("x/Foo", 2); got != "com/acme" {
		t.Errorf("domainOfID(x/Foo) = %q, want com/acme", got)
	}
	if got := g.domainOfID("ext", 2); got != "external" {
		t.Errorf("external node domain = %q, want external", got)
	}
	if got := g.domainOfID("missing", 2); got != "external" {
		t.Errorf("unknown id domain = %q, want external", got)
	}
}

// surpriseFixture builds a graph where domain "a" has 20 cross-domain calls: 19 to
// domain "b" (common) and 1 to domain "c" (rare). share(a->c)=1/20=0.05 < 0.10
// threshold (surprising); share(a->b)=19/20 (not surprising). All are call edges.
func surpriseFixture() *Graph {
	pkg := map[string]string{"a/src.go": "a", "c/rare.go": "c"}
	nodes := []graph.Node{
		{ID: "a/Src", Kind: graph.KindFunction, Name: "Src", Path: "a/src.go"},
		{ID: "c/Rare", Kind: graph.KindFunction, Name: "Rare", Path: "c/rare.go"},
	}
	edges := []graph.Edge{}
	// a -> b x19 (common)
	for i := 0; i < 19; i++ {
		id := fmt.Sprintf("b/B%d", i)
		path := fmt.Sprintf("b/b%d.go", i)
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindFunction, Name: fmt.Sprintf("B%d", i), Path: path})
		pkg[path] = "b"
		edges = append(edges, graph.Edge{From: "a/Src", To: id, Kind: graph.EdgeCalls})
	}
	// a -> c x1 (rare)
	edges = append(edges, graph.Edge{From: "a/Src", To: "c/Rare", Kind: graph.EdgeCalls})
	return NewGraph(nodes, edges, WithPackages(pkg))
}

func TestComputeSurprising_FlagsRareEdge(t *testing.T) {
	g := surpriseFixture()
	res, _ := g.Impact("c/Rare", 2, DirUp, 0)
	got := g.ComputeSurprising("c/Rare", res, SurpriseConfig{})
	if len(got) != 1 {
		t.Fatalf("want exactly 1 surprising edge, got %d: %+v", len(got), got)
	}
	e := got[0]
	if e.FromID != "a/Src" || e.ToID != "c/Rare" {
		t.Errorf("wrong edge: %s -> %s", e.FromID, e.ToID)
	}
	if e.FromDomain != "a" || e.ToDomain != "c" {
		t.Errorf("domains = %s -> %s, want a -> c", e.FromDomain, e.ToDomain)
	}
	if e.Score < 0.94 || e.Score > 0.96 {
		t.Errorf("score = %.3f, want ~0.95", e.Score)
	}
}

func TestComputeSurprising_CommonEdgeNotFlagged(t *testing.T) {
	g := surpriseFixture()
	res, _ := g.Impact("b/B0", 2, DirUp, 0)
	got := g.ComputeSurprising("b/B0", res, SurpriseConfig{})
	for _, e := range got {
		if e.ToDomain == "b" {
			t.Errorf("common a->b edge must not be surprising: %+v", e)
		}
	}
}

func TestComputeSurprising_SupportGuard(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a/Src", Kind: graph.KindFunction, Name: "Src", Path: "a/src.go"},
		{ID: "z/Z", Kind: graph.KindFunction, Name: "Z", Path: "z/z.go"},
	}
	edges := []graph.Edge{{From: "a/Src", To: "z/Z", Kind: graph.EdgeCalls}}
	g := NewGraph(nodes, edges, WithPackages(map[string]string{"a/src.go": "a", "z/z.go": "z"}))
	res, _ := g.Impact("z/Z", 2, DirUp, 0)
	if got := g.ComputeSurprising("z/Z", res, SurpriseConfig{}); len(got) != 0 {
		t.Errorf("support guard: want 0 surprising, got %+v", got)
	}
}

func TestComputeSurprising_SameDomainAndExternalNeverFlagged(t *testing.T) {
	nodes := []graph.Node{
		{ID: "a/Src", Kind: graph.KindFunction, Name: "Src", Path: "a/src.go"},
		{ID: "a/Peer", Kind: graph.KindFunction, Name: "Peer", Path: "a/peer.go"},
		{ID: "ext", Kind: graph.KindExternal, Name: "Ext"},
	}
	edges := []graph.Edge{
		{From: "a/Src", To: "a/Peer", Kind: graph.EdgeCalls}, // same domain
		{From: "a/Src", To: "ext", Kind: graph.EdgeCalls},    // external target
	}
	g := NewGraph(nodes, edges, WithPackages(map[string]string{"a/src.go": "a", "a/peer.go": "a"}))
	res, _ := g.Impact("a/Src", 2, DirDown, 0)
	if got := g.ComputeSurprising("a/Src", res, SurpriseConfig{}); len(got) != 0 {
		t.Errorf("same-domain/external must never be surprising, got %+v", got)
	}
}

func TestFormatSurprising(t *testing.T) {
	res := &ImpactResult{SurprisingEdges: []SurprisingEdge{
		{FromID: "a/Src", ToID: "c/Rare", FromName: "Src", ToName: "Rare", FromDomain: "a", ToDomain: "c", Score: 0.95},
	}}
	out := FormatSurprising("c/Rare", res, 50)
	if !strings.Contains(out, "a::Src → c::Rare") || !strings.Contains(out, "surprise: 0.95") {
		t.Errorf("missing formatted edge:\n%s", out)
	}

	empty := FormatSurprising("x", &ImpactResult{}, 50)
	if !strings.Contains(empty, "No surprising") {
		t.Errorf("empty case should say none:\n%s", empty)
	}
}

// TestComputeSurprising_ThresholdBoundary covers spec §6: a pair whose share is
// exactly at the threshold is NOT surprising (the comparison is strict `<`).
// Domain "a" has 10 cross-domain edges: 1 to "c" (share 1/10 = 0.10) and 9 to "b".
// With the default threshold 0.10, the a->c edge sits exactly on the boundary and
// must be excluded.
func TestComputeSurprising_ThresholdBoundary(t *testing.T) {
	pkg := map[string]string{"a/src.go": "a", "c/rare.go": "c"}
	nodes := []graph.Node{
		{ID: "a/Src", Kind: graph.KindFunction, Name: "Src", Path: "a/src.go"},
		{ID: "c/Rare", Kind: graph.KindFunction, Name: "Rare", Path: "c/rare.go"},
	}
	edges := []graph.Edge{}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("b/B%d", i)
		path := fmt.Sprintf("b/b%d.go", i)
		nodes = append(nodes, graph.Node{ID: id, Kind: graph.KindFunction, Name: fmt.Sprintf("B%d", i), Path: path})
		pkg[path] = "b"
		edges = append(edges, graph.Edge{From: "a/Src", To: id, Kind: graph.EdgeCalls})
	}
	edges = append(edges, graph.Edge{From: "a/Src", To: "c/Rare", Kind: graph.EdgeCalls})
	g := NewGraph(nodes, edges, WithPackages(pkg))
	res, _ := g.Impact("c/Rare", 2, DirUp, 0)
	for _, e := range g.ComputeSurprising("c/Rare", res, SurpriseConfig{}) {
		if e.ToDomain == "c" {
			t.Errorf("share exactly at threshold (0.10) must not be surprising: %+v", e)
		}
	}
}

func TestFormatImpactIncludesSurprisingSection(t *testing.T) {
	res := &ImpactResult{
		Hits:            []Hit{{Node: graph.Node{ID: "a/Src", Name: "Src", Path: "a/src.go", Line: 1}}},
		SurprisingEdges: []SurprisingEdge{{FromID: "a/Src", ToID: "c/Rare", FromName: "Src", ToName: "Rare", FromDomain: "a", ToDomain: "c", Score: 0.95}},
	}
	out := FormatImpact("c/Rare", res, 50)
	if !strings.Contains(out, "Cross-domain dependencies (surprising)") || !strings.Contains(out, "a::Src → c::Rare") {
		t.Errorf("FormatImpact missing surprising section:\n%s", out)
	}
}
