package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// ProviderStats aggregates telemetry for a single provider.
type ProviderStats struct {
	Calls        int
	InputTokens  int
	OutputTokens int
	TotalCostUSD float64
	TotalLatMS   int64
}

// AvgLatencyMS returns the mean latency across all calls, or 0 for no calls.
func (p *ProviderStats) AvgLatencyMS() float64 {
	if p.Calls == 0 {
		return 0
	}
	return float64(p.TotalLatMS) / float64(p.Calls)
}

// Summary holds aggregated statistics from a set of telemetry entries.
type Summary struct {
	TotalCalls   int
	TotalCostUSD float64
	ByProvider   map[string]*ProviderStats
}

// ReadEntries reads telemetry entries from a JSONL file.
// If the file does not exist, nil entries and no error are returned.
// Malformed lines are silently skipped.
func ReadEntries(path string) ([]telemetry.Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stats.ReadEntries: open %s: %w", path, err)
	}
	defer f.Close()

	var entries []telemetry.Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e telemetry.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return entries, fmt.Errorf("stats.ReadEntries: scan %s: %w", path, err)
	}
	return entries, nil
}

// Summarize aggregates a slice of telemetry entries into a Summary.
// Non-model event rows (e.g. load_skill) are skipped.
func Summarize(entries []telemetry.Entry) Summary {
	s := Summary{ByProvider: make(map[string]*ProviderStats)}
	for _, e := range entries {
		if e.Event != "" {
			continue // non-model event rows (e.g. load_skill) are not usage
		}
		s.TotalCalls++
		s.TotalCostUSD += e.CostUSD
		ps, ok := s.ByProvider[e.Provider]
		if !ok {
			ps = &ProviderStats{}
			s.ByProvider[e.Provider] = ps
		}
		ps.Calls++
		ps.InputTokens += e.InputTokens
		ps.OutputTokens += e.OutputTokens
		ps.TotalCostUSD += e.CostUSD
		ps.TotalLatMS += e.LatencyMS
	}
	return s
}

// ToolInsight aggregates mcp_call rows for a single tool.
type ToolInsight struct {
	Calls       int
	ZeroResults int // mem_search rows with ZeroResult
	TotalBytes  int
	TotalDurMS  int64
}

// InsightsSummary aggregates event rows (mcp_call, graph_refresh, hook) for
// the diagnostic `stats --insights` report.
type InsightsSummary struct {
	Tools            map[string]*ToolInsight // mcp_call rows keyed by Tool
	GraphRefreshes   int
	GraphFailedFiles int // sum of Failed across graph_refresh rows
	GraphLastParsed  int // Parsed of the last graph_refresh row
	GraphLastDeleted int
	HookCalls        int
	HookFailures     int
	HookDurationsMS  []int64 // DurationMS of hook rows, for percentiles

	// Violations buckets violation rows by Outcome (e.g. "secret-guard",
	// "edit-without-context").
	Violations map[string]int
	// Prompts counts prompt rows (one per UserPromptSubmit hook firing).
	Prompts int
	// PromptSessions counts distinct SessionID among prompt rows — a proxy
	// for how many sessions generated friction signals.
	PromptSessions int
	// GateFailures buckets gate_attempt rows with OK=false by Outcome reason
	// (e.g. "build_failed", "suite_failing", "runner_error").
	GateFailures map[string]int
}

