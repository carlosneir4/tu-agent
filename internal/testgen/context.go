package testgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
	"github.com/tu/tu-agent/internal/graph/query"
)

// DefaultContextBudget caps the assembled prompt context in bytes (spec: 16 KB).
const DefaultContextBudget = 16 * 1024

// CallSite is one real usage of the target, shown to the model as evidence
// of expected behavior.
type CallSite struct {
	Caller  string `json:"caller"` // "path:line (caller name)"
	Snippet string `json:"snippet"`
}

// GenContext is everything the generation prompt knows about the target.
type GenContext struct {
	Target        Target     `json:"target"`
	PackageClause string     `json:"package_clause"`
	Body          string     `json:"body"`
	CallSites     []CallSite `json:"call_sites,omitempty"`
	Callees       []string   `json:"callees,omitempty"`
	SkillExcerpt  string     `json:"skill_excerpt,omitempty"`
	BlastRadius   int        `json:"blast_radius"`
}

const (
	maxCallSites   = 5
	callSiteMargin = 3
	blastDepth     = 2
	blastMax       = 500
)

// BuildContext assembles the generation context for a resolved target under
// a byte budget. Truncation priority: the body is never truncated -> call
// sites -> callees -> skill excerpt (spec, pipeline step 4).
func BuildContext(g *query.Graph, repoRoot string, t Target, budget int) (*GenContext, error) {
	if budget <= 0 {
		budget = DefaultContextBudget
	}
	srcLines, err := readLines(filepath.Join(repoRoot, t.Path))
	if err != nil {
		return nil, fmt.Errorf("testgen.BuildContext: %w", err)
	}
	gc := &GenContext{Target: t}
	gc.PackageClause = packageClause(srcLines)
	gc.Body = sliceLines(srcLines, t.Line, t.EndLine)
	used := len(gc.Body) + len(gc.PackageClause)

	imp, err := g.Impact(t.NodeID, blastDepth, query.DirUp, blastMax)
	if err != nil {
		return nil, fmt.Errorf("testgen.BuildContext: %w", err)
	}
	gc.BlastRadius = len(imp.Hits)

	symbol := t.Name
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		symbol = symbol[i+1:]
	}
	for _, caller := range g.Callers(t.NodeID) {
		if len(gc.CallSites) == maxCallSites || used >= budget {
			break
		}
		cs, ok := callSiteSnippet(repoRoot, caller, symbol)
		if !ok || used+len(cs.Snippet) > budget {
			continue
		}
		used += len(cs.Snippet)
		gc.CallSites = append(gc.CallSites, cs)
	}
	for _, callee := range g.Callees(t.NodeID) {
		p := fmt.Sprintf("%s:%d %s%s %s", callee.Path, callee.Line, callee.Name, callee.Params, callee.ReturnType)
		if used+len(p) > budget {
			break
		}
		used += len(p)
		gc.Callees = append(gc.Callees, p)
	}
	for _, sk := range g.SkillsFor(t.Path) {
		if used >= budget {
			break
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, sk.Path))
		if err != nil {
			continue // skill file missing on disk — skip, not fatal
		}
		excerpt := string(data)
		if used+len(excerpt) > budget {
			excerpt = excerpt[:budget-used]
		}
		gc.SkillExcerpt += excerpt
		used += len(excerpt)
	}
	return gc, nil
}

// readLines returns the file's lines split on newline.
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// packageClause returns the first "package ..." line, or "".
func packageClause(lines []string) string {
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "package ") {
			return strings.TrimSpace(ln)
		}
	}
	return ""
}

// sliceLines returns lines [start, end] (1-based, inclusive), clamped.
func sliceLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) || end < start {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

// callSiteSnippet locates the first mention of symbol inside the caller's
// span and returns +-callSiteMargin lines around it. Calls edges carry no
// line number, so this is the spec's documented heuristic; callers whose
// call site cannot be located are omitted.
func callSiteSnippet(repoRoot string, caller graph.Node, symbol string) (CallSite, bool) {
	lines, err := readLines(filepath.Join(repoRoot, caller.Path))
	if err != nil {
		return CallSite{}, false
	}
	start, end := caller.Line, caller.EndLine
	if start < 1 {
		start = 1
	}
	if end > len(lines) || end < start {
		end = len(lines)
	}
	for i := start + 1; i <= end; i++ { // start+1: skip the declaration line
		if strings.Contains(lines[i-1], symbol) {
			lo := max(i-callSiteMargin, start)
			hi := min(i+callSiteMargin, end)
			return CallSite{
				Caller:  fmt.Sprintf("%s:%d (%s)", caller.Path, i, caller.Name),
				Snippet: strings.Join(lines[lo-1:hi], "\n"),
			}, true
		}
	}
	return CallSite{}, false
}
