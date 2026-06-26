package coverage

import (
	"strings"
	"testing"
)

func TestParseIstanbul(t *testing.T) {
	const j = `{
  "/abs/repo/src/store.ts": {
    "path": "/abs/repo/src/store.ts",
    "statementMap": {
      "0": {"start": {"line": 3}, "end": {"line": 3}},
      "1": {"start": {"line": 4}, "end": {"line": 5}}
    },
    "s": {"0": 1, "1": 0}
  }
}`
	p, err := ParseIstanbul(strings.NewReader(j), "/abs/repo")
	if err != nil {
		t.Fatal(err)
	}
	ratio, ok := p.SymbolCoverage("src/store.ts", 3, 5)
	if !ok {
		t.Fatal("want data")
	}
	if ratio < 0.33 || ratio > 0.34 { // line 3 covered; 4,5 not → 1 of 3
		t.Fatalf("ratio = %v, want ~0.333", ratio)
	}
}
