package coverage

import (
	"strings"
	"testing"
)

func TestParseCobertura(t *testing.T) {
	const xml = `<coverage>
<packages><package><classes>
<class filename="pkg/mod.py">
<lines>
<line number="5" hits="2"/>
<line number="6" hits="0"/>
<line number="7" hits="1"/>
</lines>
</class>
</classes></package></packages>
</coverage>`
	p, err := ParseCobertura(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	ratio, ok := p.SymbolCoverage("pkg/mod.py", 5, 7)
	if !ok {
		t.Fatal("want data")
	}
	if ratio < 0.66 || ratio > 0.67 { // 2 of 3
		t.Fatalf("ratio = %v, want ~0.667", ratio)
	}
}
