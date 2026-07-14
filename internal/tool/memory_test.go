package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func storeLen(t *testing.T, s *memory.Store) int {
	t.Helper()
	n, err := s.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	return n
}

func mustAdd(t *testing.T, s *memory.Store, topic, content string) {
	t.Helper()
	if _, err := s.Add(topic, content, "agent"); err != nil {
		t.Fatalf("Add(%q): %v", topic, err)
	}
}

func TestMemSaveTool_Name(t *testing.T) {
	tl := tool.NewMemSaveTool(newTestStore(t), "")
	if tl.Name() != "mem_save" {
		t.Errorf("Name() = %q, want %q", tl.Name(), "mem_save")
	}
}

func TestMemSaveTool_SavesObservation(t *testing.T) {
	store := newTestStore(t)
	tl := tool.NewMemSaveTool(store, "")

	input, _ := json.Marshal(map[string]string{"topic": "routing", "content": "use local for explore"})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "routing") {
		t.Errorf("expected result to mention topic, got %q", result)
	}
	if n := storeLen(t, store); n != 1 {
		t.Errorf("expected 1 observation after save, got %d", n)
	}
}

func TestMemSaveTool_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	tl := tool.NewMemSaveTool(store, "")

	input, _ := json.Marshal(map[string]string{"topic": "db", "content": "pool size 10"})
	if _, err := tl.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	loaded, err := memory.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("reopen after save error: %v", err)
	}
	defer loaded.Close()
	if n := storeLen(t, loaded); n != 1 {
		t.Errorf("expected 1 persisted observation, got %d", n)
	}
}

func TestMemSaveTool_UpsertBumpsRevision(t *testing.T) {
	store := newTestStore(t)
	tl := tool.NewMemSaveTool(store, "")

	input1, _ := json.Marshal(map[string]string{"topic": "architecture/auth", "content": "v1"})
	if _, err := tl.Run(context.Background(), input1); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	input2, _ := json.Marshal(map[string]string{"topic": "architecture/auth", "content": "v2"})
	result, err := tl.Run(context.Background(), input2)
	if err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
	if !strings.Contains(result, "rev:2") {
		t.Errorf("expected rev:2 in result, got %q", result)
	}
	if n := storeLen(t, store); n != 1 {
		t.Errorf("expected 1 observation after upsert, got %d", n)
	}
}

func TestMemSearchTool_Name(t *testing.T) {
	tl := tool.NewMemSearchTool(newTestStore(t))
	if tl.Name() != "mem_search" {
		t.Errorf("Name() = %q, want %q", tl.Name(), "mem_search")
	}
}

func TestMemSearchTool_FindsMatch(t *testing.T) {
	store := newTestStore(t)
	mustAdd(t, store, "auth", "JWT tokens expire in 1h")
	mustAdd(t, store, "db", "Postgres pool size 10")
	tl := tool.NewMemSearchTool(store)

	input, _ := json.Marshal(map[string]string{"query": "jwt"})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "JWT tokens expire") {
		t.Errorf("expected search result to contain observation content, got %q", result)
	}
}

func TestMemSearchTool_NoMatch(t *testing.T) {
	store := newTestStore(t)
	mustAdd(t, store, "auth", "jwt tokens")
	tl := tool.NewMemSearchTool(store)

	input, _ := json.Marshal(map[string]string{"query": "kubernetes"})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "No observations") {
		t.Errorf("expected no-match message, got %q", result)
	}
}

func TestMemSearchTool_TypeFilter(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Upsert("trap/x", "alpha trap content", memory.UpsertOpts{Type: "gotcha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert("design/x", "alpha decision content", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	tl := tool.NewMemSearchTool(store)

	// query + type → only the gotcha-typed match
	input, _ := json.Marshal(map[string]string{"query": "alpha", "type": "gotcha"})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "alpha trap content") || strings.Contains(result, "alpha decision content") {
		t.Errorf("--type gotcha should show only the gotcha, got %q", result)
	}

	// empty query + type → list all of that type (relaxed empty-query guard)
	input2, _ := json.Marshal(map[string]string{"type": "decision"})
	result2, err := tl.Run(context.Background(), input2)
	if err != nil {
		t.Fatalf("empty query + type should not error: %v", err)
	}
	if !strings.Contains(result2, "alpha decision content") {
		t.Errorf("empty query + type should list all of that type, got %q", result2)
	}

	// both empty → still an error
	input3, _ := json.Marshal(map[string]string{})
	if _, err := tl.Run(context.Background(), input3); err == nil {
		t.Error("empty query and empty type should error")
	}
}

func TestMemRecentTool_Name(t *testing.T) {
	tl := tool.NewMemRecentTool(newTestStore(t))
	if tl.Name() != "mem_recent" {
		t.Errorf("Name() = %q, want %q", tl.Name(), "mem_recent")
	}
}

func TestMemRecentTool_DefaultN(t *testing.T) {
	store := newTestStore(t)
	for i := range 7 {
		mustAdd(t, store, "topic", fmt.Sprintf("content %d", i))
	}
	tl := tool.NewMemRecentTool(store)

	// n=0 → uses default of 5
	input, _ := json.Marshal(map[string]int{"n": 0})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "content 6") {
		t.Errorf("expected most recent observation in result, got %q", result)
	}
}

func TestMemRecentTool_EmptyStore(t *testing.T) {
	tl := tool.NewMemRecentTool(newTestStore(t))

	input, _ := json.Marshal(map[string]int{"n": 5})
	result, err := tl.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(result, "No observations") {
		t.Errorf("expected empty-store message, got %q", result)
	}
}

func TestMemSaveTool_MalformedInput(t *testing.T) {
	tl := tool.NewMemSaveTool(newTestStore(t), "")
	_, err := tl.Run(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON input")
	}
}

func TestMemSaveTool_EmptyFields(t *testing.T) {
	tl := tool.NewMemSaveTool(newTestStore(t), "")
	input, _ := json.Marshal(map[string]string{"topic": "", "content": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty topic/content")
	}
}

func TestMemSearchTool_EmptyQuery(t *testing.T) {
	tl := tool.NewMemSearchTool(newTestStore(t))
	input, _ := json.Marshal(map[string]string{"query": ""})
	_, err := tl.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}
