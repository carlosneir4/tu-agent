// Package query provides read-only views over the knowledge graph: impact BFS,
// symbol find, composite context assembly, and token-budgeted pointer output.
package query

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// ImpactDir controls the traversal direction in Impact BFS.
type ImpactDir int

const (
	DirUp   ImpactDir = iota // find nodes that depend on (use) the target
	DirDown                  // find nodes the target depends on
)

// Hit is one node reached during Impact BFS.
type Hit struct {
	Node       graph.Node
	Via        graph.EdgeKind   // edge kind traversed to reach this node
	Confidence graph.Confidence // confidence of that edge
	Subsystem  string           // bucket for aggregation (package or dir); set by Impact
}

// Graph is an in-memory index built from a flat node and edge list.
type Graph struct {
	nodes       map[string]graph.Node
	byName      map[string][]string
	fwd         map[string][]graph.Edge    // non-contains/-documents edges, From->[]Edge
	rev         map[string][]graph.Edge    // non-contains/-documents edges, To->[]Edge
	downCnt     map[string][]string        // contains: parent -> []child IDs
	docByFile   map[string][]string        // documents: file path -> []skill node IDs
	conventions []graph.Node               // all convention nodes, sorted by ID
	pkgByPath   map[string]string          // repo-relative path -> package (from FileMeta); optional
	bridgeCache map[int]map[string]float64 // memoized BridgeScores keyed by sample count
	sccCache    [][]string                 // memoized StronglyConnectedComponents result; nil until first call
	sccComputed bool                       // true once sccCache is populated (cache empty results too)
}

// GraphOption configures a Graph at construction.
type GraphOption func(*Graph)

// WithPackages supplies a repo-relative-path -> package map (from the store's
// FileMeta) so query-time domain assignment can use real package names.
func WithPackages(pkgByPath map[string]string) GraphOption {
	return func(g *Graph) { g.pkgByPath = pkgByPath }
}

// SubsystemOf buckets a node for aggregation: its package when known (from the
// store's FileMeta via WithPackages), else the file's directory, else "(root)".
func SubsystemOf(g *Graph, n graph.Node) string {
	if pkg := g.pkgByPath[n.Path]; pkg != "" {
		return pkg
	}
	if d := path.Dir(n.Path); d != "" && d != "." {
		return d
	}
	return "(root)"
}

// subsystemBreakdown renders "name count, name count …" over all hits, sorted by
// count descending then name ascending. Shared by FormatImpact and FormatContext;
// always aggregates the full set regardless of any display cap.
func subsystemBreakdown(hits []Hit) string {
	counts := map[string]int{}
	for _, h := range hits {
		counts[h.Subsystem]++
	}
	type sub struct {
		name string
		n    int
	}
	subs := make([]sub, 0, len(counts))
	for name, n := range counts {
		subs = append(subs, sub{name, n})
	}
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].n != subs[j].n {
			return subs[i].n > subs[j].n
		}
		return subs[i].name < subs[j].name
	})
	parts := make([]string, 0, len(subs))
	for _, s := range subs {
		parts = append(parts, fmt.Sprintf("%s %d", s.name, s.n))
	}
	return strings.Join(parts, ", ")
}

