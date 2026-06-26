package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// NodeRef is a compact pointer to a graph node in traits output.
// Explicit JSON tags: this shape is the get_traits tool contract.
type NodeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

func refOf(n graph.Node) NodeRef {
	return NodeRef{ID: n.ID, Name: n.Name, Path: n.Path, Line: n.Line}
}

// TypeTrait is one interface in the by-type view: the trait itself, how the
// type acquired it, where each method's logic lives for this type, and who
// else shares it.
type TypeTrait struct {
	Interface         NodeRef   `json:"interface"`
	Via               string    `json:"via,omitempty"` // ancestor/super-interface the trait is inherited through; "" = direct
	Methods           []NodeRef `json:"methods"`       // per method: the override in the type's chain, or the declaration
	OtherImplementers []NodeRef `json:"other_implementers,omitempty"`
}

// Implementer is one implementing type in the by-interface view.
type Implementer struct {
	NodeRef
	Via string `json:"via,omitempty"` // direct implementer this subtype inherits from; "" = direct
}

// InterfaceTraits is the by-interface view: implementers, method definition
// sites, and what reaches the interface (blast radius minus the implementers).
type InterfaceTraits struct {
	Implementers []Implementer `json:"implementers"`
	Methods      []NodeRef     `json:"methods"`
	Impact       []NodeRef     `json:"impact"`
}

// TraitsResult bundles both directions of the trait view. AsType is empty for
// pure interfaces; AsInterface is nil for types nothing implements. Both can
// be present (e.g. an abstract class).
type TraitsResult struct {
	Target      NodeRef          `json:"target"`
	AsType      []TypeTrait      `json:"as_type,omitempty"`
	AsInterface *InterfaceTraits `json:"as_interface,omitempty"`
}

// Traits assembles the trait-centric view of target from implements/extends/
// contains edges. depth and maxImpact control the impact BFS in the
// by-interface section (same semantics as Impact).
func (g *Graph) Traits(target string, depth, maxImpact int) (*TraitsResult, error) {
	n, ok := g.nodes[target]
	if !ok {
		return nil, fmt.Errorf("query.Graph.Traits: unknown node %q", target)
	}
	res := &TraitsResult{
		Target: refOf(n),
		AsType: g.traitsByType(target),
	}
	for _, e := range g.rev[target] {
		if e.Kind == graph.EdgeImplements {
			ai, err := g.traitsByInterface(target, depth, maxImpact)
			if err != nil {
				return nil, fmt.Errorf("query.Graph.Traits: %w", err)
			}
			res.AsInterface = ai
			break
		}
	}
	return res, nil
}

// extendsClosure returns the IDs reachable from id over forward extends edges
// in BFS order, excluding id itself. Cycle-safe.
func (g *Graph) extendsClosure(id string) []string {
	visited := map[string]bool{id: true}
	frontier := []string{id}
	var out []string
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, e := range g.fwd[cur] {
				if e.Kind != graph.EdgeExtends || visited[e.To] {
					continue
				}
				visited[e.To] = true
				out = append(out, e.To)
				next = append(next, e.To)
			}
		}
		frontier = next
	}
	return out
}

// ownMethods returns the function nodes directly contained in ownerID,
// sorted by ID for determinism.
func (g *Graph) ownMethods(ownerID string) []graph.Node {
	ids := append([]string(nil), g.downCnt[ownerID]...)
	sort.Strings(ids)
	out := make([]graph.Node, 0, len(ids))
	for _, id := range ids {
		if n, ok := g.nodes[id]; ok && n.Kind == graph.KindFunction {
			out = append(out, n)
		}
	}
	return out
}

