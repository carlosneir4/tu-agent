package main

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
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
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
		if got := sanitizeTicket(c.in); got != c.want {
			t.Errorf("sanitizeTicket(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTddRelBaseAndBaseDir(t *testing.T) {
	if got := tddRelBase("ABC-123", "user-login"); got != filepath.Join(".tu-agent", "tdd", "ABC-123-user-login") {
		t.Errorf("tddRelBase with ticket = %q", got)
	}
	if got := tddRelBase("", "user-login"); got != filepath.Join(".tu-agent", "tdd", "user-login") {
		t.Errorf("tddRelBase no ticket = %q", got)
	}
	if got := tddBaseDir("/repo", "ABC-123", "user-login"); got != filepath.Join("/repo", ".tu-agent", "tdd", "ABC-123-user-login") {
		t.Errorf("tddBaseDir = %q", got)
	}
}

func TestWarnBranch(t *testing.T) {
	var buf bytes.Buffer
	warnBranch("main", "ABC-123", &buf)
	if !strings.Contains(buf.String(), "ABC-123") || !strings.Contains(buf.String(), "main") {
		t.Errorf("expected mismatch warning, got %q", buf.String())
	}
	buf.Reset()
	warnBranch("feature/ABC-123-login", "ABC-123", &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no warning on match, got %q", buf.String())
	}
	buf.Reset()
	warnBranch("main", "", &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no warning without ticket, got %q", buf.String())
	}
}

func TestTddPathCmdParity(t *testing.T) {
	var buf bytes.Buffer
	cmd := tddPathCmd
	cmd.SetOut(&buf)
	tddPathTicket = "ABC-9"
	if err := cmd.RunE(cmd, []string{"user", "login"}); err != nil {
		t.Fatalf("path cmd: %v", err)
	}
	tddPathTicket = ""
	got := strings.TrimSpace(buf.String())
	want := tddRelBase("ABC-9", slugify("user login"))
	if got != want {
		t.Errorf("tdd path output = %q, want %q", got, want)
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

	got, ok := resolveTddBase(root, "ABC-1")
	if !ok || got != older {
		t.Errorf("resolveTddBase by ticket = %q, %v; want %q", got, ok, older)
	}
	got, ok = resolveTddBase(root, "")
	if !ok || got != newer {
		t.Errorf("resolveTddBase newest = %q, %v; want %q", got, ok, newer)
	}
	got, ok = resolveTddBase(t.TempDir(), "")
	if ok {
		t.Errorf("expected not-ok on empty repo, got %q", got)
	}
}
