package stats_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// TestSummarize_ModelCostSurvivesOmitemptySchema is the @s3 green-guard: a
// model-call row with non-zero tokens and non-zero cost must round-trip through
// the omitempty schema and still be summarized. This passes both before and
// after the omitempty change (omitempty only drops zero values, and these are
// non-zero); it exists to prove omitempty did not drop real cost data.
func TestSummarize_ModelCostSurvivesOmitemptySchema(t *testing.T) {
	in := telemetry.Entry{
		Timestamp:    time.Unix(0, 0).UTC(),
		Provider:     "claude",
		Model:        "m",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.00105,
		LatencyMS:    1234,
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := stats.ReadEntries(path)
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	s := stats.Summarize(entries)

	const tol = 1e-9
	if math.Abs(s.TotalCostUSD-0.00105) > tol {
		t.Errorf("TotalCostUSD = %v, want 0.00105", s.TotalCostUSD)
	}
	ps := s.ByProvider["claude"]
	if ps == nil {
		t.Fatal("expected ByProvider[claude] to be non-nil")
	}
	if ps.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", ps.InputTokens)
	}
	if ps.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", ps.OutputTokens)
	}
	if math.Abs(ps.TotalCostUSD-0.00105) > tol {
		t.Errorf("provider TotalCostUSD = %v, want 0.00105", ps.TotalCostUSD)
	}
}
