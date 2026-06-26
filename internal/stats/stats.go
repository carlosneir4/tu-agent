package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tu/tu-agent/internal/telemetry"
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
