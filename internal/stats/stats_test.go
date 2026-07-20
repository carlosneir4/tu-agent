package stats_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
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

func TestSummarizeInsights_MixedRows(t *testing.T) {
	entries := []telemetry.Entry{
		// model rows and load_skill rows are ignored.
		{Provider: "claude", CostUSD: 0.01, Timestamp: time.Now()},
		{Event: telemetry.EventLoadSkill, Skill: "demo", Found: true, Timestamp: time.Now()},
		// mcp_call rows.
		{Event: telemetry.EventMCPCall, Tool: "mem_search", DurationMS: 100, ResultBytes: 50, ZeroResult: true, Timestamp: time.Now()},
		{Event: telemetry.EventMCPCall, Tool: "mem_search", DurationMS: 200, ResultBytes: 150, ZeroResult: false, Timestamp: time.Now()},
		{Event: telemetry.EventMCPCall, Tool: "get_context", DurationMS: 50, ResultBytes: 300, Timestamp: time.Now()},
		// graph_refresh rows.
		{Event: telemetry.EventGraphRefresh, Parsed: 5, Unchanged: 2, Deleted: 1, Failed: 0, Timestamp: time.Now()},
		{Event: telemetry.EventGraphRefresh, Parsed: 10, Unchanged: 3, Deleted: 4, Failed: 2, Timestamp: time.Now().Add(time.Minute)},
		// hook rows.
		{Event: telemetry.EventHook, Tool: "graph update", DurationMS: 30, OK: true, Timestamp: time.Now()},
		{Event: telemetry.EventHook, Tool: "memory relink", DurationMS: 70, OK: false, Timestamp: time.Now()},
	}

	got := stats.SummarizeInsights(entries)

	memSearch := got.Tools["mem_search"]
	if memSearch == nil {
		t.Fatal("expected Tools[mem_search] to be non-nil")
	}
	if memSearch.Calls != 2 {
		t.Errorf("mem_search Calls = %d, want 2", memSearch.Calls)
	}
	if memSearch.ZeroResults != 1 {
		t.Errorf("mem_search ZeroResults = %d, want 1", memSearch.ZeroResults)
	}
	if memSearch.TotalBytes != 200 {
		t.Errorf("mem_search TotalBytes = %d, want 200", memSearch.TotalBytes)
	}
	if memSearch.TotalDurMS != 300 {
		t.Errorf("mem_search TotalDurMS = %d, want 300", memSearch.TotalDurMS)
	}

	getContext := got.Tools["get_context"]
	if getContext == nil || getContext.Calls != 1 {
		t.Fatalf("expected Tools[get_context].Calls = 1, got %+v", getContext)
	}

	if got.GraphRefreshes != 2 {
		t.Errorf("GraphRefreshes = %d, want 2", got.GraphRefreshes)
	}
	if got.GraphFailedFiles != 2 {
		t.Errorf("GraphFailedFiles = %d, want 2", got.GraphFailedFiles)
	}
	// Last graph_refresh row (by iteration order) sets GraphLast*.
	if got.GraphLastParsed != 10 {
		t.Errorf("GraphLastParsed = %d, want 10", got.GraphLastParsed)
	}
	if got.GraphLastDeleted != 4 {
		t.Errorf("GraphLastDeleted = %d, want 4", got.GraphLastDeleted)
	}

	if got.HookCalls != 2 {
		t.Errorf("HookCalls = %d, want 2", got.HookCalls)
	}
	if got.HookFailures != 1 {
		t.Errorf("HookFailures = %d, want 1", got.HookFailures)
	}
	wantDurs := []int64{30, 70}
	if len(got.HookDurationsMS) != len(wantDurs) {
		t.Fatalf("HookDurationsMS = %v, want %v", got.HookDurationsMS, wantDurs)
	}
	for i, d := range wantDurs {
		if got.HookDurationsMS[i] != d {
			t.Errorf("HookDurationsMS[%d] = %d, want %d", i, got.HookDurationsMS[i], d)
		}
	}
}

