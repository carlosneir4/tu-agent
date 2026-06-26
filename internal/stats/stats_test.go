package stats_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/stats"
	"github.com/tu/tu-agent/internal/telemetry"
)

func TestReadEntries_NoFile(t *testing.T) {
	entries, err := stats.ReadEntries("/nonexistent/telemetry.jsonl")
	if err != nil {
		t.Fatalf("ReadEntries() on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing file, got %d", len(entries))
	}
}

func TestReadEntries_ValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	lines := `{"timestamp":"2026-01-01T00:00:00Z","provider":"claude","model":"claude-sonnet-4-6","input_tokens":100,"output_tokens":50,"latency_ms":500,"cost_usd":0.001,"tool_calls_count":1}
{"timestamp":"2026-01-01T01:00:00Z","provider":"local","model":"local","input_tokens":200,"output_tokens":80,"latency_ms":200,"cost_usd":0.0002,"tool_calls_count":0}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := stats.ReadEntries(path)
	if err != nil {
		t.Fatalf("ReadEntries() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Provider != "claude" {
		t.Errorf("entries[0].Provider = %q, want %q", entries[0].Provider, "claude")
	}
	if entries[1].Provider != "local" {
		t.Errorf("entries[1].Provider = %q, want %q", entries[1].Provider, "local")
	}
}

func TestReadEntries_SkipsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	lines := `{"timestamp":"2026-01-01T00:00:00Z","provider":"claude","model":"m","input_tokens":10,"output_tokens":5,"latency_ms":100,"cost_usd":0.0001,"tool_calls_count":0}
not valid json
{"timestamp":"2026-01-02T00:00:00Z","provider":"local","model":"m","input_tokens":20,"output_tokens":8,"latency_ms":200,"cost_usd":0.0002,"tool_calls_count":0}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := stats.ReadEntries(path)
	if err != nil {
		t.Fatalf("ReadEntries() error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (malformed line skipped), got %d", len(entries))
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := stats.Summarize(nil)
	if s.TotalCalls != 0 {
		t.Errorf("TotalCalls = %d, want 0", s.TotalCalls)
	}
	if s.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD = %f, want 0", s.TotalCostUSD)
	}
	if len(s.ByProvider) != 0 {
		t.Errorf("ByProvider should be empty, got %v", s.ByProvider)
	}
}

func TestSummarize_SingleProvider(t *testing.T) {
	entries := []telemetry.Entry{
		{Provider: "claude", InputTokens: 100, OutputTokens: 50, LatencyMS: 500, CostUSD: 0.001, Timestamp: time.Now()},
		{Provider: "claude", InputTokens: 200, OutputTokens: 80, LatencyMS: 300, CostUSD: 0.002, Timestamp: time.Now()},
	}
	s := stats.Summarize(entries)
	if s.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d, want 2", s.TotalCalls)
	}
	ps := s.ByProvider["claude"]
	if ps == nil {
		t.Fatal("expected ByProvider[claude] to be non-nil")
	}
	if ps.Calls != 2 {
		t.Errorf("claude Calls = %d, want 2", ps.Calls)
	}
	if ps.InputTokens != 300 {
		t.Errorf("claude InputTokens = %d, want 300", ps.InputTokens)
	}
	if ps.OutputTokens != 130 {
		t.Errorf("claude OutputTokens = %d, want 130", ps.OutputTokens)
	}
}

func TestSummarize_MultipleProviders(t *testing.T) {
	entries := []telemetry.Entry{
		{Provider: "claude", InputTokens: 100, LatencyMS: 500, CostUSD: 0.001, Timestamp: time.Now()},
		{Provider: "local", InputTokens: 50, LatencyMS: 100, CostUSD: 0.0001, Timestamp: time.Now()},
	}
	s := stats.Summarize(entries)
	if len(s.ByProvider) != 2 {
		t.Errorf("expected 2 providers, got %d", len(s.ByProvider))
	}
	if s.ByProvider["claude"] == nil || s.ByProvider["local"] == nil {
		t.Error("expected both providers in ByProvider")
	}
}

func TestSummarize_AvgLatency(t *testing.T) {
	entries := []telemetry.Entry{
		{Provider: "claude", LatencyMS: 400, Timestamp: time.Now()},
		{Provider: "claude", LatencyMS: 600, Timestamp: time.Now()},
	}
	s := stats.Summarize(entries)
	avg := s.ByProvider["claude"].AvgLatencyMS()
	if avg != 500.0 {
		t.Errorf("AvgLatencyMS() = %f, want 500.0", avg)
	}
}

func TestProviderStats_AvgLatency_ZeroCalls(t *testing.T) {
	ps := &stats.ProviderStats{}
	if ps.AvgLatencyMS() != 0 {
		t.Errorf("AvgLatencyMS() on zero calls = %f, want 0", ps.AvgLatencyMS())
	}
}

func TestSummarize_SkipsEventRows(t *testing.T) {
	entries := []telemetry.Entry{
		{Provider: "claude", InputTokens: 100, OutputTokens: 50, CostUSD: 0.001, Timestamp: time.Now()},
		{Event: "load_skill", Skill: "demo", Found: true, Timestamp: time.Now()},
	}
	s := stats.Summarize(entries)
	if s.TotalCalls != 1 {
		t.Errorf("TotalCalls = %d, want 1 (event rows must be skipped)", s.TotalCalls)
	}
}
