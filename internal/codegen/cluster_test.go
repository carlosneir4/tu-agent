package codegen

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// edgesAmong builds an unweighted clique among the given paths.
func edgesAmong(paths ...string) []WeightedEdge {
	var out []WeightedEdge
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			out = append(out, WeightedEdge{From: paths[i], To: paths[j], Weight: 1})
		}
	}
	return out
}

func TestClusterFilesTwoCliquesWithBridge(t *testing.T) {
	files := []string{"a1", "a2", "a3", "b1", "b2", "b3"}
	edges := append(edgesAmong("a1", "a2", "a3"), edgesAmong("b1", "b2", "b3")...)
	edges = append(edges, WeightedEdge{From: "a1", To: "b1", Weight: 1})

	got := ClusterFiles(files, edges)

	want := [][]string{{"a1", "a2", "a3"}, {"b1", "b2", "b3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClusterFiles = %v, want %v", got, want)
	}
}

func TestClusterFilesWeightsDecideMembership(t *testing.T) {
	// b is pulled toward a (weight 10), not toward c (weight 1).
	files := []string{"a", "b", "c", "d"}
	edges := []WeightedEdge{
		{From: "a", To: "b", Weight: 10},
		{From: "c", To: "d", Weight: 10},
		{From: "b", To: "c", Weight: 1},
	}

	got := ClusterFiles(files, edges)

	want := [][]string{{"a", "b"}, {"c", "d"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClusterFiles = %v, want %v", got, want)
	}
}

func TestClusterFilesDeterministicAcrossEdgeOrder(t *testing.T) {
	files := []string{"a1", "a2", "a3", "b1", "b2", "b3"}
	edges := append(edgesAmong("a1", "a2", "a3"), edgesAmong("b1", "b2", "b3")...)
	edges = append(edges, WeightedEdge{From: "a1", To: "b1", Weight: 1})

	first := ClusterFiles(files, edges)
	// Reverse the edge slice: result must be identical.
	rev := make([]WeightedEdge, 0, len(edges))
	for i := len(edges) - 1; i >= 0; i-- {
		rev = append(rev, edges[i])
	}
	second := ClusterFiles(files, rev)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("edge order changed the result:\n first = %v\nsecond = %v", first, second)
	}
}

func TestClusterFilesNoEdgesYieldsSingletons(t *testing.T) {
	got := ClusterFiles([]string{"b", "a"}, nil)
	want := [][]string{{"a"}, {"b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClusterFiles = %v, want %v", got, want)
	}
}

func TestClusterFilesIgnoresUnknownAndSelfEdges(t *testing.T) {
	files := []string{"a", "b"}
	edges := []WeightedEdge{
		{From: "a", To: "a", Weight: 5},     // self loop: ignored
		{From: "a", To: "ghost", Weight: 5}, // unknown endpoint: ignored
		{From: "a", To: "b", Weight: 0},     // zero weight: ignored
	}
	got := ClusterFiles(files, edges)
	want := [][]string{{"a"}, {"b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClusterFiles = %v, want %v", got, want)
	}
}

func unitsByPath(us []SourceUnit) map[string]SourceUnit {
	m := make(map[string]SourceUnit, len(us))
	for _, u := range us {
		m[u.Path] = u
	}
	return m
}

func TestCommunitiesToDomainsNamesByCommonPackage(t *testing.T) {
	units := unitsByPath([]SourceUnit{
		{Path: "src/b/Invoice.java", Package: "com.acme.billing"},
		{Path: "src/b/Ledger.java", Package: "com.acme.billing.ledger"},
		{Path: "src/c/Base.java", Package: "com.acme.core"},
	})
	comms := [][]string{
		{"src/b/Invoice.java", "src/b/Ledger.java"},
		{"src/c/Base.java"},
	}

	got := communitiesToDomains(comms, units, "com.acme")

	if len(got) != 2 {
		t.Fatalf("domains = %d, want 2", len(got))
	}
	if got[0].Name != "billing" || got[0].Package != "com.acme.billing" {
		t.Errorf("domain 0 = %q/%q, want billing/com.acme.billing", got[0].Name, got[0].Package)
	}
	if got[1].Name != "core" || got[1].Package != "com.acme.core" {
		t.Errorf("domain 1 = %q/%q, want core/com.acme.core", got[1].Name, got[1].Package)
	}
}