func TestSummarizeInsights_ViolationsAndPrompts(t *testing.T) {
	entries := []telemetry.Entry{
		{Event: telemetry.EventViolation, Outcome: "secret-guard", Timestamp: time.Now()},
		{Event: telemetry.EventViolation, Outcome: "secret-guard", Timestamp: time.Now()},
		{Event: telemetry.EventViolation, Outcome: "edit-without-context", Timestamp: time.Now()},
		{Event: telemetry.EventPrompt, SessionID: "s1", Timestamp: time.Now()},
		{Event: telemetry.EventPrompt, SessionID: "s1", Timestamp: time.Now()},
		{Event: telemetry.EventPrompt, SessionID: "s2", Timestamp: time.Now()},
	}

	got := stats.SummarizeInsights(entries)

	if got.Violations["secret-guard"] != 2 {
		t.Errorf("Violations[secret-guard] = %d, want 2", got.Violations["secret-guard"])
	}
	if got.Violations["edit-without-context"] != 1 {
		t.Errorf("Violations[edit-without-context] = %d, want 1", got.Violations["edit-without-context"])
	}
	if got.Prompts != 3 {
		t.Errorf("Prompts = %d, want 3", got.Prompts)
	}
	if got.PromptSessions != 2 {
		t.Errorf("PromptSessions = %d, want 2", got.PromptSessions)
	}
}

// TestSummarizeInsights_PromptSessionsSkipsEmptySessionID guards against an
// empty session_id (a UserPromptSubmit payload that omits it) counting as a
// distinct prompt session and inflating PromptSessions. Empty-SessionID prompt
// rows still count toward Prompts (the total), just not toward the distinct-
// session proxy.
func TestSummarizeInsights_PromptSessionsSkipsEmptySessionID(t *testing.T) {
	entries := []telemetry.Entry{
		{Event: telemetry.EventPrompt, SessionID: "", Timestamp: time.Now()},
		{Event: telemetry.EventPrompt, SessionID: "", Timestamp: time.Now()},
		{Event: telemetry.EventPrompt, SessionID: "s1", Timestamp: time.Now()},
	}

	got := stats.SummarizeInsights(entries)

	if got.Prompts != 3 {
		t.Errorf("Prompts = %d, want 3 (empty SessionID still counts as a prompt)", got.Prompts)
	}
	if got.PromptSessions != 1 {
		t.Errorf("PromptSessions = %d, want 1 (empty SessionID must not count as a distinct session)", got.PromptSessions)
	}
}

func TestSummarizeInsights_Empty(t *testing.T) {
	got := stats.SummarizeInsights(nil)
	if len(got.Tools) != 0 {
		t.Errorf("Tools should be empty, got %v", got.Tools)
	}
	if got.GraphRefreshes != 0 || got.HookCalls != 0 {
		t.Errorf("expected zero counts, got %+v", got)
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name string
		xs   []int64
		p    float64
		want int64
	}{
		{name: "empty", xs: nil, p: 50, want: 0},
		{name: "single value any percentile", xs: []int64{42}, p: 99, want: 42},
		{name: "p50 of five values", xs: []int64{10, 20, 30, 40, 50}, p: 50, want: 30},
		{name: "p95 of five values", xs: []int64{10, 20, 30, 40, 50}, p: 95, want: 50},
		{name: "unsorted input is sorted first", xs: []int64{50, 10, 30, 20, 40}, p: 50, want: 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stats.Percentile(tt.xs, tt.p); got != tt.want {
				t.Errorf("Percentile(%v, %v) = %d, want %d", tt.xs, tt.p, got, tt.want)
			}
		})
	}
}

func TestSummarizeFlow_Empty(t *testing.T) {
	s := stats.SummarizeFlow(nil)
	if len(s.Features) != 0 {
		t.Errorf("Features should be empty, got %v", s.Features)
	}
	if s.ReviewStage != "" || s.ReviewOutcome != "" {
		t.Errorf("ReviewStage/ReviewOutcome should be empty, got %q/%q", s.ReviewStage, s.ReviewOutcome)
	}
}

