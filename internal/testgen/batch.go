package testgen

import (
	"context"

	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// BatchItem is one target's outcome in a batch run. Result is nil only when
// the target could not be set up (e.g. no adapter for its language); Err then
// holds why.
type BatchItem struct {
	Target Target
	Result *Result
	Err    error
}

// BatchReport aggregates a batch run. The counters partition Items: every item
// increments exactly one of Passed / FIXMEd / Discarded / Errored.
type BatchReport struct {
	Items     []BatchItem
	Passed    int
	FIXMEd    int
	Discarded int
	Errored   int
}

// GenerateBatch generates a test for each target, reusing the shared provider,
// telemetry, graph, and runner but building the correct adapter per target so a
// mixed-language batch works. One failing target never aborts the batch
// (spec §7.2): its outcome is recorded and the loop continues.
func GenerateBatch(ctx context.Context, g *query.Graph, prov provider.Provider, tel *telemetry.Logger, run Runner, targets []Target, opts Options) BatchReport {
	rep := BatchReport{Items: make([]BatchItem, 0, len(targets))}
	for _, t := range targets {
		item := BatchItem{Target: t}
		ad, err := AdapterFor(t.Language)
		if err != nil {
			item.Err = err
			rep.Errored++
			rep.Items = append(rep.Items, item)
			continue
		}
		p := Pipeline{Graph: g, Adapter: ad, Provider: prov, Tel: tel, Run: run}
		res, genErr := p.Generate(ctx, t, opts)
		item.Result, item.Err = res, genErr
		switch {
		case res != nil && res.Passed:
			rep.Passed++
		case res != nil && res.FIXME:
			rep.FIXMEd++
		case res != nil && res.Discarded:
			rep.Discarded++
		default:
			rep.Errored++
		}
		rep.Items = append(rep.Items, item)
	}
	return rep
}
