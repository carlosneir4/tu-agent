package bench

import (
	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// Report holds the A/B comparison between a baseline run and a candidate run.
type Report struct {
	BaselineCalls   int
	CompareCalls    int
	BaselineTokens  int
	CompareTokens   int
	TokenDeltaPct   float64
	BaselineCostUSD float64
	CompareCostUSD  float64
	CostDeltaPct    float64
}

// Compare builds a Report from two slices of telemetry entries.
func Compare(baseline, candidate []telemetry.Entry) Report {
	b := stats.Summarize(baseline)
	c := stats.Summarize(candidate)

	bTok := totalTokens(b)
	cTok := totalTokens(c)

	tokenDelta := 0.0
	if bTok > 0 {
		tokenDelta = (float64(cTok) - float64(bTok)) / float64(bTok) * 100
	}
	costDelta := 0.0
	if b.TotalCostUSD > 0 {
		costDelta = (c.TotalCostUSD - b.TotalCostUSD) / b.TotalCostUSD * 100
	}

	return Report{
		BaselineCalls:   b.TotalCalls,
		CompareCalls:    c.TotalCalls,
		BaselineTokens:  bTok,
		CompareTokens:   cTok,
		TokenDeltaPct:   tokenDelta,
		BaselineCostUSD: b.TotalCostUSD,
		CompareCostUSD:  c.TotalCostUSD,
		CostDeltaPct:    costDelta,
	}
}

func totalTokens(s stats.Summary) int {
	n := 0
	for _, ps := range s.ByProvider {
		n += ps.InputTokens + ps.OutputTokens
	}
	return n
}
