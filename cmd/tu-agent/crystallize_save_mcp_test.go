package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

func TestHandleCrystallizeSave_StoresAndMaterializes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { memCrystallizeMin = 5 })
	memCrystallizeMin = 3
	seedCluster(t) // from crystallize_gen_test.go

	_, out, err := handleCrystallizeSave(context.Background(), nil,
		crystallizeSaveMCPInput{Label: "checkout", Body: "---\nname: checkout\n---\nbody"})
	if err != nil {
		t.Fatalf("handleCrystallizeSave: %v", err)
	}
	if !strings.Contains(out.Result, "checkout") {
		t.Errorf("expected the saved path in the result, got: %q", out.Result)
	}
	ms, _ := memory.Open(memoryDBPath(repoRoot()))
	defer ms.Close()
	obs, _ := ms.List()
	found := false
	for _, o := range obs {
		if o.TopicKey == crystallize.SkillTopic("checkout") && o.Type == "skill" {
			found = true
		}
	}
	if !found {
		t.Error("crystallize_save did not store the skill/checkout record")
	}
}

func TestCrystallizeSaveInMCPToolNames(t *testing.T) {
	found := false
	for _, n := range mcpToolNames {
		if n == "crystallize_save" {
			found = true
		}
	}
	if !found {
		t.Error("mcpToolNames missing crystallize_save")
	}
}