// simpleName returns the part after the last dot: "Order.refund" -> "refund".
func simpleName(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// methodFor returns the function node defining method (by simple name) for a
// type chain — the type's own override or the nearest ancestor's — falling
// back to the interface declaration decl when no override exists.
func (g *Graph) methodFor(chain []string, method string, decl graph.Node) NodeRef {
	for _, tid := range chain {
		for _, m := range g.ownMethods(tid) {
			if simpleName(m.Name) == method {
				return refOf(m)
			}
		}
	}
	return refOf(decl)
}

// traitsByType assembles every interface id implements: direct implements
// edges, then those declared by ancestors in its extends chain, then the
// extends closure of each collected interface (super-interfaces). The first
// route found (BFS from the type) is the one reported in Via.
func (g *Graph) traitsByType(id string) []TypeTrait {
	chain := append([]string{id}, g.extendsClosure(id)...)
	inChain := map[string]bool{}
	for _, c := range chain {
		inChain[c] = true
	}

	type cand struct{ iface, via string }
	var queue []cand
	seen := map[string]bool{}
	for i, tid := range chain {
		via := ""
		if i > 0 {
			if tn, ok := g.nodes[tid]; ok {
				via = tn.Name
			}
		}
		for _, e := range g.fwd[tid] {
			if e.Kind == graph.EdgeImplements && !seen[e.To] {
				seen[e.To] = true
				queue = append(queue, cand{e.To, via})
			}
		}
	}
	for i := 0; i < len(queue); i++ {
		ifn, ok := g.nodes[queue[i].iface]
		if !ok {
			continue
		}
		for _, super := range g.extendsClosure(queue[i].iface) {
			if !seen[super] {
				seen[super] = true
				queue = append(queue, cand{super, ifn.Name})
			}
		}
	}

	out := make([]TypeTrait, 0, len(queue))
	for _, c := range queue {
		ifn, ok := g.nodes[c.iface]
		if !ok {
			continue
		}
		tt := TypeTrait{Interface: refOf(ifn), Via: c.via}
		for _, decl := range g.ownMethods(c.iface) {
			tt.Methods = append(tt.Methods, g.methodFor(chain, simpleName(decl.Name), decl))
		}
		implSeen := map[string]bool{}
		for _, e := range g.rev[c.iface] {
			if e.Kind != graph.EdgeImplements || inChain[e.From] || implSeen[e.From] {
				continue
			}
			implSeen[e.From] = true
			if n, ok := g.nodes[e.From]; ok {
				tt.OtherImplementers = append(tt.OtherImplementers, refOf(n))
			}
		}
		sort.Slice(tt.OtherImplementers, func(a, b int) bool {
			return tt.OtherImplementers[a].Name < tt.OtherImplementers[b].Name
		})
		out = append(out, tt)
	}
	return out
}

// containsClosure returns all contains descendants of id (methods, nested
// types). Used to exclude an implementer's own members from the interface's
// impact set — they belong to the implementer, not to the blast radius.
func (g *Graph) containsClosure(id string) []string {
	visited := map[string]bool{id: true}
	frontier := []string{id}
	var out []string
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, c := range g.downCnt[cur] {
				if visited[c] {
					continue
				}
				visited[c] = true
				out = append(out, c)
				next = append(next, c)
			}
		}
		frontier = next
	}
	return out
}

// revExtendsClosure returns the IDs reachable from id over reverse extends
// edges (its subtypes) in BFS order, excluding id. Cycle-safe.
func (g *Graph) revExtendsClosure(id string) []string {
	visited := map[string]bool{id: true}
	frontier := []string{id}
	var out []string
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, e := range g.rev[cur] {
				if e.Kind != graph.EdgeExtends || visited[e.From] {
					continue
				}
				visited[e.From] = true
				out = append(out, e.From)
				next = append(next, e.From)
			}
		}
		frontier = next
	}
	return out
}