func TestCommunitiesToDomainsFlattensSlashPackages(t *testing.T) {
	// Go packages are slash-separated. The domain name must be a flat kebab
	// string with no "/", otherwise filepath.Join nests the SKILL.md card
	// under unreadable subdirectories.
	units := unitsByPath([]SourceUnit{
		{Path: "internal/graph/extract/build.go", Package: "internal/graph/extract"},
		{Path: "internal/graph/extract/java.go", Package: "internal/graph/extract"},
		{Path: "cmd/tu-agent/main.go", Package: "cmd/tu-agent"},
	})
	comms := [][]string{
		{"internal/graph/extract/build.go", "internal/graph/extract/java.go"},
		{"cmd/tu-agent/main.go"},
	}

	got := communitiesToDomains(comms, units, "")

	if len(got) != 2 {
		t.Fatalf("domains = %d, want 2", len(got))
	}
	names := map[string]bool{}
	for _, d := range got {
		if strings.Contains(d.Name, "/") {
			t.Errorf("domain name %q contains '/'; want flat kebab name", d.Name)
		}
		names[d.Name] = true
	}
	for _, want := range []string{"internal-graph-extract", "cmd-tu-agent"} {
		if !names[want] {
			t.Errorf("missing flat domain name %q; got %v", want, names)
		}
	}
}

func TestCommunitiesToDomainsSuffixesNameCollisions(t *testing.T) {
	// Two topological communities inside the same package must get distinct names.
	units := unitsByPath([]SourceUnit{
		{Path: "f1", Package: "com.acme.app"},
		{Path: "f2", Package: "com.acme.app"},
		{Path: "f3", Package: "com.acme.app"},
		{Path: "f4", Package: "com.acme.app"},
	})
	comms := [][]string{{"f1", "f2"}, {"f3", "f4"}}

	got := communitiesToDomains(comms, units, "com.acme")

	if got[0].Name != "app" {
		t.Errorf("domain 0 name = %q, want app", got[0].Name)
	}
	if got[1].Name != "app-2" {
		t.Errorf("domain 1 name = %q, want app-2", got[1].Name)
	}
}

func TestCommunitiesToDomainsEmptyPrefixFallsBackToRoot(t *testing.T) {
	// Members from unrelated trees: common prefix is "", package falls back to root.
	units := unitsByPath([]SourceUnit{
		{Path: "x", Package: "alpha.one"},
		{Path: "y", Package: "beta.two"},
	})
	got := communitiesToDomains([][]string{{"x", "y"}}, units, "")
	if got[0].Name != "core" {
		t.Errorf("name = %q, want core (root rendering)", got[0].Name)
	}
}

// clusterFixture builds n files in pkg with the given path prefix.
func clusterFixture(prefix, pkg string, n int) []SourceUnit {
	out := make([]SourceUnit, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, SourceUnit{
			Path:    fmt.Sprintf("%s%02d.java", prefix, i),
			Package: pkg,
			Size:    100,
		})
	}
	return out
}

func pathsOf(us []SourceUnit) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.Path)
	}
	return out
}

func TestBuildDomainMapClusteredFallsBackUnder20Files(t *testing.T) {
	files := clusterFixture("src/a/A", "com.acme.alpha", 3)
	files = append(files, clusterFixture("src/b/B", "com.acme.beta", 3)...)
	opts := DomainMapOptions{Depth: 1, MinFiles: 1}

	got := BuildDomainMapClustered(files, nil, []WeightedEdge{{From: files[0].Path, To: files[1].Path, Weight: 1}}, opts)
	want := BuildDomainMap(files, nil, opts)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("under-20 fallback: got %v, want heuristic result %v", got, want)
	}
}

