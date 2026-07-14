package query

import (
	"sort"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

// BridgeConfig tunes betweenness approximation. Zero values normalize to defaults.
type BridgeConfig struct {
	Samples    int     // source nodes sampled (default 100; <=0 → 100)
	TopPercent float64 // top percentile that counts as a chokepoint (default 5)
}

func (c BridgeConfig) samples() int {
	if c.Samples <= 0 {
		return 100
	}
	return c.Samples
}

func (c BridgeConfig) topPercent() float64 {
	if c.TopPercent <= 0 {
		return 5
	}
	return c.TopPercent
}

// callAdjacency builds a From->[]To adjacency over calls edges only.
func (g *Graph) callAdjacency() map[string][]string {
	adj := make(map[string][]string)
	for from, edges := range g.fwd {
		for _, e := range edges {
			if e.Kind == graph.EdgeCalls {
				adj[from] = append(adj[from], e.To)
			}
		}
	}
	return adj
}

// BridgeScores returns approximate betweenness per node over the calls subgraph,
// via Brandes from the first `Samples` sorted node IDs. Deterministic; cached per
// (Graph, Samples).
func (g *Graph) BridgeScores(cfg BridgeConfig) map[string]float64 {
	k := cfg.samples()
	if g.bridgeCache == nil {
		g.bridgeCache = make(map[int]map[string]float64)
	}
	if cached, ok := g.bridgeCache[k]; ok {
		return cached
	}

	adj := g.callAdjacency()
	nodeset := make(map[string]bool)
	for from, tos := range adj {
		nodeset[from] = true
		for _, to := range tos {
			nodeset[to] = true
		}
	}
	nodes := make([]string, 0, len(nodeset))
	for n := range nodeset {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	sources := nodes
	if len(nodes) > k {
		sources = nodes[:k]
	}

	cb := make(map[string]float64, len(nodes))
	for _, s := range sources {
		stack := make([]string, 0, len(nodes))
		pred := make(map[string][]string)
		sigma := make(map[string]float64, len(nodes))
		dist := make(map[string]int, len(nodes))
		for _, v := range nodes {
			sigma[v] = 0
			dist[v] = -1
		}
		sigma[s] = 1
		dist[s] = 0
		queue := []string{s}
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}
		delta := make(map[string]float64, len(nodes))
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	}
	g.bridgeCache[k] = cb
	return cb
}

// BridgeRank is one node's bridge score for the top listing.
type BridgeRank struct {
	ID    string
	Name  string
	Path  string
	Score float64
}

// IsChokepoint reports a node's bridge score and whether it ranks in the top
// TopPercent of positively-scored nodes (and is itself > 0).
func (g *Graph) IsChokepoint(id string, cfg BridgeConfig) (float64, bool) {
	scores := g.BridgeScores(cfg)
	score := scores[id]
	if score <= 0 {
		return score, false
	}
	vals := make([]float64, 0, len(scores))
	for _, v := range scores {
		if v > 0 {
			vals = append(vals, v)
		}
	}
	sort.Float64s(vals)
	idx := int(float64(len(vals)) * (1 - cfg.topPercent()/100.0))
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	if idx < 0 {
		idx = 0
	}
	threshold := vals[idx]
	return score, score >= threshold
}

// BridgeTop returns the n highest-scoring nodes, descending (ties broken by ID).
func (g *Graph) BridgeTop(cfg BridgeConfig, n int) []BridgeRank {
	scores := g.BridgeScores(cfg)
	out := make([]BridgeRank, 0, len(scores))
	for id, sc := range scores {
		if sc <= 0 {
			continue
		}
		node := g.nodes[id]
		out = append(out, BridgeRank{ID: id, Name: node.Name, Path: node.Path, Score: sc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
