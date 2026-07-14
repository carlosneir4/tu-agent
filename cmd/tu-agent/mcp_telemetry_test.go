package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// readTelemetryEntries reads a JSONL telemetry file and decodes every line.
func readTelemetryEntries(t *testing.T, path string) []telemetry.Entry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readTelemetryEntries: %v", err)
	}
	var entries []telemetry.Entry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e telemetry.Entry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("readTelemetryEntries: unmarshal line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("readTelemetryEntries: scan: %v", err)
	}
	return entries
}

// withMCPTelemetryLogger points mcpTelemetryLogger at a fresh logger over a
// temp file for the duration of the test, restoring the nil default after.
func withMCPTelemetryLogger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	lg, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("telemetry.NewLogger: %v", err)
	}
	prev := mcpTelemetryLogger
	mcpTelemetryLogger = lg
	t.Cleanup(func() { mcpTelemetryLogger = prev })
	return path
}

func TestMaybeInitMCPTelemetry_MinimalLeavesLoggerNil(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")
	prev := mcpTelemetryLogger
	mcpTelemetryLogger = nil
	t.Cleanup(func() { mcpTelemetryLogger = prev })

	maybeInitMCPTelemetry()

	if mcpTelemetryLogger != nil {
		t.Fatalf("mcpTelemetryLogger must stay nil at minimal level, got %v", mcpTelemetryLogger)
	}
}

func TestMaybeInitMCPTelemetry_FullSetsLogger(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")
	prev := mcpTelemetryLogger
	mcpTelemetryLogger = nil
	t.Cleanup(func() { mcpTelemetryLogger = prev })

	maybeInitMCPTelemetry()

	if mcpTelemetryLogger == nil {
		t.Fatal("mcpTelemetryLogger must be set at full level")
	}
}

func TestMCP_TelemetryMiddlewareRecordsToolCall(t *testing.T) {
	path := withMCPTelemetryLogger(t)

	t.Chdir(t.TempDir())
	buildFixtureGraph(t)

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	callTool(t, session, "find_symbol", map[string]any{"symbol": "Widget"})

	entries := readTelemetryEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("got %d telemetry entries, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Event != telemetry.EventMCPCall {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventMCPCall)
	}
	if e.Tool != "find_symbol" {
		t.Errorf("Tool = %q, want find_symbol", e.Tool)
	}
	if e.DurationMS < 0 {
		t.Errorf("DurationMS = %d, want >= 0", e.DurationMS)
	}
	if !e.OK {
		t.Errorf("OK = %v, want true", e.OK)
	}
}

func TestMCP_TelemetryUnknownToolNoPanicRecordsFailure(t *testing.T) {
	path := withMCPTelemetryLogger(t)

	t.Chdir(t.TempDir())
	buildFixtureGraph(t)

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// Unknown tool: SDK callTool returns a typed-nil *mcp.CallToolResult wrapped
	// in the mcp.Result interface plus a jsonrpc error. The middleware must not
	// panic dereferencing that typed nil.
	_, callErr := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "no_such_tool_xyz",
		Arguments: map[string]any{},
	})
	if callErr == nil {
		t.Fatal("expected an error for an unknown tool, got nil")
	}

	entries := readTelemetryEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("got %d telemetry entries, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Event != telemetry.EventMCPCall {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventMCPCall)
	}
	if e.OK {
		t.Errorf("OK = %v, want false for a failed call", e.OK)
	}
}

func TestMCP_MemSearchFailureRecordsNotOK(t *testing.T) {
	path := withMCPTelemetryLogger(t)

	t.Chdir(t.TempDir())

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// Empty query is an early error path in handleMemSearch; the SDK surfaces a
	// handler error as a tool-error CallToolResult (IsError=true), not a
	// transport error, so callErr stays nil.
	res, callErr := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "mem_search",
		Arguments: map[string]any{"query": ""},
	})
	if callErr != nil {
		t.Fatalf("CallTool transport error: %v", callErr)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected a tool-error result for empty mem_search query, got %+v", res)
	}

	entries := readTelemetryEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("got %d telemetry entries, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Event != telemetry.EventMCPCall {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventMCPCall)
	}
	if e.Tool != "mem_search" {
		t.Errorf("Tool = %q, want mem_search", e.Tool)
	}
	if e.OK {
		t.Errorf("OK = %v, want false for a failed mem_search", e.OK)
	}
}

func TestMCP_TelemetryNilLoggerWritesNothing(t *testing.T) {
	// mcpTelemetryLogger left at its nil default (no withMCPTelemetryLogger call).
	if mcpTelemetryLogger != nil {
		t.Fatalf("mcpTelemetryLogger must default to nil, got %v", mcpTelemetryLogger)
	}

	t.Chdir(t.TempDir())
	buildFixtureGraph(t)

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	callTool(t, session, "find_symbol", map[string]any{"symbol": "Widget"})

	if _, err := os.Stat(filepath.Join(".tu-agent", "telemetry.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected no telemetry.jsonl with nil logger, stat err = %v", err)
	}
}

func TestMCP_TelemetrySkipsSelfReportingMemSearch(t *testing.T) {
	path := withMCPTelemetryLogger(t)

	t.Chdir(t.TempDir())

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	callTool(t, session, "mem_search", map[string]any{"query": "nothing-will-match-xyz"})

	entries := readTelemetryEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("got %d telemetry entries for mem_search, want exactly 1 (no middleware double-emit): %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "mem_search" {
		t.Errorf("Tool = %q, want mem_search", e.Tool)
	}
	if e.Event != telemetry.EventMCPCall {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventMCPCall)
	}
}

func TestMCP_MemSearchZeroResultFlag(t *testing.T) {
	path := withMCPTelemetryLogger(t)

	t.Chdir(t.TempDir())

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("decision/topic-a", "matches-something body", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newMCPServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	callTool(t, session, "mem_search", map[string]any{"query": "nothing-will-match-xyz"})
	callTool(t, session, "mem_search", map[string]any{"query": "matches-something"})

	entries := readTelemetryEntries(t, path)
	if len(entries) != 2 {
		t.Fatalf("got %d telemetry entries, want 2: %+v", len(entries), entries)
	}
	if !entries[0].ZeroResult {
		t.Errorf("first mem_search (no match): ZeroResult = %v, want true", entries[0].ZeroResult)
	}
	if entries[1].ZeroResult {
		t.Errorf("second mem_search (has match): ZeroResult = %v, want false", entries[1].ZeroResult)
	}
}
