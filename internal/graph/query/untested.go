package query

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// SymbolCoverer reports the covered fraction of a symbol's span. Implemented by
// *coverage.Profile; declared here so the query package does not import coverage.
type SymbolCoverer interface {
	SymbolCoverage(path string, start, end int) (ratio float64, hasData bool)
}

// trivialMaxSpan is the line-span ceiling at/below which a method that calls
// nothing is treated as a trivial accessor/setter and dropped from gap ranking.
// Kept tight (a 4-line fluent setter: signature + assign + return + brace) so
// the filter is high-precision and does not drop short methods with real logic.
const trivialMaxSpan = 4

// spanComplexityCap is the line span at/above which a method counts as
// maximally complex for ranking; beyond it, extra length adds no weight.
const spanComplexityCap = 60

// complexityFactor maps a method's line span to a (1, 2] multiplier so that,
// among gaps with comparable connectivity, longer (typically branchier)
// methods outrank trivially short ones. It MODULATES the topology score, it
// does not replace it: value × connectivity, not connectivity alone.
func complexityFactor(span int) float64 {
	if span > spanComplexityCap {
		span = spanComplexityCap
	}
	if span < 0 {
		span = 0
	}
	return 1.0 + float64(span)/float64(spanComplexityCap)
}

// hasCallee reports whether id makes any outgoing call edge (to a project or an
// external stub node) — a coarse "has logic" signal for the triviality filter.
func (g *Graph) hasCallee(id string) bool {
	for _, e := range g.fwd[id] {
		if e.Kind == graph.EdgeCalls {
			return true
		}
	}
	return false
}

// GapOptions configures UntestedGaps. Zero values get defaults: MinLines 4, Depth 2.
type GapOptions struct {
	MinLines    int
	Depth       int
	Top         int
	Domain      string
	Criticality map[string]float64
	Coverage    SymbolCoverer // nil → graph-proxy ranking (today's behavior)
}

// Gap is one untested public function and its risk score.
type Gap struct {
	Node        graph.Node
	FanIn       int
	BlastRadius int
	Span        int
	Score       float64
	Covered     float64 // [0,1] in coverage mode with data; -1 when unknown/proxy
}

// UntestedGaps returns exported functions with no test linkage, ranked by
// (fan_in+1) × blast_radius × criticality × complexity descending, ties broken
// by node ID. complexity (complexityFactor) lifts longer/branchier methods over
// trivially-connected short ones — value × connectivity, not connectivity alone.
func (g *Graph) UntestedGaps(opts GapOptions) ([]Gap, error) {
	if opts.MinLines <= 0 {
		opts.MinLines = 4
	}
	if opts.Depth <= 0 {
		opts.Depth = 2
	}

	domainFiles, err := g.domainFiles(opts.Domain)
	if err != nil {
		return nil, err
	}

	parentOf := make(map[string]string, len(g.nodes))
	for parent, children := range g.downCnt {
		for _, c := range children {
			parentOf[c] = parent
		}
	}

	var gaps []Gap
	for _, n := range g.nodes {
		if n.Kind != graph.KindFunction || !n.Exported {
			continue
		}
		if domainFiles != nil && !domainFiles[n.Path] {
			continue
		}
		if g.isTestCode(n, parentOf) {
			continue
		}
		span := n.EndLine - n.Line + 1
		if n.Line == 0 || span < opts.MinLines {
			continue
		}
		// Complexity-aware filter: a short method that calls nothing is a
		// getter/setter/builder passthrough — no branching logic to protect.
		// Excluding it keeps a fluent builder setter (reached by everything, so
		// a huge fan-in×blast score) from topping the ranking by connection
		// count alone. Methods that delegate to a callee (including external
		// stubs) or are longer than trivialMaxSpan keep their place.
		if span <= trivialMaxSpan && !g.hasCallee(n.ID) {
			continue
		}
		fanIn := 0
		callers := map[string]bool{}
		for _, e := range g.rev[n.ID] {
			if e.Kind == graph.EdgeCalls && !callers[e.From] {
				callers[e.From] = true
				fanIn++
			}
		}
		imp, err := g.Impact(n.ID, opts.Depth, DirUp, 0)
		if err != nil {
			return nil, fmt.Errorf("query.Graph.UntestedGaps: %w", err)
		}
		crit := 1.0
		if c, ok := opts.Criticality[n.ID]; ok {
			crit = c
		}

		uncovered := 1.0 // proxy default: full weight (keeps today's score)
		covered := -1.0  // unknown
		if opts.Coverage != nil {
			if ratio, hasData := opts.Coverage.SymbolCoverage(n.Path, n.Line, n.EndLine); hasData {
				if ratio >= 1.0 {
					continue // fully covered → not a gap
				}
				uncovered = 1.0 - ratio
				covered = ratio
			} else if g.isTested(n, parentOf) {
				continue // no coverage data for this symbol → graph proxy gate
			}
		} else if g.isTested(n, parentOf) {
			continue // no coverage requested → graph proxy gate
		}

		gaps = append(gaps, Gap{
			Node: n, FanIn: fanIn, BlastRadius: len(imp.Hits), Span: span, Covered: covered,
			Score: uncovered * float64(fanIn+1) * float64(len(imp.Hits)) * crit * complexityFactor(span),
		})
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Score != gaps[j].Score {
			return gaps[i].Score > gaps[j].Score
		}
		return gaps[i].Node.ID < gaps[j].Node.ID
	})
	if opts.Top > 0 && len(gaps) > opts.Top {
		gaps = gaps[:opts.Top]
	}
	return gaps, nil
}

