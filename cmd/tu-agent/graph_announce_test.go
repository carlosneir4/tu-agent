package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/graph/store"
)

// decodeSessionStartHook parses announceGraph's stdout as the Claude Code
// SessionStart hook envelope, failing the test if it is not valid hook JSON.
func decodeSessionStartHook(t *testing.T, out string) sessionStartHook {
	t.Helper()
	var h sessionStartHook
	if err := json.Unmarshal([]byte(out), &h); err != nil {
		t.Fatalf("announce output is not valid hook JSON: %v\nraw: %s", err, out)
	}
	return h
}

// mustInitGraphFixture creates a .git marker (so repoRoot() anchors at dir)
// and opens the graph store to create .tu-agent/graph.db, then closes it.
func mustInitGraphFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	s, err := openGraphStore()
	if err != nil {
		t.Fatalf("openGraphStore: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close graph store: %v", err)
	}
}

func TestAnnounceGraph_NoGraphIsSilentNoop(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph without graph.db: %v", err)
	}
	if out != "" {
		t.Errorf("expected no output without a graph, got %q", out)
	}
}

func TestGraphEmptyWarning(t *testing.T) {
	if w := graphEmptyWarning(0, 5); !strings.Contains(w, "EMPTY") || !strings.Contains(w, "learn") {
		t.Errorf("empty graph (0 nodes, 5 files) should warn to run learn; got %q", w)
	}
	if w := graphEmptyWarning(10, 5); w != "" {
		t.Errorf("healthy graph should not warn; got %q", w)
	}
	if w := graphEmptyWarning(0, 0); w != "" {
		t.Errorf("unbuilt graph (no files) should not warn; got %q", w)
	}
}

// mustSeedFileRow opens the graph store and writes one files row with no nodes —
// the silent-empty-graph state (files present, zero nodes).
func mustSeedFileRow(t *testing.T) {
	t.Helper()
	s, err := openGraphStore()
	if err != nil {
		t.Fatalf("openGraphStore: %v", err)
	}
	if err := s.UpsertFile(store.FileRecord{Path: "a.go", SHA256: "x", Language: "go", Status: "ok"}); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAnnounceGraph_WarnsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInitGraphFixture(t, dir)
	mustSeedFileRow(t)

	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph: %v", err)
	}
	hook := decodeSessionStartHook(t, out)
	if !strings.Contains(hook.SystemMessage, "EMPTY") || !strings.Contains(hook.SystemMessage, "tu-agent learn") {
		t.Errorf("expected loud empty-graph warning in systemMessage; got:\n%s", hook.SystemMessage)
	}
	if strings.Contains(hook.SystemMessage, "graph ready") {
		t.Errorf("empty graph must not report 'graph ready'; got:\n%s", hook.SystemMessage)
	}
}

func TestAnnounceGraph_PrintsNudgeWithCounts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInitGraphFixture(t, dir)

	out, err := captureStdout(t, func() error { return announceGraph() })
	if err != nil {
		t.Fatalf("announceGraph: %v", err)
	}
	hook := decodeSessionStartHook(t, out)
	if hook.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", hook.HookSpecificOutput.HookEventName)
	}
	// The user sees a concise confirmation line...
	if !strings.Contains(hook.SystemMessage, "graph ready") {
		t.Errorf("systemMessage should confirm graph ready; got: %q", hook.SystemMessage)
	}
	// ...while the model gets the full PROTOCOL nudge as additionalContext.
	for _, want := range []string{
		"graph ready",
		"get_context",
		"DEFERRED",
		"ToolSearch",
		"tu-agent graph context",
		"mem_recent",
	} {
		if !strings.Contains(hook.HookSpecificOutput.AdditionalContext, want) {
			t.Errorf("additionalContext missing %q; got:\n%s", want, hook.HookSpecificOutput.AdditionalContext)
		}
	}
}

func TestWriteSessionStartHook_ShapeAndNoHTMLEscape(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSessionStartHook(&buf, "user line", "model <ctx>"); err != nil {
		t.Fatalf("writeSessionStartHook: %v", err)
	}
	var h sessionStartHook
	if err := json.Unmarshal(buf.Bytes(), &h); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if h.SystemMessage != "user line" {
		t.Errorf("systemMessage = %q, want %q", h.SystemMessage, "user line")
	}
	if h.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", h.HookSpecificOutput.HookEventName)
	}
	if h.HookSpecificOutput.AdditionalContext != "model <ctx>" {
		t.Errorf("additionalContext = %q, want %q", h.HookSpecificOutput.AdditionalContext, "model <ctx>")
	}
	// HTML escaping is disabled: the raw JSON keeps "<" literally. With escaping
	// on, encoding/json would emit the < escape and no literal "<" at all,
	// so the literal's presence is what proves SetEscapeHTML(false) took effect.
	if !strings.Contains(buf.String(), "model <ctx>") {
		t.Errorf("expected literal \"model <ctx>\" in raw JSON (HTML escaping off); got: %s", buf.String())
	}
}
