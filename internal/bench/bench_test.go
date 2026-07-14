package bench_test

import (
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/bench"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

func entry(provider string, in, out int, cost float64) telemetry.Entry {
	return telemetry.Entry{
		Timestamp:    time.Now(),
		Provider:     provider,
		Model:        "test-model",
		InputTokens:  in,
		OutputTokens: out,
		CostUSD:      cost,
	}
}

func TestCompare_TokenDelta(t *testing.T) {
	baseline := []telemetry.Entry{
		entry("claude", 1000, 500, 0.003),
		entry("claude", 2000, 800, 0.006),
	}
	candidate := []telemetry.Entry{
		entry("local", 600, 300, 0.0),
		entry("local", 1200, 400, 0.0),
	}

	r := bench.Compare(baseline, candidate)

	if r.BaselineTokens != 4300 {
		t.Errorf("BaselineTokens = %d, want 4300", r.BaselineTokens)
	}
	if r.CompareTokens != 2500 {
		t.Errorf("CompareTokens = %d, want 2500", r.CompareTokens)
	}
	// delta = (2500 - 4300) / 4300 * 100 ≈ -41.86%
	if r.TokenDeltaPct > -40 || r.TokenDeltaPct < -43 {
		t.Errorf("TokenDeltaPct = %.2f, want around -41.86", r.TokenDeltaPct)
	}
}

func TestCompare_CostDelta(t *testing.T) {
	baseline := []telemetry.Entry{entry("claude", 1000, 500, 0.010)}
	candidate := []telemetry.Entry{entry("local", 1000, 500, 0.0)}

	r := bench.Compare(baseline, candidate)

	if r.BaselineCostUSD != 0.010 {
		t.Errorf("BaselineCostUSD = %f, want 0.010", r.BaselineCostUSD)
	}
	if r.CompareCostUSD != 0.0 {
		t.Errorf("CompareCostUSD = %f, want 0.0", r.CompareCostUSD)
	}
	if r.CostDeltaPct != -100.0 {
		t.Errorf("CostDeltaPct = %f, want -100.0", r.CostDeltaPct)
	}
}

func TestCompare_ZeroBaseline(t *testing.T) {
	r := bench.Compare(nil, nil)
	if r.TokenDeltaPct != 0 || r.CostDeltaPct != 0 {
		t.Errorf("expected zero deltas for empty input, got token=%f cost=%f",
			r.TokenDeltaPct, r.CostDeltaPct)
	}
}
