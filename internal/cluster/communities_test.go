package cluster

import (
	"reflect"
	"testing"
)

// reverseEdges returns a new slice with the edges in reverse order.
// This is a deterministic permutation (no math/rand) used to prove that
// Communities is order-invariant with respect to the input edge slice.
func reverseEdges(edges []Edge) []Edge {
	out := make([]Edge, len(edges))
	for i, e := range edges {
		out[len(edges)-1-i] = e
	}
	return out
}

// permuteEdges applies a fixed permutation given by idx (a list of source
// indices into edges) and returns the reordered slice. idx must be a
// permutation of [0,len(edges)). Deterministic — no randomness.
func permuteEdges(edges []Edge, idx []int) []Edge {
	out := make([]Edge, len(edges))
	for dst, src := range idx {
		out[dst] = edges[src]
	}
	return out
}

// communityOf returns the index (in partition) of the community that contains
// node n, or -1 if n is not present in any community.
func communityOf(partition [][]int, n int) int {
	for ci, members := range partition {
		for _, m := range members {
			if m == n {
				return ci
			}
		}
	}
	return -1
}

// containsNode reports whether the community members contains node n.
func containsNode(members []int, n int) bool {
	for _, m := range members {
		if m == n {
			return true
		}
	}
	return false
}

// @s1 Deterministic regardless of edge order.
// Build a fixed weighted graph, call Communities twice — once with the edges
// reversed — and assert the two partitions are byte-identical (same
// communities, same members, same ordering) via reflect.DeepEqual.
func TestCommunities_DeterministicRegardlessOfEdgeOrder_s1(t *testing.T) {
	const n = 6
	// Two triangles {0,1,2} and {3,4,5}, weakly linked 2-3.
	edges := []Edge{
		{A: 0, B: 1, Weight: 5},
		{A: 1, B: 2, Weight: 5},
		{A: 0, B: 2, Weight: 5},
		{A: 3, B: 4, Weight: 5},
		{A: 4, B: 5, Weight: 5},
		{A: 3, B: 5, Weight: 5},
		{A: 2, B: 3, Weight: 1},
	}

	first := Communities(n, edges)
	second := Communities(n, reverseEdges(edges))

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("@s1 partition not order-invariant:\n original order = %v\n reversed order = %v", first, second)
	}
}

// @s2 A zero-degree node comes back as its own singleton.
// Node index 4 appears in no edge; assert it is returned as the singleton
// community [4].
func TestCommunities_ZeroDegreeNodeIsSingleton_s2(t *testing.T) {
	const n = 5
	// Nodes 0..3 connected; node 4 has degree zero.
	edges := []Edge{
		{A: 0, B: 1, Weight: 3},
		{A: 1, B: 2, Weight: 3},
		{A: 2, B: 3, Weight: 3},
		{A: 0, B: 3, Weight: 3},
	}

	got := Communities(n, edges)

	ci := communityOf(got, 4)
	if ci == -1 {
		t.Fatalf("@s2 node 4 missing from partition entirely: %v", got)
	}
	if !reflect.DeepEqual(got[ci], []int{4}) {
		t.Fatalf("@s2 node 4 not returned as its own singleton [4]; its community = %v (full partition %v)", got[ci], got)
	}
}

// @s3 Modularity separates where single-linkage would merge.
// Two dense cliques joined by a single LOW-weight bridge edge. A
// connected-components / single-linkage clustering would merge everything into
// one community; modularity should keep two, and the two bridge endpoints must
// land in different communities.
func TestCommunities_ModularitySeparatesBridge_s3(t *testing.T) {
	const n = 8
	// Clique A = {0,1,2,3}, Clique B = {4,5,6,7}, each fully connected with
	// heavy weights. Single LOW-weight bridge 3-4.
	edges := []Edge{
		// Clique A (all pairs).
		{A: 0, B: 1, Weight: 10},
		{A: 0, B: 2, Weight: 10},
		{A: 0, B: 3, Weight: 10},
		{A: 1, B: 2, Weight: 10},
		{A: 1, B: 3, Weight: 10},
		{A: 2, B: 3, Weight: 10},
		// Clique B (all pairs).
		{A: 4, B: 5, Weight: 10},
		{A: 4, B: 6, Weight: 10},
		{A: 4, B: 7, Weight: 10},
		{A: 5, B: 6, Weight: 10},
		{A: 5, B: 7, Weight: 10},
		{A: 6, B: 7, Weight: 10},
		// Low-weight bridge between the two cliques.
		{A: 3, B: 4, Weight: 1},
	}

	got := Communities(n, edges)

	if len(got) < 2 {
		t.Fatalf("@s3 expected at least 2 communities, got %d: %v", len(got), got)
	}

	ciBridgeA := communityOf(got, 3)
	ciBridgeB := communityOf(got, 4)
	if ciBridgeA == -1 || ciBridgeB == -1 {
		t.Fatalf("@s3 bridge endpoint missing from partition: node3->%d node4->%d (%v)", ciBridgeA, ciBridgeB, got)
	}
	if ciBridgeA == ciBridgeB {
		t.Fatalf("@s3 bridge endpoints 3 and 4 landed in the SAME community %d — single-linkage behavior, not modularity: %v", ciBridgeA, got)
	}
}

// @s4 Integer-weight accumulation is order-invariant.
// Run Communities against several deterministic shuffles of the same
// integer-weighted edges and assert every run yields byte-identical community
// assignments (reflect.DeepEqual across all shuffles). Weights are chosen so
// that summed as raw floats in different orders a near-tie could flip; integer
// accumulation must not.
func TestCommunities_IntegerWeightAccumulationOrderInvariant_s4(t *testing.T) {
	const n = 6
	edges := []Edge{
		{A: 0, B: 1, Weight: 7},
		{A: 0, B: 2, Weight: 7},
		{A: 1, B: 2, Weight: 7},
		{A: 3, B: 4, Weight: 7},
		{A: 3, B: 5, Weight: 7},
		{A: 4, B: 5, Weight: 7},
		{A: 2, B: 3, Weight: 2},
		{A: 1, B: 4, Weight: 1},
	}

	// A set of fixed, distinct permutations of indices [0,len(edges)).
	permutations := [][]int{
		{0, 1, 2, 3, 4, 5, 6, 7}, // identity
		{7, 6, 5, 4, 3, 2, 1, 0}, // reverse
		{3, 1, 4, 0, 7, 2, 6, 5}, // fixed scramble
		{5, 0, 6, 2, 1, 7, 3, 4}, // another fixed scramble
	}

	baseline := Communities(n, permuteEdges(edges, permutations[0]))
	for i := 1; i < len(permutations); i++ {
		got := Communities(n, permuteEdges(edges, permutations[i]))
		if !reflect.DeepEqual(baseline, got) {
			t.Fatalf("@s4 partition differs under permutation %d:\n baseline = %v\n got      = %v", i, baseline, got)
		}
	}

	// Sanity: baseline must actually cover all n nodes (guards against a
	// degenerate all-empty result trivially satisfying DeepEqual).
	seen := 0
	for _, c := range baseline {
		seen += len(c)
	}
	if seen != n {
		t.Fatalf("@s4 partition does not cover all %d nodes, covers %d: %v", n, seen, baseline)
	}
	for node := 0; node < n; node++ {
		if communityOf(baseline, node) == -1 {
			t.Fatalf("@s4 node %d missing from partition: %v", node, baseline)
		}
	}
}
