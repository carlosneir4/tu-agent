package query

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// FlowHop is one callee node in the execution-flow tree.
// DispatchCandidates is set (and Callees left nil) when the callee is an
// interface method — the engine cannot pick a static target, so it lists the
// concrete candidates instead of recursing.
type FlowHop struct {
	Node               NodeRef   `json:"node"`
	CrossesBoundary    bool      `json:"crosses_boundary,omitempty"`
	DispatchCandidates []NodeRef `json:"dispatch_candidates,omitempty"`
	Callees            []FlowHop `json:"callees,omitempty"`
}

// FlowResult is the call tree rooted at Entry.
type FlowResult struct {
	Entry     NodeRef   `json:"entry"`
	Depth     int       `json:"depth"`
	FanOutCap int       `json:"fan_out_cap"`
	Truncated bool      `json:"truncated,omitempty"`
	Callees   []FlowHop `json:"callees"`
}

// Flow builds the call tree rooted at entry up to depth hops over calls edges.
// fanOutCap limits direct unvisited callees per node (0 = unlimited). A visited
// set prevents cycles; a node reachable via two paths is shown only on the first.
func (g *Graph) Flow(entry string, depth, fanOutCap int) (*FlowResult, error) {
	n, ok := g.nodes[entry]
	if !ok {
		return nil, fmt.Errorf("query.Graph.Flow: unknown node %q", entry)
	}
	childToParent := make(map[string]string, len(g.nodes))
	for parent, children := range g.downCnt {
		for _, child := range children {
			childToParent[child] = parent
		}
	}
	visited := map[string]bool{entry: true}
	var truncated bool
	callees := g.flowCallees(n, entry, depth, fanOutCap, visited, childToParent, &truncated)
	return &FlowResult{
		Entry:     refOf(n),
		Depth:     depth,
		FanOutCap: fanOutCap,
		Truncated: truncated,
		Callees:   callees,
	}, nil
}

func (g *Graph) flowCallees(
	callerNode graph.Node,
	id string,
	depth, fanOutCap int,
	visited map[string]bool,
	childToParent map[string]string,
	truncated *bool,
) []FlowHop {
	if depth < 1 {
		return nil
	}
	var hops []FlowHop
	for _, callee := range g.Callees(id) {
		if visited[callee.ID] {
			continue
		}
		if fanOutCap > 0 && len(hops) >= fanOutCap {
			*truncated = true
			break
		}
		visited[callee.ID] = true
		hop := FlowHop{
			Node:            refOf(callee),
			CrossesBoundary: filepath.Dir(callerNode.Path) != filepath.Dir(callee.Path),
		}
		if parentID := childToParent[callee.ID]; parentID != "" && g.isInterfaceNode(parentID) {
			hop.DispatchCandidates = g.concreteImpls(parentID, callee.Name)
		} else {
			hop.Callees = g.flowCallees(callee, callee.ID, depth-1, fanOutCap, visited, childToParent, truncated)
		}
		hops = append(hops, hop)
	}
	return hops
}

// isInterfaceNode reports whether id is an interface (i.e. some concrete type
// has an implements edge pointing to it).
func (g *Graph) isInterfaceNode(id string) bool {
	for _, e := range g.rev[id] {
		if e.Kind == graph.EdgeImplements {
			return true
		}
	}
	return false
}

// concreteImpls returns one NodeRef per direct implementer of ifaceID that
// overrides methodName (matched by simple name).
func (g *Graph) concreteImpls(ifaceID, methodName string) []NodeRef {
	simple := simpleName(methodName)
	seen := map[string]bool{}
	var out []NodeRef
	for _, e := range g.rev[ifaceID] {
		if e.Kind != graph.EdgeImplements || seen[e.From] {
			continue
		}
		seen[e.From] = true
		for _, m := range g.ownMethods(e.From) {
			if simpleName(m.Name) == simple {
				out = append(out, refOf(m))
				break
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// FormatFlow renders the flow tree as a pointer-only indented call tree.
func FormatFlow(res *FlowResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Execution flow from `%s`\n", res.Entry.Name)
	fmt.Fprintf(&b, "(depth %d, fan-out cap %d)\n", res.Depth, res.FanOutCap)
	if res.Truncated {
		b.WriteString("(results truncated — fan-out cap reached at one or more nodes)\n")
	}
	if len(res.Callees) == 0 {
		b.WriteString("\nNo outgoing calls found.\n")
		return b.String()
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s\n", refPointer(res.Entry))
	writeFlowHops(&b, res.Callees, 1)
	return b.String()
}

func writeFlowHops(b *strings.Builder, hops []FlowHop, indent int) {
	prefix := strings.Repeat("  ", indent)
	for _, h := range hops {
		var flags []string
		if h.CrossesBoundary {
			flags = append(flags, "boundary")
		}
		if len(h.DispatchCandidates) > 0 {
			flags = append(flags, "interface dispatch")
		}
		annotation := ""
		if len(flags) > 0 {
			annotation = " [" + strings.Join(flags, ", ") + "]"
		}
		fmt.Fprintf(b, "%s↳ %s%s\n", prefix, refPointer(h.Node), annotation)
		for _, c := range h.DispatchCandidates {
			fmt.Fprintf(b, "%s    ? %s\n", prefix, refPointer(c))
		}
		writeFlowHops(b, h.Callees, indent+1)
	}
}

// FormatFlowMermaid renders the flow tree as a Mermaid flowchart diagram.
// Node IDs are sequential integers (N0, N1, ...) in DFS order for stability.
func FormatFlowMermaid(res *FlowResult) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	var counter int
	entryID := mermaidNodeID(&counter)
	fmt.Fprintf(&b, "    %s[\"%s\"]\n", entryID, mermaidLabel(refPointer(res.Entry)))
	writeMermaidHops(&b, entryID, res.Callees, &counter)
	return b.String()
}

func mermaidNodeID(counter *int) string {
	id := fmt.Sprintf("N%d", *counter)
	*counter++
	return id
}

func mermaidLabel(s string) string {
	return strings.ReplaceAll(s, `"`, `'`)
}

func writeMermaidHops(b *strings.Builder, parentID string, hops []FlowHop, counter *int) {
	for _, h := range hops {
		nodeID := mermaidNodeID(counter)
		edgeLabel := ""
		if h.CrossesBoundary {
			edgeLabel = "|\"boundary\"|"
		}
		fmt.Fprintf(b, "    %s -->%s %s[\"%s\"]\n",
			parentID, edgeLabel, nodeID, mermaidLabel(refPointer(h.Node)))
		for _, c := range h.DispatchCandidates {
			candID := mermaidNodeID(counter)
			fmt.Fprintf(b, "    %s -.->|\"dispatch\"| %s[\"%s\"]\n",
				nodeID, candID, mermaidLabel(refPointer(c)))
		}
		writeMermaidHops(b, nodeID, h.Callees, counter)
	}
}