// NewGraph builds a Graph from a slice of nodes and edges.
func NewGraph(nodes []graph.Node, edges []graph.Edge, opts ...GraphOption) *Graph {
	g := &Graph{
		nodes:     make(map[string]graph.Node, len(nodes)),
		byName:    make(map[string][]string),
		fwd:       make(map[string][]graph.Edge),
		rev:       make(map[string][]graph.Edge),
		downCnt:   make(map[string][]string),
		docByFile: make(map[string][]string),
		pkgByPath: make(map[string]string),
	}
	for _, n := range nodes {
		g.nodes[n.ID] = n
		g.byName[n.Name] = append(g.byName[n.Name], n.ID)
		if n.Kind == graph.KindConvention {
			g.conventions = append(g.conventions, n)
		}
	}
	sort.Slice(g.conventions, func(i, j int) bool { return g.conventions[i].ID < g.conventions[j].ID })
	for _, e := range edges {
		switch e.Kind {
		case graph.EdgeContains:
			g.downCnt[e.From] = append(g.downCnt[e.From], e.To)
		case graph.EdgeDocuments:
			g.docByFile[e.To] = append(g.docByFile[e.To], e.From)
		default:
			g.fwd[e.From] = append(g.fwd[e.From], e)
			g.rev[e.To] = append(g.rev[e.To], e)
		}
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// ImpactResult is the set of nodes affected by a change in a source node.
type ImpactResult struct {
	Hits            []Hit            // affected nodes, in BFS order
	SurprisingEdges []SurprisingEdge // rare cross-domain dependencies within the blast radius
	Truncated       bool             // true when maxResults capped the traversal (more reachable nodes exist)
}

// Contains reports whether nodeID is in the result set.
func (r *ImpactResult) Contains(nodeID string) bool {
	if r == nil {
		return false
	}
	for _, h := range r.Hits {
		if h.Node.ID == nodeID {
			return true
		}
	}
	return false
}

// NodeIDs returns all affected node IDs, sorted.
func (r *ImpactResult) NodeIDs() []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.Node.ID)
	}
	sort.Strings(out)
	return out
}

// maxDependents is the hard safety cap used when the caller passes maxResults=0
// (unlimited). It prevents runaway BFS on extremely high-fan-in symbols.
const maxDependents = 5000

// Impact returns nodes transitively affected by a change to target, up to
// depth hops. maxResults caps the number of traversed dependents (DirUp) or
// dependencies (DirDown) (0 = use safety cap of 5000).
// DirUp traverses reverse edges (what uses target); DirDown traverses forward
// edges (what target uses). documents edges are excluded — skills are not code dependents.
// Only real dependency nodes are returned; contained members are not expanded.
// Each Hit carries its Subsystem for aggregated display.
func (g *Graph) Impact(target string, depth int, dir ImpactDir, maxResults int) (*ImpactResult, error) {
	result := &ImpactResult{}
	if depth < 1 {
		return result, nil
	}
	visited := map[string]bool{target: true}
	frontier := []string{target}

	// Bridge symbol→file granularity: file-level edges (imports) are keyed by
	// the containing file node, so a class/function query must also seed its
	// file to reach them. The file node ID equals its path.
	if n, ok := g.nodes[target]; ok && n.Path != "" && n.Path != target {
		if _, ok := g.nodes[n.Path]; ok && !visited[n.Path] {
			visited[n.Path] = true
			frontier = append(frontier, n.Path)
		}
	}

	dependents := 0
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, id := range frontier {
			var candidates []graph.Edge
			if dir == DirUp {
				candidates = g.rev[id]
			} else {
				candidates = g.fwd[id]
			}
			for _, e := range candidates {
				peer := e.From
				if dir == DirDown {
					peer = e.To
				}
				if visited[peer] {
					continue
				}
				n, ok := g.nodes[peer]
				if !ok {
					// Mark visited so we don't revisit; node absent from graph.
					visited[peer] = true
					continue
				}
				limit := maxResults
				if limit <= 0 {
					limit = maxDependents
				}
				if dependents >= limit {
					result.Truncated = true
					return result, nil
				}
				visited[peer] = true
				result.Hits = append(result.Hits, Hit{
					Node: n, Via: e.Kind, Confidence: e.Confidence, Subsystem: SubsystemOf(g, n),
				})
				next = append(next, peer)
				dependents++
			}
		}
		frontier = next
	}
	return result, nil
}

