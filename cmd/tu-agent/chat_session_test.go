package main

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestAppendLastSessionSummary(t *testing.T) {
	base := "SYSTEM PROMPT"
	// empty summary → prompt unchanged
	if got := appendLastSessionSummary(base, ""); got != base {
		t.Errorf("empty summary should not modify prompt, got %q", got)
	}
	// non-empty → appends a "## Last session" block containing the summary
	got := appendLastSessionSummary(base, "fixed the cache race")
	if !strings.HasPrefix(got, base) {
		t.Errorf("helper must preserve the original prompt prefix, got %q", got)
	}
	if !strings.Contains(got, "## Last session") {
		t.Errorf("expected a '## Last session' heading, got %q", got)
	}
	if !strings.Contains(got, "fixed the cache race") {
		t.Errorf("expected the summary text, got %q", got)
	}
}

// TestLastSessionPrecedesRecentMemory pins that, when composing the full prompt,
// the "## Last session" block appears before "## Recent Memory" so the model reads
// continuity first, then the most-recent observations.
func TestLastSessionPrecedesRecentMemory(t *testing.T) {
	base := "SYSTEM"
	withSession := appendLastSessionSummary(base, "did the thing")
	full := appendMemorySection(withSession, []memory.Observation{{Title: "t", Content: "c"}})
	li := strings.Index(full, "## Last session")
	ri := strings.Index(full, "## Recent Memory")
	if li < 0 || ri < 0 || li > ri {
		t.Errorf("want '## Last session' before '## Recent Memory'; li=%d ri=%d\n%s", li, ri, full)
	}
}
