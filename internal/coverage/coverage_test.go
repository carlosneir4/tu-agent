package coverage

import "testing"

func TestSymbolCoverageIntersectsSpan(t *testing.T) {
	p := Profile{}
	// lines 10..14 known; 10,11,12 covered, 13,14 not.
	for _, ln := range []int{10, 11, 12, 13, 14} {
		p.add("internal/x/y.go", ln, ln <= 12)
	}
	ratio, ok := p.SymbolCoverage("internal/x/y.go", 10, 14)
	if !ok {
		t.Fatal("want data")
	}
	if ratio < 0.59 || ratio > 0.61 {
		t.Fatalf("ratio = %v, want ~0.6", ratio)
	}
	// suffix match: node path carries a src root the report omits.
	if _, ok := p.SymbolCoverage("repo/internal/x/y.go", 10, 14); !ok {
		t.Fatal("suffix match failed")
	}
	// no known lines in span → no data (graph proxy should take over).
	if _, ok := p.SymbolCoverage("internal/x/y.go", 100, 110); ok {
		t.Fatal("want no data outside known lines")
	}
	// A path that shares a trailing substring but not a "/"-boundary must NOT match.
	if _, ok := p.SymbolCoverage("other/xy.go", 10, 14); ok {
		t.Fatal("xy.go must not match internal/x/y.go")
	}
}

func TestProfileOverall(t *testing.T) {
	p := Profile{}
	p.add("a.go", 1, true)
	p.add("a.go", 2, true)
	p.add("a.go", 3, false)
	p.add("b.go", 10, false)
	// a.go: 2/3 covered; b.go: 0/1 covered → overall 2/4

	covered, known, ratio := p.Overall()
	if covered != 2 || known != 4 {
		t.Fatalf("covered/known = %d/%d, want 2/4", covered, known)
	}
	if ratio < 0.49 || ratio > 0.51 {
		t.Errorf("ratio = %v, want 0.5", ratio)
	}

	c2, k2, r2 := Profile{}.Overall()
	if c2 != 0 || k2 != 0 || r2 != 0 {
		t.Errorf("empty profile = %d/%d/%v, want 0/0/0", c2, k2, r2)
	}
}