// Find returns all nodes whose Name contains q (case-insensitive).
func (g *Graph) Find(q string) []graph.Node {
	lower := strings.ToLower(q)
	var out []graph.Node
	for _, n := range g.nodes {
		if strings.Contains(strings.ToLower(n.Name), lower) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// NodeByID returns a node by its ID, and whether it was found.
func (g *Graph) NodeByID(id string) (graph.Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// FormatFind renders a pointer-only markdown list of Find results.
func FormatFind(q string, nodes []graph.Node) string {
	if len(nodes) == 0 {
		return fmt.Sprintf("No symbols match `%s`.\n", q)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Symbols matching `%s`\n\n", q))
	for _, n := range nodes {
		if n.Line > 0 {
			sb.WriteString(fmt.Sprintf("- `%s` (%s, %s:%d)\n", n.Name, n.Kind, n.Path, n.Line))
		} else {
			sb.WriteString(fmt.Sprintf("- `%s` (%s, %s)\n", n.Name, n.Kind, n.Path))
		}
	}
	return sb.String()
}

// FormatImpact renders a pointer-only markdown summary of the impact result.
// displayCap limits how many dependents are shown in the file-grouped section
// (0 = show all). The true total is always in the header. A "By subsystem"
// line aggregates over all hits. No source code is included — callers use this
// as a token budget-safe context block.
func FormatImpact(sourceID string, result *ImpactResult, displayCap int) string {
	total := len(result.Hits)
	if total == 0 {
		return fmt.Sprintf("No dependents affected by changes to `%s`.\n", sourceID)
	}

	shown := result.Hits
	if displayCap > 0 && len(shown) > displayCap {
		shown = shown[:displayCap]
	}
	byFile := map[string][]Hit{}
	var files []string
	for _, h := range shown {
		if _, seen := byFile[h.Node.Path]; !seen {
			files = append(files, h.Node.Path)
		}
		byFile[h.Node.Path] = append(byFile[h.Node.Path], h)
	}
	allFiles := map[string]bool{}
	for _, h := range result.Hits {
		allFiles[h.Node.Path] = true
	}
	fileCount := len(allFiles)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Impact of `%s`\n\n", sourceID)
	fmt.Fprintf(&sb, "%d dependent(s) across %d file(s):\n\n", total, fileCount)
	for _, f := range files {
		fmt.Fprintf(&sb, "**%s**\n", f)
		for _, h := range byFile[f] {
			if h.Node.Line > 0 {
				fmt.Fprintf(&sb, "  - `%s` (line %d) via %s\n", h.Node.Name, h.Node.Line, h.Via)
			} else {
				fmt.Fprintf(&sb, "  - `%s` via %s\n", h.Node.Name, h.Via)
			}
		}
		sb.WriteString("\n")
	}
	if displayCap > 0 && total > displayCap {
		fmt.Fprintf(&sb, "_(showing %d of %d dependents; +%d more — raise max_results)_\n", displayCap, total, total-displayCap)
	}
	fmt.Fprintf(&sb, "\nBy subsystem: %s\n", subsystemBreakdown(result.Hits))
	if result.Truncated {
		fmt.Fprintf(&sb, "_(traversal capped at %d; narrow depth/scope to refine)_\n", maxDependents)
	}
	if len(result.SurprisingEdges) > 0 {
		sb.WriteString("\n")
		sb.WriteString(formatSurprisingSection(result.SurprisingEdges, displayCap))
	}
	return sb.String()
}

// ContextResult is the composite "everything relevant to touching X" package.
type ContextResult struct {
	Target         string
	TargetNode     graph.Node    // the node for Target, if found in the graph
	Impact         *ImpactResult // dependents (blast radius)
	Skills         []graph.Node  // skills documenting the target or any affected file
	Conventions    []graph.Node  // global convention nodes
	Tests          []graph.Node  // tests covering the target or affected classes
	BridgeScore    float64       // approximate betweenness of the target over the call graph
	IsChokepoint   bool          // target ranks in the top-percentile of bridge scores
	CyclicCore     []string      // co-members when the target is in a cyclic core (capped); empty otherwise
	CyclicCoreSize int           // full core size, for "(+N more)"
}

// Context answers "I'm about to work on X — give me everything relevant" in one
// call: blast radius (dependents) + skills documenting affected files + global
// conventions + tests to run. Pointers only, never source.
func (g *Graph) Context(target string, depth int) (*ContextResult, error) {
	imp, err := g.Impact(target, depth, DirUp, 0) // 0 = collect all (safety default)
	if err != nil {
		return nil, fmt.Errorf("query.Graph.Context: %w", err)
	}
	// Affected file IDs = the target's file + every hit's file.
	affected := map[string]bool{}
	if n, ok := g.nodes[target]; ok {
		affected[n.Path] = true
	}
	for _, h := range imp.Hits {
		affected[h.Node.Path] = true
	}
	skillSeen := map[string]bool{}
	var skills []graph.Node
	for fileID := range affected {
		for _, sid := range g.docByFile[fileID] {
			if n, ok := g.nodes[sid]; ok && !skillSeen[sid] {
				skillSeen[sid] = true
				skills = append(skills, n)
			}
		}
	}
	sortNodesByID(skills)

	testSeen := map[string]bool{}
	var tests []graph.Node
	collect := func(id string) {
		for _, e := range g.fwd[id] {
			if e.Kind == graph.EdgeTestedBy {
				if n, ok := g.nodes[e.To]; ok && !testSeen[e.To] {
					testSeen[e.To] = true
					tests = append(tests, n)
				}
			}
		}
	}
	collect(target)
	for _, h := range imp.Hits {
		collect(h.Node.ID)
	}
	sortNodesByID(tests)

	var targetNode graph.Node
	if n, ok := g.nodes[target]; ok {
		targetNode = n
	}
	var bridgeScore float64
	var isChoke bool
	if targetNode.ID != "" {
		bridgeScore, isChoke = g.IsChokepoint(targetNode.ID, BridgeConfig{})
	}
	var cyclicCore []string
	var cyclicCoreSize int
	if core, ok := g.CyclicCoreOf(target); ok {
		cyclicCoreSize = len(core)
		if len(core) > 15 {
			core = core[:15]
		}
		cyclicCore = core
	}
	return &ContextResult{
		Target: target, TargetNode: targetNode, Impact: imp, Skills: skills,
		Conventions: g.conventions, Tests: tests,
		BridgeScore: bridgeScore, IsChokepoint: isChoke,
		CyclicCore: cyclicCore, CyclicCoreSize: cyclicCoreSize,
	}, nil
}

func sortNodesByID(ns []graph.Node) {
	sort.Slice(ns, func(i, j int) bool { return ns[i].ID < ns[j].ID })
}

// signatureName renders "name(params) returnType" for function nodes and
// the bare name for everything else.
func signatureName(n graph.Node) string {
	name := n.Name
	if n.Params != "" {
		name += n.Params
		if n.ReturnType != "" {
			name += " " + n.ReturnType
		}
	}
	return name
}

// nodePointer renders one node as a compact "path:line-range (kind name)".
// Function nodes append their signature: "name(params) returnType".
func nodePointer(n graph.Node) string {
	loc := n.Path
	if n.Line > 0 {
		loc = fmt.Sprintf("%s:%d-%d", n.Path, n.Line, n.EndLine)
	}
	return fmt.Sprintf("%s (%s %s)", loc, n.Kind, signatureName(n))
}

func writeNodeList(b *strings.Builder, ns []graph.Node) {
	if len(ns) == 0 {
		b.WriteString("- (none)\n")
		return
	}
	for _, n := range ns {
		fmt.Fprintf(b, "- %s\n", nodePointer(n))
	}
}

// FormatContext renders the context package as compact pointer-only markdown.
// displayCap limits the number of individual dependent lines shown (0 = show all).
func FormatContext(res *ContextResult, displayCap int) string {
	var b strings.Builder
	header := res.Target
	if res.TargetNode.ID != "" {
		header = nodePointer(res.TargetNode)
	}
	fmt.Fprintf(&b, "Context for %s\n", header)
	if res.IsChokepoint {
		fmt.Fprintf(&b, "\n⚠ Chokepoint (bridge node): lies on ~%.0f shortest paths between other methods (sampled).\n   Risk: changes here have amplified blast-radius beyond direct callers.\n", res.BridgeScore)
	}
	if len(res.CyclicCore) > 0 {
		more := ""
		if res.CyclicCoreSize > len(res.CyclicCore) {
			more = fmt.Sprintf(" (+%d more)", res.CyclicCoreSize-len(res.CyclicCore))
		}
		fmt.Fprintf(&b, "\n⚠ Cyclic core: in a strongly-connected group of %d (co-coupled — a change ripples across all). Members: %s%s. Cut the cycle to bound impact.\n",
			res.CyclicCoreSize, strings.Join(res.CyclicCore, ", "), more)
	}
	b.WriteString("\n## Blast radius (dependents)\n")
	hits := res.Impact.Hits
	if len(hits) == 0 {
		b.WriteString("- (none)\n")
	} else {
		shown := hits
		if displayCap > 0 && len(shown) > displayCap {
			shown = shown[:displayCap]
		}
		for _, h := range shown {
			fmt.Fprintf(&b, "- %s via %s [%s]\n", nodePointer(h.Node), h.Via, h.Confidence)
		}
		if displayCap > 0 && len(hits) > displayCap {
			fmt.Fprintf(&b, "_(showing %d of %d dependents; +%d more — raise max_results)_\n", displayCap, len(hits), len(hits)-displayCap)
		}
		fmt.Fprintf(&b, "\nBy subsystem: %s\n", subsystemBreakdown(hits))
	}
	b.WriteString("\n## Domain skills\n")
	writeNodeList(&b, res.Skills)
	b.WriteString("\n## Conventions\n")
	writeNodeList(&b, res.Conventions)
	b.WriteString("\n## Tests to run\n")
	writeNodeList(&b, res.Tests)
	return b.String()
}

// Callers returns the distinct nodes that have a calls edge to id, sorted by ID.
func (g *Graph) Callers(id string) []graph.Node {
	seen := map[string]bool{}
	var out []graph.Node
	for _, e := range g.rev[id] {
		if e.Kind != graph.EdgeCalls || seen[e.From] {
			continue
		}
		if n, ok := g.nodes[e.From]; ok {
			seen[e.From] = true
			out = append(out, n)
		}
	}
	sortNodesByID(out)
	return out
}

// Callees returns the distinct nodes id calls, sorted by ID.
func (g *Graph) Callees(id string) []graph.Node {
	seen := map[string]bool{}
	var out []graph.Node
	for _, e := range g.fwd[id] {
		if e.Kind != graph.EdgeCalls || seen[e.To] {
			continue
		}
		if n, ok := g.nodes[e.To]; ok {
			seen[e.To] = true
			out = append(out, n)
		}
	}
	sortNodesByID(out)
	return out
}

// StronglyConnectedComponents returns the SCCs over the dependency edges
// (calls/imports/extends/implements; contains/documents are excluded, matching
// the Impact BFS). A node not in any cycle is its own singleton component.
// The result is memoized: subsequent calls return the cached value without
// re-running Tarjan's algorithm (Graph is immutable after NewGraph).
func (g *Graph) StronglyConnectedComponents() [][]string {
	if g.sccComputed {
		return g.sccCache
	}
	adj := make(map[string][]string, len(g.fwd))
	for from, edges := range g.fwd {
		for _, e := range edges {
			adj[from] = append(adj[from], e.To)
		}
	}
	g.sccCache = graph.StronglyConnectedComponents(adj)
	g.sccComputed = true
	return g.sccCache
}

// CyclicCoreOf returns the strongly-connected component containing id when that
// component has more than one member (a real cycle); otherwise (nil, false).
func (g *Graph) CyclicCoreOf(id string) ([]string, bool) {
	for _, comp := range g.StronglyConnectedComponents() {
		if len(comp) < 2 {
			continue
		}
		for _, m := range comp {
			if m == id {
				return comp, true
			}
		}
	}
	return nil, false
}

// TSFunctionFiles returns the distinct repo-relative paths of TypeScript
// function/test nodes, for per-package coverage generation. When domain is
// non-empty, only paths belonging to that domain skill are included (same
// semantics as domainFiles). An empty domain returns all TS function files.
func (g *Graph) TSFunctionFiles(domain string) ([]string, error) {
	allowed, err := g.domainFiles(domain)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range g.nodes {
		if n.Language == "typescript" && (n.Kind == graph.KindFunction || n.Kind == graph.KindTest) && n.Path != "" {
			if allowed != nil && !allowed[n.Path] {
				continue
			}
			if !seen[n.Path] {
				seen[n.Path] = true
				out = append(out, n.Path)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// SkillsFor returns the skill nodes documenting a file path, sorted by ID.
func (g *Graph) SkillsFor(path string) []graph.Node {
	var out []graph.Node
	for _, sid := range g.docByFile[path] {
		if n, ok := g.nodes[sid]; ok {
			out = append(out, n)
		}
	}
	sortNodesByID(out)
	return out
}
