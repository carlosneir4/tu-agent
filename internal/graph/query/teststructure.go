package query

import (
	"sort"

	"github.com/carlosneir4/tu-agent/internal/graph"
)

// TestStructureResult describes how a class's existing tests are organized, as
// facts for the generation context. SharedBase is the base class extended by
// every one of the class's test classes (empty when they disagree or there is
// none). Grouping is "per-concern" when more than one test class exists for the
// class, "single" for exactly one, "none" for zero.
type TestStructureResult struct {
	SharedBase string
	Grouping   string
	Siblings   []string
}

// TestStructure reports the test-organization conventions for the class that
// owns targetID (a class node, or any node it contains, e.g. a method). It
// reads the class's tested_by edges and the extends closure of each test class.
func (g *Graph) TestStructure(targetID string) TestStructureResult {
	classID := g.containingClass(targetID)
	res := TestStructureResult{Grouping: "none"}
	if classID == "" {
		return res
	}

	var siblings []string
	baseSets := make([][]string, 0)
	for _, e := range g.fwd[classID] {
		if e.Kind != graph.EdgeTestedBy {
			continue
		}
		tn, ok := g.nodes[e.To]
		if !ok {
			continue
		}
		siblings = append(siblings, tn.Name)
		baseSets = append(baseSets, g.extendsClosure(e.To))
	}
	if len(siblings) == 0 {
		return res
	}
	sort.Strings(siblings)
	res.Siblings = siblings
	if len(siblings) > 1 {
		res.Grouping = "per-concern"
	} else {
		res.Grouping = "single"
	}
	res.SharedBase = sharedFirst(baseSets, g)
	return res
}

// containingClass returns targetID if it is a class node, otherwise the nearest
// ancestor class reached over reverse contains edges, or "" if none.
func (g *Graph) containingClass(targetID string) string {
	n, ok := g.nodes[targetID]
	if !ok {
		return ""
	}
	if n.Kind == graph.KindClass {
		return targetID
	}
	for parent, children := range g.downCnt {
		for _, c := range children {
			if c == targetID {
				if p, ok := g.nodes[parent]; ok && p.Kind == graph.KindClass {
					return parent
				}
				return g.containingClass(parent)
			}
		}
	}
	return ""
}

// sharedFirst returns the name of the first base-class node ID that appears in
// EVERY test class's extends closure (a base shared by all), or "".
func sharedFirst(baseSets [][]string, g *Graph) string {
	if len(baseSets) == 0 {
		return ""
	}
	for _, candidate := range baseSets[0] {
		inAll := true
		for _, set := range baseSets[1:] {
			found := false
			for _, id := range set {
				if id == candidate {
					found = true
					break
				}
			}
			if !found {
				inAll = false
				break
			}
		}
		if inAll {
			if n, ok := g.nodes[candidate]; ok {
				return n.Name
			}
			return candidate
		}
	}
	return ""
}