// domainFiles returns the file set documented by the named domain skill, or
// nil when domain is "". Unknown names error with the available list.
func (g *Graph) domainFiles(domain string) (map[string]bool, error) {
	if domain == "" {
		return nil, nil
	}
	var available []string
	matched := map[string]bool{}
	for _, n := range g.nodes {
		if n.Kind != graph.KindSkill {
			continue
		}
		available = append(available, n.Name)
		if n.Name == domain {
			matched[n.ID] = true
		}
	}
	if len(matched) == 0 {
		sort.Strings(available)
		return nil, fmt.Errorf("query.Graph.UntestedGaps: unknown domain %q (available: %s)",
			domain, strings.Join(available, ", "))
	}
	files := map[string]bool{}
	for fileID, skillIDs := range g.docByFile {
		for _, sid := range skillIDs {
			if matched[sid] {
				files[fileID] = true
			}
		}
	}
	return files, nil
}

// isTested reports whether n is linked to a test.
func (g *Graph) isTested(n graph.Node, parentOf map[string]string) bool {
	for _, e := range g.fwd[n.ID] {
		if e.Kind == graph.EdgeTestedBy {
			return true
		}
	}
	if pid, ok := parentOf[n.ID]; ok {
		for _, e := range g.fwd[pid] {
			if e.Kind == graph.EdgeTestedBy {
				return true
			}
		}
	}
	for _, e := range g.rev[n.ID] {
		if e.Kind != graph.EdgeCalls {
			continue
		}
		if caller, ok := g.nodes[e.From]; ok && g.isTestCode(caller, parentOf) {
			return true
		}
	}
	return false
}

// isTestCode reports whether n is test code.
func (g *Graph) isTestCode(n graph.Node, parentOf map[string]string) bool {
	if n.Kind == graph.KindTest {
		return true
	}
	if p, ok := g.nodes[parentOf[n.ID]]; ok && p.Kind == graph.KindTest {
		return true
	}
	return isTestPath(n.Path)
}

func isTestPath(path string) bool {
	if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_test.py") {
		return true
	}
	base := filepath.Base(path)
	if strings.HasSuffix(path, ".py") && strings.HasPrefix(base, "test_") {
		return true
	}
	if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			return true
		}
		if strings.Contains(filepath.ToSlash(path), "__tests__/") {
			return true
		}
	}
	return false
}

// ClassMethods returns the exported function nodes contained by classID,
// sorted by node ID. It mirrors the private ownMethods helper but keeps only
// exported methods — the batch generator's "all public methods of a class"
// resolution. Empty when classID is unknown or has no exported methods.
func (g *Graph) ClassMethods(classID string) []graph.Node {
	ids := append([]string(nil), g.downCnt[classID]...)
	sort.Strings(ids)
	out := make([]graph.Node, 0, len(ids))
	for _, id := range ids {
		if n, ok := g.nodes[id]; ok && n.Kind == graph.KindFunction && n.Exported {
			out = append(out, n)
		}
	}
	return out
}

// FunctionLanguages returns the language of every function node (for primary-
// language inference).
func (g *Graph) FunctionLanguages() []string {
	out := make([]string, 0, len(g.nodes))
	for _, n := range g.nodes {
		if n.Kind == graph.KindFunction {
			out = append(out, n.Language)
		}
	}
	return out
}

// FormatGaps renders gaps as a ranked markdown table, highest risk first.
func FormatGaps(gaps []Gap) string {
	if len(gaps) == 0 {
		return "No untested public symbols found.\n"
	}
	var b strings.Builder
	b.WriteString("## Untested public symbols (highest risk first)\n\n")
	b.WriteString("| # | symbol | location | fan-in | blast | span | covered | score |\n")
	b.WriteString("|---|--------|----------|--------|-------|------|---------|-------|\n")
	for i, gp := range gaps {
		cov := "—"
		if gp.Covered >= 0 {
			cov = fmt.Sprintf("%.0f%%", gp.Covered*100)
		}
		fmt.Fprintf(&b, "| %d | `%s` | %s:%d | %d | %d | %d | %s | %.0f |\n",
			i+1, signatureName(gp.Node), gp.Node.Path, gp.Node.Line,
			gp.FanIn, gp.BlastRadius, gp.Span, cov, gp.Score)
	}
	return b.String()
}