// traitsByInterface assembles the by-interface view: direct implementers,
// their subtypes (inherited implementers), method declaration sites across
// the interface's extends closure, and the impact set — everything that
// reaches the interface or its methods, minus the implementers, their
// contained members, and the seeds themselves.
func (g *Graph) traitsByInterface(id string, depth, maxImpact int) (*InterfaceTraits, error) {
	res := &InterfaceTraits{}
	seen := map[string]bool{}
	var direct []string
	for _, e := range g.rev[id] {
		if e.Kind != graph.EdgeImplements || seen[e.From] {
			continue
		}
		seen[e.From] = true
		direct = append(direct, e.From)
		if n, ok := g.nodes[e.From]; ok {
			res.Implementers = append(res.Implementers, Implementer{NodeRef: refOf(n)})
		}
	}
	for _, d := range direct {
		dn, ok := g.nodes[d]
		if !ok {
			continue
		}
		for _, sub := range g.revExtendsClosure(d) {
			if seen[sub] {
				continue
			}
			seen[sub] = true
			if n, ok := g.nodes[sub]; ok {
				res.Implementers = append(res.Implementers, Implementer{NodeRef: refOf(n), Via: dn.Name})
			}
		}
	}
	sort.Slice(res.Implementers, func(a, b int) bool {
		return res.Implementers[a].Name < res.Implementers[b].Name
	})

	for _, owner := range append([]string{id}, g.extendsClosure(id)...) {
		for _, m := range g.ownMethods(owner) {
			res.Methods = append(res.Methods, refOf(m))
		}
	}

	// calls edges land on method nodes, so seed the interface AND its methods.
	seeds := []string{id}
	excluded := map[string]bool{id: true}
	for _, m := range g.ownMethods(id) {
		seeds = append(seeds, m.ID)
		excluded[m.ID] = true
	}
	for s := range seen {
		excluded[s] = true
		// Exclude each implementer's own members so they don't show up as
		// false-positive callers/dependents in the trait impact set.
		for _, c := range g.containsClosure(s) {
			excluded[c] = true
		}
	}
	for _, seed := range seeds {
		r, err := g.Impact(seed, depth, DirUp, maxImpact)
		if err != nil {
			return nil, fmt.Errorf("query.Graph.traitsByInterface: impact of %s: %w", seed, err)
		}
		for _, h := range r.Hits {
			if excluded[h.Node.ID] {
				continue
			}
			excluded[h.Node.ID] = true
			res.Impact = append(res.Impact, refOf(h.Node))
		}
	}
	sort.Slice(res.Impact, func(a, b int) bool { return res.Impact[a].ID < res.Impact[b].ID })
	if maxImpact > 0 && len(res.Impact) > maxImpact {
		res.Impact = res.Impact[:maxImpact]
	}
	return res, nil
}

// refPointer renders "Name (path:line)" or "Name (path)" when line is unknown.
func refPointer(r NodeRef) string {
	if r.Line > 0 {
		return fmt.Sprintf("%s (%s:%d)", r.Name, r.Path, r.Line)
	}
	return fmt.Sprintf("%s (%s)", r.Name, r.Path)
}

// FormatTraits renders the trait view as pointer-only markdown. No source
// code is included — same token-budget posture as FormatImpact/FormatContext.
func FormatTraits(res *TraitsResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Traits of `%s`\n", res.Target.Name)
	if len(res.AsType) == 0 && res.AsInterface == nil {
		b.WriteString("\nNo implements/extends relations found.\n")
		return b.String()
	}
	for _, tt := range res.AsType {
		if tt.Via != "" {
			fmt.Fprintf(&b, "\n## implements %s (via %s) — %s\n", tt.Interface.Name, tt.Via, tt.Interface.Path)
		} else {
			fmt.Fprintf(&b, "\n## implements %s — %s\n", tt.Interface.Name, tt.Interface.Path)
		}
		if len(tt.Methods) > 0 {
			b.WriteString("Logic lives in:\n")
			for _, m := range tt.Methods {
				fmt.Fprintf(&b, "- %s\n", refPointer(m))
			}
		}
		if len(tt.OtherImplementers) > 0 {
			b.WriteString("Other implementers:\n")
			for _, o := range tt.OtherImplementers {
				fmt.Fprintf(&b, "- %s\n", refPointer(o))
			}
		}
	}
	if ai := res.AsInterface; ai != nil {
		b.WriteString("\n## As interface\n")
		b.WriteString("Implementers:\n")
		for _, im := range ai.Implementers {
			if im.Via != "" {
				fmt.Fprintf(&b, "- %s — via %s\n", refPointer(im.NodeRef), im.Via)
			} else {
				fmt.Fprintf(&b, "- %s\n", refPointer(im.NodeRef))
			}
		}
		b.WriteString("Methods defined at:\n")
		for _, m := range ai.Methods {
			fmt.Fprintf(&b, "- %s\n", refPointer(m))
		}
		b.WriteString("Impact (what reaches this interface):\n")
		if len(ai.Impact) == 0 {
			b.WriteString("- (none)\n")
		} else {
			for _, n := range ai.Impact {
				fmt.Fprintf(&b, "- %s\n", refPointer(n))
			}
		}
	}
	return b.String()
}