func TestSummarizeFlow_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		entries           []telemetry.Entry
		wantFeatureNames  []string
		checkFeature      string
		wantRed           int
		wantGreen         int
		wantFailures      map[string]int
		wantFinal         string
		wantReviewStage   string
		wantReviewOutcome string
	}{
		{
			name: "gate_attempt rows bucket red/green attempts and failures by outcome",
			entries: []telemetry.Entry{
				{Event: telemetry.EventGateAttempt, Feature: "login-form", Stage: "red", OK: true},
				{Event: telemetry.EventGateAttempt, Feature: "login-form", Stage: "red", Outcome: "test_failed", OK: false},
				{Event: telemetry.EventGateAttempt, Feature: "login-form", Stage: "green", Outcome: "build_failed", OK: false},
			},
			wantFeatureNames: []string{"login-form"},
			checkFeature:     "login-form",
			wantRed:          2,
			wantGreen:        1,
			wantFailures:     map[string]int{"test_failed": 1, "build_failed": 1},
			wantFinal:        "",
		},
		{
			name: "tdd_stage mark rows are last-row-wins for FinalStatus",
			entries: []telemetry.Entry{
				{Event: telemetry.EventTddStage, Feature: "login-form", Stage: "mark", Outcome: "pending"},
				{Event: telemetry.EventTddStage, Feature: "login-form", Stage: "mark", Outcome: "pass"},
			},
			wantFeatureNames: []string{"login-form"},
			checkFeature:     "login-form",
			wantFinal:        "pass",
		},
		{
			name: "run-level review rows are last-row-wins across review and branch-review",
			entries: []telemetry.Entry{
				{Event: telemetry.EventTddStage, Stage: "review", Outcome: "skipped"},
				{Event: telemetry.EventTddStage, Stage: "branch-review", Outcome: "critical:0,important:1"},
			},
			wantFeatureNames:  nil,
			wantReviewStage:   "branch-review",
			wantReviewOutcome: "critical:0,important:1",
		},
		{
			name: "tdd_stage begin rows are recognized and skipped",
			entries: []telemetry.Entry{
				{Event: telemetry.EventTddStage, Feature: "login-form", Stage: "begin", Outcome: "n/a"},
			},
			wantFeatureNames: nil,
		},
		{
			name: "rows with empty Feature never create a feature bucket",
			entries: []telemetry.Entry{
				{Event: telemetry.EventGateAttempt, Feature: "", Stage: "red", OK: true},
				{Event: telemetry.EventTddStage, Feature: "", Stage: "mark", Outcome: "pass"},
			},
			wantFeatureNames: nil,
		},
		{
			name: "non-flow events are ignored",
			entries: []telemetry.Entry{
				{Provider: "claude", CostUSD: 0.01},
				{Event: telemetry.EventLoadSkill, Skill: "login-form", Found: true},
				{Event: telemetry.EventMCPCall, Tool: "mem_search"},
			},
			wantFeatureNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stats.SummarizeFlow(tt.entries)

			gotNames := make([]string, 0, len(got.Features))
			for name := range got.Features {
				gotNames = append(gotNames, name)
			}
			sort.Strings(gotNames)
			if len(gotNames) != len(tt.wantFeatureNames) {
				t.Fatalf("Features keys = %v, want %v", gotNames, tt.wantFeatureNames)
			}
			for i, name := range tt.wantFeatureNames {
				if gotNames[i] != name {
					t.Errorf("Features keys = %v, want %v", gotNames, tt.wantFeatureNames)
				}
			}

			if tt.checkFeature != "" {
				ff := got.Features[tt.checkFeature]
				if ff == nil {
					t.Fatalf("expected Features[%q] to be non-nil", tt.checkFeature)
				}
				if ff.RedAttempts != tt.wantRed {
					t.Errorf("RedAttempts = %d, want %d", ff.RedAttempts, tt.wantRed)
				}
				if ff.GreenAttempts != tt.wantGreen {
					t.Errorf("GreenAttempts = %d, want %d", ff.GreenAttempts, tt.wantGreen)
				}
				for reason, count := range tt.wantFailures {
					if ff.Failures[reason] != count {
						t.Errorf("Failures[%q] = %d, want %d", reason, ff.Failures[reason], count)
					}
				}
				if ff.FinalStatus != tt.wantFinal {
					t.Errorf("FinalStatus = %q, want %q", ff.FinalStatus, tt.wantFinal)
				}
			}

			if got.ReviewStage != tt.wantReviewStage {
				t.Errorf("ReviewStage = %q, want %q", got.ReviewStage, tt.wantReviewStage)
			}
			if got.ReviewOutcome != tt.wantReviewOutcome {
				t.Errorf("ReviewOutcome = %q, want %q", got.ReviewOutcome, tt.wantReviewOutcome)
			}
		})
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
