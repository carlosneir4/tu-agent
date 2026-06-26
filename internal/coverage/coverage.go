// Package coverage parses language coverage reports into per-symbol coverage
// for the knowledge graph's test-gap ranking.
package coverage

import "strings"

// FileCoverage records, per source file, which lines are instrumented (Known)
// and which of those are covered.
type FileCoverage struct {
	Covered map[int]bool
	Known   map[int]bool
}

// Profile is per-file coverage keyed by the report's file path (normalized to
// repo-relative where the parser can; matched by suffix otherwise).
type Profile map[string]FileCoverage

// add records line as known, and covered when covered is true. Once a line is
// covered by any block it stays covered.
func (p Profile) add(file string, line int, covered bool) {
	fc, ok := p[file]
	if !ok {
		fc = FileCoverage{Covered: map[int]bool{}, Known: map[int]bool{}}
		p[file] = fc
	}
	fc.Known[line] = true
	if covered {
		fc.Covered[line] = true
	}
}

// SymbolCoverage returns the covered fraction of the known lines within
// [start,end] (inclusive) of the matched file. hasData is false when no known
// line falls in the span, signalling the caller to fall back to the graph proxy.
func (p Profile) SymbolCoverage(path string, start, end int) (ratio float64, hasData bool) {
	fc, ok := p.match(path)
	if !ok {
		return 0, false
	}
	known, cov := 0, 0
	for ln := start; ln <= end; ln++ {
		if fc.Known[ln] {
			known++
			if fc.Covered[ln] {
				cov++
			}
		}
	}
	if known == 0 {
		return 0, false
	}
	return float64(cov) / float64(known), true
}

// match finds the FileCoverage for path: exact key, else the longest key that
// is a suffix of path or that path is a suffix of (bridging differing src roots).
func (p Profile) match(path string) (FileCoverage, bool) {
	if fc, ok := p[path]; ok {
		return fc, true
	}
	best := ""
	for k := range p {
		if strings.HasSuffix(path, "/"+k) || strings.HasSuffix(k, "/"+path) || k == path {
			// Longest matching key wins; equal-length ties resolve by map order
			// (degenerate — distinct files rarely suffix-match the same path).
			if len(k) > len(best) {
				best = k
			}
		}
	}
	if best == "" {
		return FileCoverage{}, false
	}
	return p[best], true
}

// Merge folds other into p: every known line is recorded, and a line covered in
// either profile stays covered (covered-wins, consistent with add).
func (p Profile) Merge(other Profile) {
	for file, fc := range other {
		for ln := range fc.Known {
			p.add(file, ln, fc.Covered[ln])
		}
	}
}

// Overall returns the aggregate covered and known line counts across every
// file in the profile, with ratio = covered/known (0 when known is 0). Because
// Covered is always a subset of Known (maintained by add), covered <= known.
func (p Profile) Overall() (covered, known int, ratio float64) {
	for _, fc := range p {
		known += len(fc.Known)
		covered += len(fc.Covered)
	}
	if known == 0 {
		return 0, 0, 0
	}
	return covered, known, float64(covered) / float64(known)
}
