package tdd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user login", "user-login"},
		{"Add OAuth2 support!!!", "add-oauth2-support"},
		{"  spaced   out  ", "spaced-out"},
		{"one two three four five six seven", "one-two-three-four-five"}, // first 5 words
		{"", "feature"},
		{"???", "feature"},
		{"a-really-really-really-really-long-single-token-name", "a-really-really-really-really-long-singl"}, // 40 chars
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTicket(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ABC-123", "ABC-123"},
		{"abc/../x", "abc-..-x"},
		{"PROJ 42", "PROJ-42"},
	}
	for _, c := range cases {
		if got := SanitizeTicket(c.in); got != c.want {
			t.Errorf("SanitizeTicket(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTddRelBase(t *testing.T) {
	if got := TddRelBase("ABC-123", "user-login"); got != filepath.Join(".tu-agent", "tdd", "ABC-123-user-login") {
		t.Errorf("TddRelBase with ticket = %q", got)
	}
	if got := TddRelBase("", "user-login"); got != filepath.Join(".tu-agent", "tdd", "user-login") {
		t.Errorf("TddRelBase no ticket = %q", got)
	}
}

func TestWarnBranch(t *testing.T) {
	var buf bytes.Buffer
	WarnBranch("main", "ABC-123", &buf)
	if !strings.Contains(buf.String(), "ABC-123") || !strings.Contains(buf.String(), "main") {
		t.Errorf("expected mismatch warning, got %q", buf.String())
	}
	buf.Reset()
	WarnBranch("feature/ABC-123-login", "ABC-123", &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no warning on match, got %q", buf.String())
	}
	buf.Reset()
	WarnBranch("main", "", &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no warning without ticket, got %q", buf.String())
	}
}

func TestResolveTddBase(t *testing.T) {
	root := t.TempDir()
	tddDir := filepath.Join(root, ".tu-agent", "tdd")
	older := filepath.Join(tddDir, "ABC-1-old")
	newer := filepath.Join(tddDir, "ABC-2-new")
	if err := os.MkdirAll(older, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	got, ok := ResolveTddBase(root, "ABC-1")
	if !ok || got != older {
		t.Errorf("ResolveTddBase by ticket = %q, %v; want %q", got, ok, older)
	}
	got, ok = ResolveTddBase(root, "")
	if !ok || got != newer {
		t.Errorf("ResolveTddBase newest = %q, %v; want %q", got, ok, newer)
	}
	got, ok = ResolveTddBase(t.TempDir(), "")
	if ok {
		t.Errorf("expected not-ok on empty repo, got %q", got)
	}
}

// TestProjectConfigPathStaysAtRoot guards that config.yaml stays at the
// .tu-agent root through the per-subsystem relayout (dir-relayout @s5). Unlike
// memory/graph/telemetry, the config file is the entry point and must NOT be
// nested under a subsystem directory. This is expected to already pass — it is
// a guard against accidental relocation, not part of the RED gate.
func TestProjectConfigPathStaysAtRoot(t *testing.T) {
	const root = "/repo"
	got := projectConfigPath(root)
	want := filepath.Join(root, ".tu-agent", "config.yaml")
	if got != want {
		t.Errorf("projectConfigPath(%q) = %q, want %q", root, got, want)
	}
}

func TestPromptRelBase(t *testing.T) {
	// explicit base wins, ignores ticket/desc
	if got := PromptRelBase(".tu-agent/tdd/EXPLICIT", "ABC-1", []string{"user", "login"}); got != ".tu-agent/tdd/EXPLICIT" {
		t.Errorf("explicit base = %q", got)
	}
	// no base -> derive from ticket + desc
	if got := PromptRelBase("", "ABC-1", []string{"user", "login"}); got != TddRelBase("ABC-1", Slugify("user login")) {
		t.Errorf("derived base = %q", got)
	}
}