func TestBuildDomainMapClusteredSplitsByTopologyNotPackage(t *testing.T) {
	// 24 files in ONE package, but two 12-file cliques: the heuristic yields
	// one domain; clustering must yield two (PRD M2.1 success criterion shape).
	a := clusterFixture("src/app/A", "com.acme.app", 12)
	b := clusterFixture("src/app/B", "com.acme.app", 12)
	files := append(append([]SourceUnit(nil), a...), b...)
	weighted := append(edgesAmong(pathsOf(a)...), edgesAmong(pathsOf(b)...)...)
	opts := DomainMapOptions{Depth: 1, MinFiles: 1}

	heur := BuildDomainMap(files, nil, opts)
	if len(heur) != 1 {
		t.Fatalf("precondition: heuristic domains = %d, want 1", len(heur))
	}
	got := BuildDomainMapClustered(files, nil, weighted, opts)
	if len(got) != 2 {
		t.Fatalf("clustered domains = %d, want 2: %v", len(got), got)
	}
	if got[0].Name != "app" || got[1].Name != "app-2" {
		t.Errorf("names = %q, %q; want app, app-2", got[0].Name, got[1].Name)
	}
	if len(got[0].Files) != 12 || len(got[1].Files) != 12 {
		t.Errorf("sizes = %d, %d; want 12, 12", len(got[0].Files), len(got[1].Files))
	}
}

func TestBuildDomainMapClusteredGroupsZeroDegreeFilesByPackage(t *testing.T) {
	// 20 connected files + 2 unconnected files sharing a package: the loose
	// files must form one package-grouped domain, not two singletons.
	a := clusterFixture("src/app/A", "com.acme.app", 10)
	b := clusterFixture("src/app/B", "com.acme.app", 10)
	loose := clusterFixture("src/util/U", "com.acme.util", 2)
	files := append(append(append([]SourceUnit(nil), a...), b...), loose...)
	weighted := append(edgesAmong(pathsOf(a)...), edgesAmong(pathsOf(b)...)...)
	opts := DomainMapOptions{Depth: 1, MinFiles: 1}

	got := BuildDomainMapClustered(files, nil, weighted, opts)

	var util *Domain
	for i := range got {
		if got[i].Name == "util" {
			util = &got[i]
		}
	}
	if util == nil {
		t.Fatalf("no util domain in %v", got)
	}
	if len(util.Files) != 2 {
		t.Errorf("util files = %d, want 2", len(util.Files))
	}
}

func TestBuildDomainMapClusteredAppliesSplitGuardrail(t *testing.T) {
	// One 24-file community over MaxFiles=10 must still be split by the
	// existing guardrail (sub-package split, then parent marker emitted).
	a := clusterFixture("src/app/x/X", "com.acme.app.x", 12)
	b := clusterFixture("src/app/y/Y", "com.acme.app.y", 12)
	files := append(append([]SourceUnit(nil), a...), b...)
	weighted := edgesAmong(pathsOf(files)...) // one big clique => one community
	opts := DomainMapOptions{Depth: 1, MinFiles: 1, MaxFiles: 10}

	got := BuildDomainMapClustered(files, nil, weighted, opts)

	var parents, leaves int
	for _, d := range got {
		if d.Files == nil {
			parents++
		} else {
			leaves++
			if len(d.Files) > 10 {
				t.Errorf("domain %s has %d files, over MaxFiles=10", d.Name, len(d.Files))
			}
		}
	}
	if parents == 0 || leaves < 2 {
		t.Errorf("expected a parent marker and >=2 leaves, got parents=%d leaves=%d: %v", parents, leaves, got)
	}
}

func TestBuildDomainMapClusteredDeterministic(t *testing.T) {
	a := clusterFixture("src/app/A", "com.acme.app", 12)
	b := clusterFixture("src/app/B", "com.acme.app", 12)
	files := append(append([]SourceUnit(nil), a...), b...)
	weighted := append(edgesAmong(pathsOf(a)...), edgesAmong(pathsOf(b)...)...)
	opts := DomainMapOptions{Depth: 1, MinFiles: 1}

	first := BuildDomainMapClustered(files, nil, weighted, opts)
	rev := make([]WeightedEdge, 0, len(weighted))
	for i := len(weighted) - 1; i >= 0; i-- {
		rev = append(rev, weighted[i])
	}
	second := BuildDomainMapClustered(files, nil, rev, opts)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("edge order changed the domain map")
	}
}

