package graph

import "sort"

// StronglyConnectedComponents returns the strongly-connected components of a
// directed graph given as an adjacency list (node -> successors). It uses an
// iterative Tarjan to avoid stack overflow on large graphs. Deterministic:
// components are sorted by descending size then by first member; members within
// a component are sorted by ID. Every node that appears as a key or as a
// successor is included (a node with no cycle is its own singleton component).
func StronglyConnectedComponents(adj map[string][]string) [][]string {
	nodeSet := make(map[string]struct{})
	for n, succs := range adj {
		nodeSet[n] = struct{}{}
		for _, s := range succs {
			nodeSet[s] = struct{}{}
		}
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes) // deterministic visiting order

	const undefined = -1
	index := make(map[string]int, len(nodes))
	low := make(map[string]int, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		index[n] = undefined
	}
	var tarStack []string
	idx := 0
	var comps [][]string

	type frame struct {
		node string
		next int
	}

	for _, root := range nodes {
		if index[root] != undefined {
			continue
		}
		stack := []frame{{node: root}}
		index[root] = idx
		low[root] = idx
		idx++
		tarStack = append(tarStack, root)
		onStack[root] = true

		for len(stack) > 0 {
			f := &stack[len(stack)-1]
			succs := adj[f.node]
			if f.next < len(succs) {
				w := succs[f.next]
				f.next++
				switch {
				case index[w] == undefined:
					index[w] = idx
					low[w] = idx
					idx++
					tarStack = append(tarStack, w)
					onStack[w] = true
					stack = append(stack, frame{node: w})
				// Tarjan back-edge rule: low[v] = min(low[v], index[w]). index[w] is used
				// deliberately (not low[w]); the child→parent low propagation on pop (below) compensates.
				case onStack[w] && index[w] < low[f.node]:
					low[f.node] = index[w]
				}
				continue
			}
			if low[f.node] == index[f.node] {
				var comp []string
				for {
					w := tarStack[len(tarStack)-1]
					tarStack = tarStack[:len(tarStack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == f.node {
						break
					}
				}
				sort.Strings(comp)
				comps = append(comps, comp)
			}
			child := f.node
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				parent := stack[len(stack)-1].node
				if low[child] < low[parent] {
					low[parent] = low[child]
				}
			}
		}
	}

	sort.SliceStable(comps, func(i, j int) bool {
		if len(comps[i]) != len(comps[j]) {
			return len(comps[i]) > len(comps[j])
		}
		return comps[i][0] < comps[j][0]
	})
	return comps
}
