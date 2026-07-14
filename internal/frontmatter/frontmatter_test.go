package frontmatter_test

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/frontmatter"
)

func TestSplit_NoFrontmatter(t *testing.T) {
	fm, body, ok := frontmatter.Split("# Just markdown\nNo frontmatter here.")
	if ok {
		t.Fatalf("expected ok=false, got fm=%q body=%q", fm, body)
	}
	if fm != "" || body != "" {
		t.Errorf("expected empty fm/body on ok=false, got fm=%q body=%q", fm, body)
	}
}

func TestSplit_OpeningWithoutClosing(t *testing.T) {
	_, _, ok := frontmatter.Split("---\nname: broken\ndescription: no closing delimiter")
	if ok {
		t.Fatal("expected ok=false for unterminated frontmatter")
	}
}

func TestSplit_EmptyFrontmatterBlock(t *testing.T) {
	fm, body, ok := frontmatter.Split("---\n---\nbody text")
	if !ok {
		t.Fatal("expected ok=true for an empty-but-well-formed frontmatter block")
	}
	if fm != "" {
		t.Errorf("expected empty frontmatter, got %q", fm)
	}
	if body != "body text" {
		t.Errorf("body: got %q", body)
	}
}

func TestSplit_FrontmatterAndBody(t *testing.T) {
	content := "---\nname: test-skill\ndescription: a test skill\ntriggers:\n  - test\n  - demo\n---\n# Body here\nmore body"
	fm, body, ok := frontmatter.Split(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantFM := "name: test-skill\ndescription: a test skill\ntriggers:\n  - test\n  - demo"
	if fm != wantFM {
		t.Errorf("fm: got %q, want %q", fm, wantFM)
	}
	wantBody := "# Body here\nmore body"
	if body != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

func TestSplit_CRLF(t *testing.T) {
	content := "---\r\nname: test-skill\r\ndescription: a test skill\r\n---\r\n# Body here\r\nmore body"
	fm, body, ok := frontmatter.Split(content)
	if !ok {
		t.Fatal("expected ok=true for CRLF input")
	}
	wantFM := "name: test-skill\ndescription: a test skill"
	if fm != wantFM {
		t.Errorf("fm: got %q, want %q", fm, wantFM)
	}
	wantBody := "# Body here\nmore body"
	if body != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

func TestSplit_BodyContainingDashLines(t *testing.T) {
	// A "---" line inside the body (after the real closing delimiter) must
	// not be mistaken for a later closing delimiter — the FIRST "---" after
	// the opening one closes the frontmatter block, full stop.
	content := "---\nname: x\n---\nBody line1\n---\nBody line2"
	fm, body, ok := frontmatter.Split(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if fm != "name: x" {
		t.Errorf("fm: got %q", fm)
	}
	wantBody := "Body line1\n---\nBody line2"
	if body != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

func TestSplit_EmptyContent(t *testing.T) {
	_, _, ok := frontmatter.Split("")
	if ok {
		t.Fatal("expected ok=false for empty content")
	}
}

func TestSplit_NotAtStart(t *testing.T) {
	// The opening delimiter must be the very first line; a "---" line
	// further down does not retroactively become an opening delimiter.
	_, _, ok := frontmatter.Split("intro text\n---\nname: x\n---\nbody")
	if ok {
		t.Fatal("expected ok=false when --- is not the first line")
	}
}

func TestSplitLoose_LeadingPreamble(t *testing.T) {
	content := "<!-- tu-agent:crystallize source-hash=abc label=bash-helpers -->\n" +
		"---\nname: bash-helpers\ndescription: reusable snippets\n---\n# Body\nmore body"
	fm, body, ok := frontmatter.SplitLoose(content)
	if !ok {
		t.Fatal("expected ok=true for a leading preamble before the frontmatter")
	}
	wantFM := "name: bash-helpers\ndescription: reusable snippets"
	if fm != wantFM {
		t.Errorf("fm: got %q, want %q", fm, wantFM)
	}
	wantBody := "# Body\nmore body"
	if body != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

func TestSplitLoose_PreambleAndBodyDashLines(t *testing.T) {
	// The exact shape a crystallized skill takes: a provenance comment before
	// the frontmatter, AND a "---" line inside the body (e.g. a horizontal
	// rule). Pins that the preamble is skipped to find the opening delimiter,
	// the FIRST closing delimiter wins (not a later body "---"), and body
	// "---" lines are preserved verbatim.
	content := "<!-- tu-agent:crystallize source-hash=abc label=foo -->\n" +
		"---\nname: foo\ndescription: bar\n---\nBody line one\n---\nBody line two"
	fm, body, ok := frontmatter.SplitLoose(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantFM := "name: foo\ndescription: bar"
	if fm != wantFM {
		t.Errorf("fm: got %q, want %q", fm, wantFM)
	}
	if !strings.Contains(body, "Body line one") {
		t.Errorf("body missing %q: got %q", "Body line one", body)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body missing preserved %q: got %q", "---", body)
	}
	if !strings.Contains(body, "Body line two") {
		t.Errorf("body missing %q: got %q", "Body line two", body)
	}
}

func TestSplitLoose_NoOpeningAnywhere(t *testing.T) {
	_, _, ok := frontmatter.SplitLoose("# Just markdown\nNo frontmatter here.")
	if ok {
		t.Fatal("expected ok=false when no --- line exists")
	}
}

func TestSplitLoose_UnclosedAfterPreamble(t *testing.T) {
	_, _, ok := frontmatter.SplitLoose("intro\n---\nname: broken\ndescription: no closing delimiter")
	if ok {
		t.Fatal("expected ok=false for unterminated frontmatter after a preamble")
	}
}

func TestSplitLoose_AtStartStillWorks(t *testing.T) {
	// SplitLoose must behave the same as Split for the common case where ---
	// is already the first line.
	content := "---\nname: test-skill\n---\nbody"
	fm, body, ok := frontmatter.SplitLoose(content)
	if !ok || fm != "name: test-skill" || body != "body" {
		t.Errorf("got fm=%q body=%q ok=%v", fm, body, ok)
	}
}

func TestBounds_Empty(t *testing.T) {
	if _, _, ok := frontmatter.Bounds(nil); ok {
		t.Fatal("expected ok=false for nil lines")
	}
	if _, _, ok := frontmatter.Bounds([]string{}); ok {
		t.Fatal("expected ok=false for empty lines")
	}
}

func TestBounds_NoOpening(t *testing.T) {
	if _, _, ok := frontmatter.Bounds([]string{"name: x", "---", "body"}); ok {
		t.Fatal("expected ok=false when line 0 is not ---")
	}
}

func TestBounds_NoClosing(t *testing.T) {
	if _, _, ok := frontmatter.Bounds([]string{"---", "name: x"}); ok {
		t.Fatal("expected ok=false with no closing delimiter")
	}
}

func TestBounds_Valid(t *testing.T) {
	lines := []string{"---", "name: x", "tools: Read, Write", "---", "body"}
	start, end, ok := frontmatter.Bounds(lines)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if start != 0 {
		t.Errorf("start: got %d, want 0", start)
	}
	if end != 3 {
		t.Errorf("end: got %d, want 3", end)
	}
}

func TestBounds_FirstClosingWins(t *testing.T) {
	lines := []string{"---", "name: x", "---", "body", "---", "more body"}
	_, end, ok := frontmatter.Bounds(lines)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if end != 2 {
		t.Errorf("end: got %d, want the first closing delimiter (2)", end)
	}
}
