package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

func TestRunRescope_ReportsMove(t *testing.T) {
	dir := t.TempDir()
	s, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("project/rag", "strategy", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRescope(s, &out, "project/rag", "project", "personal"); err != nil {
		t.Fatalf("runRescope: %v", err)
	}
	if !strings.Contains(out.String(), "rescoped project/rag") {
		t.Errorf("output = %q, want rescoped message", out.String())
	}
	_ = s.Close()
}

func TestRunRescope_NotFoundExitsZero(t *testing.T) {
	dir := t.TempDir()
	s, _ := memory.Open(filepath.Join(dir, "memory.db"))
	var out bytes.Buffer
	if err := runRescope(s, &out, "missing", "project", "personal"); err != nil {
		t.Fatalf("not-found must not error: %v", err)
	}
	if !strings.Contains(out.String(), "no observation found") {
		t.Errorf("output = %q, want no-observation message", out.String())
	}
	_ = s.Close()
}

func TestRunRescope_RequiresScope(t *testing.T) {
	dir := t.TempDir()
	s, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Upsert("k", "some content", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runRescope(s, &out, "k", "project", "")
	if err == nil {
		t.Fatal("expected error for empty toScope, got nil")
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Errorf("error = %q, want message containing \"scope\"", err.Error())
	}

	// Verify the observation was NOT modified — its scope must still be "project".
	obs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range obs {
		if o.TopicKey == "k" {
			found = true
			if o.Scope != "project" {
				t.Errorf("observation scope = %q, want \"project\" (row was corrupted)", o.Scope)
			}
		}
	}
	if !found {
		t.Error("observation 'k' not found in store after failed rescope")
	}
}

func TestRunDelete_ReportsAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, _ := memory.Open(filepath.Join(dir, "memory.db"))
	if _, err := s.Upsert("k", "v", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDelete(s, &out, "k", "project"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deleted k") {
		t.Errorf("output = %q, want deleted message", out.String())
	}
	out.Reset()
	if err := runDelete(s, &out, "k", "project"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no observation found") {
		t.Errorf("second delete output = %q, want no-observation message", out.String())
	}
	_ = s.Close()
}
