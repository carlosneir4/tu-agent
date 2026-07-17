package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

func TestEditWithoutContext(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		editedFile string
		want       bool
	}{
		{
			name: "no context query flags violation",
			lines: []string{
				`{"type":"assistant","message":{"content":[{"type":"text","text":"editing now"}]}}`,
			},
			editedFile: "cmd/tu-agent/guard.go",
			want:       true,
		},
		{
			name: "get_context on the file suppresses violation",
			lines: []string{
				`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_tu-agent_tu-agent-graph__get_context","input":{"path":"cmd/tu-agent/guard.go"}}]}}`,
			},
			editedFile: "cmd/tu-agent/guard.go",
			want:       false,
		},
		{
			name: "get_impact on a different file still flags",
			lines: []string{
				`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_tu-agent_tu-agent-graph__get_impact","input":{"path":"cmd/tu-agent/other.go"}}]}}`,
			},
			editedFile: "cmd/tu-agent/guard.go",
			want:       true,
		},
		{
			name: "find_symbol basename match suppresses violation",
			lines: []string{
				`{"tool_use":{"name":"mcp__plugin_tu-agent_tu-agent-graph__find_symbol","input":{"query":"guard.go"}}}`,
			},
			editedFile: "cmd/tu-agent/guard.go",
			want:       false,
		},
		{
			name: "get_concept basename match suppresses violation",
			lines: []string{
				`{"tool_use":{"name":"mcp__plugin_tu-agent_tu-agent-graph__get_concept","input":{"name":"guard.go"}}}`,
			},
			editedFile: "cmd/tu-agent/guard.go",
			want:       false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "transcript.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := editWithoutContext(path, tc.editedFile); got != tc.want {
				t.Errorf("editWithoutContext() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEditWithoutContext_SymbolQueryPairedByID(t *testing.T) {
	// A get_context("Store.Open") query: the tool_use line carries the tool
	// name + a SYMBOL (not the basename); the matching tool_result on a LATER
	// line carries the basename but not the tool name. Pairing by tool_use_id
	// must suppress the violation.
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_tu-agent_tu-agent-graph__get_context","id":"toolu_X","input":{"target":"Store.Open"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_X","content":"symbol Store.Open in internal/graph/store/store.go; dependents ...","is_error":false}]}}`,
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if editWithoutContext(path, "internal/graph/store/store.go") {
		t.Error("symbol query paired by id must suppress the violation (got true)")
	}
}

func TestEditWithoutContext_SymbolQueryResultContentArray(t *testing.T) {
	// Same as above, but the tool_result content is an ARRAY of blocks, not a
	// string. Raw-bytes substring matching must still find the basename.
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_tu-agent_tu-agent-graph__get_impact","id":"toolu_Y","input":{"target":"Store.Open"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_Y","content":[{"type":"text","text":"impact of Store.Open in internal/graph/store/store.go"}]}]}}`,
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if editWithoutContext(path, "internal/graph/store/store.go") {
		t.Error("symbol query with array content must suppress the violation (got true)")
	}
}

func TestEditWithoutContext_UnrelatedResultStillFlags(t *testing.T) {
	// A context query whose result does NOT mention the edited file's basename
	// must still flag a violation (no false suppression).
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_tu-agent_tu-agent-graph__get_context","id":"toolu_Z","input":{"target":"Other.Thing"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_Z","content":"symbol Other.Thing in internal/other/other.go"}]}}`,
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !editWithoutContext(path, "internal/graph/store/store.go") {
		t.Error("context query on an unrelated symbol must still flag (got false)")
	}
}

func TestEditWithoutContext_OversizedLineFailsOpen(t *testing.T) {
	// A single line larger than the 4MB scanner buffer triggers a scanner
	// error; editWithoutContext must fail open (return false), never flag.
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	huge := strings.Repeat("x", 5*1024*1024)
	if err := os.WriteFile(path, []byte(huge+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if editWithoutContext(path, "internal/graph/store/store.go") {
		t.Error("oversized line must fail open (got true)")
	}
}

func TestEditCheckDecision_EmptyFilePathNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := editCheckDecision(strings.NewReader(`{"session_id":"s1","transcript_path":"whatever","tool_input":{"file_path":""}}`)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("empty file_path must not write a row, got: %s", data)
	}
}

func TestEditWithoutContext_EmptyOrMissingTranscript(t *testing.T) {
	if editWithoutContext("", "cmd/tu-agent/guard.go") {
		t.Error("empty transcriptPath must fail open (false)")
	}
	if editWithoutContext(filepath.Join(t.TempDir(), "missing.jsonl"), "cmd/tu-agent/guard.go") {
		t.Error("missing transcript file must fail open (false)")
	}
}

func TestEditCheckDecision_FullRecordsViolation(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	transcript := filepath.Join(root, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"message":{"content":[{"type":"text","text":"no tool use here"}]}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := fmt.Sprintf(`{"session_id":"s1","transcript_path":%q,"tool_input":{"file_path":"cmd/tu-agent/guard.go"}}`, transcript)
	if err := editCheckDecision(strings.NewReader(payload)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Event != telemetry.EventViolation {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventViolation)
	}
	if e.Outcome != "edit-without-context" {
		t.Errorf("Outcome = %q, want edit-without-context", e.Outcome)
	}
	if e.SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1", e.SessionID)
	}
}

func TestEditCheckDecision_ContextQueriedNoViolation(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	transcript := filepath.Join(root, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"tool_use":{"name":"mcp__plugin_tu-agent_tu-agent-graph__get_context","input":{"path":"guard.go"}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := fmt.Sprintf(`{"session_id":"s1","transcript_path":%q,"tool_input":{"file_path":"cmd/tu-agent/guard.go"}}`, transcript)
	if err := editCheckDecision(strings.NewReader(payload)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("expected no violation row when context was queried, got: %s", data)
	}
}

func TestEditCheckDecision_MinimalNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if err := editCheckDecision(strings.NewReader(`{"session_id":"s1","tool_input":{"file_path":"x.go"}}`)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not write a row, got: %s", data)
	}
}

func TestEditCheckDecision_BadJSONNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := editCheckDecision(strings.NewReader("not json")); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("bad json must not write a row, got: %s", data)
	}
}
