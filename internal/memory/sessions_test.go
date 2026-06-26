package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSessionStartEnd(t *testing.T) {
	s := openTestStore(t)
	sess, prev, err := s.SessionStart("")
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if sess.ID == "" || !sess.EndedAt.IsZero() {
		t.Fatalf("new session should be active: %+v", sess)
	}
	if prev != "" {
		t.Errorf("first start should have no previous summary, got %q", prev)
	}
	ended, err := s.SessionEnd("", "fixed the cache race")
	if err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	if ended.Summary != "fixed the cache race" || ended.EndedAt.IsZero() {
		t.Errorf("explicit summary not stored / not ended: %+v", ended)
	}
}

func TestSessionEndComposesSummaryFromObservations(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.SessionStart(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("bug/cache-race", "races on eviction", UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	ended, err := s.SessionEnd("", "")
	if err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	if ended.Summary == "" || ended.Summary == "(no observations recorded)" {
		t.Errorf("expected composed summary from observations, got %q", ended.Summary)
	}
	if !strings.Contains(ended.Summary, "bug/cache-race") {
		t.Errorf("composed summary should mention the topic: %q", ended.Summary)
	}
}

func TestSessionStartAutoClosesPrevious(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.SessionStart(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionEnd("", "first session work"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionStart(""); err != nil {
		t.Fatal(err)
	}
	_, prev, err := s.SessionStart("")
	if err != nil {
		t.Fatalf("third start: %v", err)
	}
	if prev == "" {
		t.Error("third start should surface the auto-closed second session's summary")
	}
	if _, found, err := s.activeSession(""); err != nil || !found {
		t.Fatalf("want exactly one active session, found=%v err=%v", found, err)
	}
}

func TestSessionEndNoActiveErrors(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.SessionEnd("", "x"); err == nil {
		t.Error("ending with no active session should error")
	}
}

func TestSessionList(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.SessionStart(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionEnd("", "one"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionStart(""); err != nil { // leaves an active session
		t.Fatal(err)
	}
	list, err := s.SessionList("", 10)
	if err != nil {
		t.Fatalf("SessionList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(list))
	}
	// newest first: the active (unended) one leads
	if !list[0].EndedAt.IsZero() {
		t.Errorf("newest session should be the active one, got %+v", list[0])
	}
	if list[1].Summary != "one" {
		t.Errorf("older session summary = %q, want one", list[1].Summary)
	}
}
