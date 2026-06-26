package testgen

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func TestGenerateBatchMixedOutcomes(t *testing.T) {
	g, root, tgt := contextFixture(t)
	writeFiles(t, root, "go.mod")

	// Two targets: the fixture's Save target generated twice. The runner scripts
	// the first target's run to pass and the second to fail through the repair
	// budget, so we get one Passed and one FIXMEd in the same batch.
	run, _ := scriptedRunner(t, []runResult{
		{out: "ok  \tstore\t0.01s", err: nil}, // target 1 attempt 1 → pass
		{out: "--- FAIL", err: errBoom},       // target 2 attempt 1 → fail
		{out: "--- FAIL", err: errBoom},       // target 2 attempt 2 → fail
		{out: "--- FAIL", err: errBoom},       // target 2 attempt 3 → fail
	})
	prov := &fakeProvider{responses: []string{cannedTest}}

	targets := []Target{tgt, tgt}
	rep := GenerateBatch(context.Background(), g, prov, nil, run, targets,
		Options{RepoRoot: root, MaxRepair: 2})

	if len(rep.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(rep.Items))
	}
	if rep.Passed != 1 || rep.FIXMEd != 1 {
		t.Fatalf("report = %+v, want 1 passed 1 FIXME", rep)
	}
	if rep.Items[0].Err != nil || !rep.Items[0].Result.Passed {
		t.Errorf("item 0 should have passed: %+v", rep.Items[0])
	}
	if !rep.Items[1].Result.FIXME {
		t.Errorf("item 1 should be FIXME: %+v", rep.Items[1])
	}
}

func TestGenerateBatchAdapterError(t *testing.T) {
	g, root, tgt := contextFixture(t)
	writeFiles(t, root, "go.mod")
	bad := tgt
	bad.Language = "cobol" // no adapter

	rep := GenerateBatch(context.Background(), g, &fakeProvider{responses: []string{cannedTest}},
		nil, func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
			return "ok", nil
		}, []Target{bad}, Options{RepoRoot: root, MaxRepair: 0})

	if rep.Errored != 1 || rep.Items[0].Err == nil {
		t.Fatalf("adapter error should count as Errored: %+v", rep)
	}
}