func TestSubClusterOversizedSplitsAlongSeam(t *testing.T) {
	// Two 12-file cliques joined by a single bridge form one oversized
	// community; re-clustering the induced subgraph splits them at the seam.
	a := pathsOf(clusterFixture("a/A", "com.a", 12))
	b := pathsOf(clusterFixture("b/B", "com.b", 12))
	community := append(append([]string(nil), a...), b...)
	edges := append(edgesAmong(a...), edgesAmong(b...)...)
	edges = append(edges, WeightedEdge{From: a[0], To: b[0], Weight: 1})

	got := subClusterOversized(community, edges, 15)

	want := [][]string{append([]string(nil), a...), append([]string(nil), b...)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subClusterOversized = %v, want %v", got, want)
	}
}

func TestSubClusterOversizedReturnsCliqueWhole(t *testing.T) {
	// A genuine clique has no internal seam: even when oversized it comes back
	// whole, leaving the package/byte guardrail to slice it.
	c := pathsOf(clusterFixture("x/X", "com.x", 24))
	edges := edgesAmong(c...)

	got := subClusterOversized(c, edges, 15)

	want := [][]string{c}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subClusterOversized = %v, want %v (clique must stay whole)", got, want)
	}
}

func TestSubClusterOversizedRespectsCap(t *testing.T) {
	c := pathsOf(clusterFixture("x/X", "com.x", 10))
	edges := edgesAmong(c...)

	// At or under the cap: returned unchanged, no re-clustering.
	if got := subClusterOversized(c, edges, 15); !reflect.DeepEqual(got, [][]string{c}) {
		t.Errorf("under cap: got %v, want one group", got)
	}
	// Exactly at the cap: still unchanged (boundary, len(c) == maxFiles).
	if got := subClusterOversized(c, edges, 10); !reflect.DeepEqual(got, [][]string{c}) {
		t.Errorf("at cap: got %v, want one group", got)
	}
	// maxFiles <= 0 disables sub-clustering entirely.
	if got := subClusterOversized(c, edges, 0); !reflect.DeepEqual(got, [][]string{c}) {
		t.Errorf("maxFiles=0: got %v, want one group", got)
	}
}

func TestBuildDomainMapClusteredSubdividesResolutionLimitBlob(t *testing.T) {
	// 16 triangles (3-file cliques) in ONE package, chained by single bridges.
	// Single-pass greedy modularity hits its resolution limit and merges
	// adjacent triangles into oversized communities. Because every file shares
	// one package, splitHugeDomains cannot slice them and batchOverfullLeaves
	// would cut across triangle boundaries. Recursive sub-clustering must
	// instead recover topologically pure pieces: no leaf domain may mix two
	// triangles, and every leaf must respect MaxFiles.
	const tris = 16
	var files []SourceUnit
	var weighted []WeightedEdge
	triOf := map[string]int{}
	for ti := 0; ti < tris; ti++ {
		var members []string
		for k := 0; k < 3; k++ {
			p := fmt.Sprintf("src/T%02d_%d.java", ti, k)
			files = append(files, SourceUnit{Path: p, Package: "com.acme.app", Size: 100})
			members = append(members, p)
			triOf[p] = ti
		}
		weighted = append(weighted, edgesAmong(members...)...)
		if ti > 0 {
			prev := fmt.Sprintf("src/T%02d_0.java", ti-1)
			weighted = append(weighted, WeightedEdge{From: prev, To: members[0], Weight: 1})
		}
	}
	opts := DomainMapOptions{Depth: 1, MinFiles: 1, MaxFiles: 4}

	domains := BuildDomainMapClustered(files, nil, weighted, opts)

	leaves := 0
	for _, d := range domains {
		if d.Files == nil {
			continue // parent marker
		}
		leaves++
		if len(d.Files) > opts.MaxFiles {
			t.Errorf("domain %s has %d files, over MaxFiles=%d", d.Name, len(d.Files), opts.MaxFiles)
		}
		first := triOf[d.Files[0]]
		for _, f := range d.Files {
			if triOf[f] != first {
				t.Errorf("domain %s mixes triangle %d and %d: %v", d.Name, first, triOf[f], d.Files)
				break
			}
		}
	}
	if leaves < tris/2 {
		t.Errorf("leaf domains = %d, want >= %d (blob must be subdivided into topological pieces)", leaves, tris/2)
	}
}
