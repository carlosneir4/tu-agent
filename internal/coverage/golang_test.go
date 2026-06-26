package coverage

import (
	"strings"
	"testing"
)

func TestParseGoProfile(t *testing.T) {
	const prof = `mode: set
github.com/tu/tu-agent/internal/x/y.go:10.20,12.3 2 1
github.com/tu/tu-agent/internal/x/y.go:13.2,14.10 1 0
`
	p, err := ParseGoProfile(strings.NewReader(prof), "github.com/tu/tu-agent")
	if err != nil {
		t.Fatal(err)
	}
	ratio, ok := p.SymbolCoverage("internal/x/y.go", 10, 14)
	if !ok {
		t.Fatal("want data")
	}
	if ratio < 0.59 || ratio > 0.61 { // 3 of 5 lines covered
		t.Fatalf("ratio = %v, want ~0.6", ratio)
	}
}
