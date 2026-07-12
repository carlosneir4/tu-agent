// Package cluster partitions weighted undirected graphs into
// modularity-maximizing communities. It is a pure, dependency-free leaf
// package (stdlib only) used to group related nodes without falling back to
// connected-components / single-linkage behavior.
package cluster

import "sort"

// Edge is an undirected weighted edge; A and B are node indices in [0,n).
type Edge struct{ A, B, Weight int }

// Communities partitions n nodes (indices 0..n-1) over weighted undirected
// edges into modularity-maximizing communities using a deterministic
// Louvain-style greedy optimization (local moving + aggregation).
//
// The result is independent of the order of the edges slice: weights are
// accumulated as exact integers and every tie is broken by ascending index,
// so no Go map iteration order can leak into the outcome.
//
// Each returned community's members are sorted ascending, and the communities
// themselves are ordered by their smallest member ascending. Every node index
// in [0,n) appears in exactly one community; a node with no incident edge is
// returned as its own singleton.
func Communities(n int, edges []Edge) [][]int {
	if n <= 0 {
		return [][]int{}
	}

	// Build the level-0 weighted adjacency. adj[i][j] holds the summed weight
	// of edges between i and j (symmetric). A self-loop contributes twice to
	// the incident node's degree and to its self weight, matching the standard
	// undirected modularity convention. totDeg is the sum of all degrees,
	// which equals 2m and is invariant under aggregation.
	adj := make([]map[int]int64, n)
	for i := range adj {
		adj[i] = make(map[int]int64)
	}
	deg := make([]int64, n)
	var totDeg int64
	for _, e := range edges {
		w := int64(e.Weight)
		a, b := e.A, e.B
		adj[a][b] += w
		adj[b][a] += w
		deg[a] += w
		deg[b] += w
		totDeg += 2 * w
	}

	// label[o] is the current-level node that original node o belongs to.
	label := make([]int, n)
	for i := range label {
		label[i] = i
	}

	curAdj, curDeg, curN := adj, deg, n
	for {
		comm := localMoving(curN, curAdj, curDeg, totDeg)

		// Compact community ids by ascending first appearance (deterministic).
		compact := make([]int, curN)
		for i := range compact {
			compact[i] = -1
		}
		numComm := 0
		for node := 0; node < curN; node++ {
			c := comm[node]
			if compact[c] == -1 {
				compact[c] = numComm
				numComm++
			}
		}

		// Fold this level's assignment into the original-node labels.
		for o := 0; o < n; o++ {
			label[o] = compact[comm[label[o]]]
		}

		if numComm == curN {
			break // No merges happened; the partition is stable.
		}

		// Build the aggregated graph: one node per compacted community.
		newAdj := make([]map[int]int64, numComm)
		for i := range newAdj {
			newAdj[i] = make(map[int]int64)
		}
		newDeg := make([]int64, numComm)
		for node := 0; node < curN; node++ {
			cu := compact[comm[node]]
			newDeg[cu] += curDeg[node]
			for other, w := range curAdj[node] {
				cv := compact[comm[other]]
				newAdj[cu][cv] += w
			}
		}

		curAdj, curDeg, curN = newAdj, newDeg, numComm
	}

	// Group original nodes by final label, then sort for a stable result.
	groups := make(map[int][]int)
	for o := 0; o < n; o++ {
		groups[label[o]] = append(groups[label[o]], o)
	}
	out := make([][]int, 0, len(groups))
	for _, members := range groups {
		sort.Ints(members)
		out = append(out, members)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i][0] < out[j][0]
	})
	return out
}

// localMoving runs the Louvain local-moving phase over a weighted undirected
// graph until no node changes community. It returns, for each node, the id of
// the community it belongs to. Nodes are visited in ascending index order and
// every tie is resolved toward the smallest community id, so the result does
// not depend on Go map iteration order.
func localMoving(nn int, adj []map[int]int64, deg []int64, totDeg int64) []int {
	comm := make([]int, nn)
	sigmaTot := make([]int64, nn)
	for i := 0; i < nn; i++ {
		comm[i] = i
		sigmaTot[i] = deg[i]
	}

	for {
		moved := false
		for i := 0; i < nn; i++ {
			cOld := comm[i]
			sigmaTot[cOld] -= deg[i]

			// Summed weight from i into each neighboring community (excluding
			// self-loops). The current community is always a candidate so a
			// node with no beneficial move simply stays put.
			neigh := make(map[int]int64)
			for j, w := range adj[i] {
				if j == i {
					continue
				}
				neigh[comm[j]] += w
			}
			if _, ok := neigh[cOld]; !ok {
				neigh[cOld] = 0
			}

			cands := make([]int, 0, len(neigh))
			for c := range neigh {
				cands = append(cands, c)
			}
			sort.Ints(cands)

			// Maximize k_{i,in} - sigmaTot[c]*deg[i]/totDeg. Multiplying the
			// comparison by the constant totDeg keeps it exact integer math:
			//   gain(c) = totDeg*neigh[c] - sigmaTot[c]*deg[i]
			// Ascending candidate order + strict improvement break ties toward
			// the smallest community id.
			bestC := cOld
			bestGain := totDeg*neigh[cOld] - sigmaTot[cOld]*deg[i]
			for _, c := range cands {
				if c == cOld {
					continue
				}
				gain := totDeg*neigh[c] - sigmaTot[c]*deg[i]
				if gain > bestGain {
					bestGain = gain
					bestC = c
				}
			}

			comm[i] = bestC
			sigmaTot[bestC] += deg[i]
			if bestC != cOld {
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return comm
}
