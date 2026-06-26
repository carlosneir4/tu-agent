package coverage

import (
	"strings"
	"testing"
)

func TestParseJaCoCo(t *testing.T) {
	const xml = `<report name="x">
<package name="com/foo">
<sourcefile name="Bar.java">
<line nr="10" mi="0" ci="3"/>
<line nr="11" mi="2" ci="0"/>
</sourcefile>
</package>
</report>`
	p, err := ParseJaCoCo(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	ratio, ok := p.SymbolCoverage("src/main/java/com/foo/Bar.java", 10, 11)
	if !ok {
		t.Fatal("want data (suffix match on com/foo/Bar.java)")
	}
	if ratio != 0.5 {
		t.Fatalf("ratio = %v, want 0.5", ratio)
	}
}
