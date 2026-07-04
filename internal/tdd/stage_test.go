package tdd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDispatcher struct {
	reply string
	err   error
	gotAg string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, agent, _ string) (string, error) {
	f.gotAg = agent
	return f.reply, f.err
}

func TestStageRunnerRun(t *testing.T) {
	fd := &fakeDispatcher{reply: "done\n```json\n{\"stage\":\"architect\",\"status\":\"pass\"}\n```"}
	c, err := StageRunner{D: fd}.Run(context.Background(), "architect", "task")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fd.gotAg != "architect" {
		t.Fatalf("dispatched %q, want architect", fd.gotAg)
	}
	if c.Status != StatusPass {
		t.Fatalf("status = %q", c.Status)
	}
}

func TestStageRunnerDispatchError(t *testing.T) {
	fd := &fakeDispatcher{err: errors.New("boom")}
	if _, err := (StageRunner{D: fd}).Run(context.Background(), "judge", "t"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestToolGrants(t *testing.T) {
	if len(DefaultToolGrant) == 0 {
		t.Fatal("DefaultToolGrant must be non-empty")
	}
	hasWrite := false
	for _, n := range CraftsmanToolGrant {
		if n == "write_file" {
			hasWrite = true
		}
	}
	if !hasWrite {
		t.Fatal("CraftsmanToolGrant must include write_file")
	}
}

func TestArchitectOverlayMultiFeature(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "features") {
		t.Error("architect overlay must describe the features list")
	}
	if !strings.Contains(ArchitectPrompt, "one .feature per") && !strings.Contains(ArchitectPrompt, "one feature file per") {
		t.Error("architect overlay must tell complex to write one feature file per sub-feature")
	}
}

func TestOverlaysGraphAndPreload(t *testing.T) {
	if !strings.Contains(ArchitectPrompt, "MUST consult the graph") {
		t.Error("architect overlay must mandate graph blast-radius consultation")
	}
	if !strings.Contains(ArchitectPrompt, "WIDE blast-radius") || !strings.Contains(ArchitectPrompt, "DECOMPOSE") {
		t.Error("architect overlay must keep the complex-decompose sizing heuristic")
	}
	if !strings.Contains(AnalystPrompt, "BEFORE your first question") {
		t.Error("analyst overlay must mandate pre-loading context before interrogating")
	}
}

func TestOverlaysAreGenericContractOnly(t *testing.T) {
	overlays := map[string]string{
		"analyst": AnalystPrompt, "architect": ArchitectPrompt,
		"craftsman": CraftsmanPrompt, "judge": JudgePrompt, "scribe": ScribePrompt,
	}
	for name, o := range overlays {
		if !strings.Contains(o, "tu-agent TDD task") {
			t.Errorf("%s overlay must open by naming the tu-agent TDD task", name)
		}
		// The durable role identity now lives in the agent body, not the overlay.
		if strings.Contains(o, "senior software architect") || strings.Contains(o, "You are a senior") {
			t.Errorf("%s overlay must not carry role identity", name)
		}
	}
	if !strings.Contains(ArchitectPrompt, "@s1") {
		t.Error("architect overlay must keep the Gherkin @s contract")
	}
	if !strings.Contains(CraftsmanPrompt, "Red -> Green") {
		t.Error("craftsman overlay must keep the red-green discipline")
	}
}

func TestAnalystPromptSeedsFromDesign(t *testing.T) {
	for _, want := range []string{"design doc", "confirm by exception"} {
		if !strings.Contains(AnalystPrompt, want) {
			t.Errorf("AnalystPrompt missing %q", want)
		}
	}
}

func TestSandwichOverlays(t *testing.T) {
	if !strings.Contains(TestWriterPrompt, "NO production") {
		t.Error("TestWriterPrompt must forbid production code")
	}
	if !strings.Contains(ImplementerPrompt, "do NOT modify") {
		t.Error("ImplementerPrompt must forbid touching tests")
	}
	for _, p := range []string{TestWriterPrompt, ImplementerPrompt} {
		if !strings.Contains(p, "```json") {
			t.Error("overlay missing contract instruction")
		}
	}
}

func TestArchitectPromptConsultsCoverage(t *testing.T) {
	for _, want := range []string{"test gaps", "already covered"} {
		if !strings.Contains(ArchitectPrompt, want) {
			t.Errorf("ArchitectPrompt missing %q", want)
		}
	}
}