// SummarizeInsights aggregates a slice of telemetry entries into an
// InsightsSummary. It is a separate aggregator from Summarize (which covers
// model-cost rows only) — see the event rows this covers: mcp_call,
// graph_refresh, hook. Other rows (model calls, load_skill, ...) are ignored.
func SummarizeInsights(entries []telemetry.Entry) InsightsSummary {
	s := InsightsSummary{Tools: make(map[string]*ToolInsight), Violations: make(map[string]int), GateFailures: make(map[string]int)}
	promptSessions := make(map[string]bool)
	for _, e := range entries {
		switch e.Event {
		case telemetry.EventMCPCall:
			ti, ok := s.Tools[e.Tool]
			if !ok {
				ti = &ToolInsight{}
				s.Tools[e.Tool] = ti
			}
			ti.Calls++
			ti.TotalBytes += e.ResultBytes
			ti.TotalDurMS += e.DurationMS
			if e.ZeroResult {
				ti.ZeroResults++
			}
		case telemetry.EventGraphRefresh:
			s.GraphRefreshes++
			s.GraphFailedFiles += e.Failed
			s.GraphLastParsed = e.Parsed
			s.GraphLastDeleted = e.Deleted
		case telemetry.EventHook:
			s.HookCalls++
			s.HookDurationsMS = append(s.HookDurationsMS, e.DurationMS)
			if !e.OK {
				s.HookFailures++
			}
		case telemetry.EventViolation:
			s.Violations[e.Outcome]++
		case telemetry.EventGateAttempt:
			if !e.OK {
				s.GateFailures[e.Outcome]++
			}
		case telemetry.EventPrompt:
			s.Prompts++
			// A prompt row with no session_id (an omitted payload field) must
			// not count as a distinct session — that would inflate
			// PromptSessions by one for the whole empty-id bucket.
			if e.SessionID != "" {
				promptSessions[e.SessionID] = true
			}
		default:
			// model rows (Event == "") and other event kinds (load_skill, ...)
			// are not measurement-insights data.
		}
	}
	s.PromptSessions = len(promptSessions)
	return s
}

// FeatureFlow is one tdd feature's funnel: gate attempts, failures, final mark.
type FeatureFlow struct {
	RedAttempts   int            // gate_attempt rows with Stage "red"
	GreenAttempts int            // gate_attempt rows with Stage "green"
	Failures      map[string]int // gate_attempt rows with OK=false, by Outcome reason
	FinalStatus   string         // Outcome of the LAST tdd_stage "mark" row; "" = none
}

// FlowSummary is the per-feature tdd funnel plus run-level review outcome.
type FlowSummary struct {
	Features      map[string]*FeatureFlow // keyed by Entry.Feature (non-empty only)
	ReviewStage   string                  // Stage of the last run-level review row: "review" | "branch-review" | ""
	ReviewOutcome string                  // Outcome of that row ("pass", "skipped", "critical:0,important:1", ...)
}

// SummarizeFlow aggregates a slice of telemetry entries into a FlowSummary.
// It is a third aggregator alongside Summarize and SummarizeInsights and does
// not modify their behavior. Only gate_attempt and tdd_stage rows are read;
// everything else (model rows, load_skill, mcp_call, prompt, ...) is ignored.
func SummarizeFlow(entries []telemetry.Entry) FlowSummary {
	s := FlowSummary{Features: make(map[string]*FeatureFlow)}
	for _, e := range entries {
		switch e.Event {
		case telemetry.EventGateAttempt:
			if e.Feature == "" {
				continue // defensive; the emitter always sets it
			}
			ff, ok := s.Features[e.Feature]
			if !ok {
				ff = &FeatureFlow{Failures: make(map[string]int)}
				s.Features[e.Feature] = ff
			}
			switch e.Stage {
			case "red":
				ff.RedAttempts++
			case "green":
				ff.GreenAttempts++
			}
			if !e.OK {
				ff.Failures[e.Outcome]++
			}
		case telemetry.EventTddStage:
			switch e.Stage {
			case "mark":
				if e.Feature == "" {
					continue
				}
				ff, ok := s.Features[e.Feature]
				if !ok {
					ff = &FeatureFlow{Failures: make(map[string]int)}
					s.Features[e.Feature] = ff
				}
				ff.FinalStatus = e.Outcome
			case "review", "branch-review":
				s.ReviewStage = e.Stage
				s.ReviewOutcome = e.Outcome
			default:
				// "begin" and other stages are recognized-and-skipped.
			}
		default:
			// non-flow events are not funnel data.
		}
	}
	return s
}

// Percentile returns the p-th percentile (0..100) of xs using the
// nearest-rank method, or 0 if xs is empty. xs is not mutated.
func Percentile(xs []int64, p float64) int64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	sorted := make([]int64, n)
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p / 100 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
