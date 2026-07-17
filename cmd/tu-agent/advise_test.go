package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// writeAdviseTelemetry appends violation rows to the temp repo's telemetry
// log so advise's behavioral rules have evidence to evaluate.
func writeAdviseTelemetry(t *testing.T, root string, outcome string, n int) {
	t.Helper()
	lg, err := telemetry.NewLogger(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("telemetry.NewLogger: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := lg.Log(telemetry.Entry{Event: telemetry.EventViolation, Outcome: outcome}); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}
}

// seedCrystallizeCluster writes 3 topically-related notes (no skill record),
// so crystallizeNeeds counts one cluster needing crystallization.
func seedCrystallizeCluster(t *testing.T, root string) {
	t.Helper()
	ms, err := memory.Open(memoryDBPath(root))
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := ms.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestAdvisePlain_PrintsAllThresholdMeetingSuggestions(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	seedCrystallizeCluster(t, root)
	writeAdviseTelemetry(t, root, "secret-guard", 3)

	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"tu-agent memory crystallize", "secret-guard"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("plain advise output missing %q:\n%s", want, out)
		}
	}
}

func TestAdvisePlain_NoSuggestionsIsSilent(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output with no evidence, got: %q", buf.String())
	}
}

func TestAdvisePlain_DropsDismissedRule(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	seedCrystallizeCluster(t, root)

	if err := runAdviseDismiss(adviseCmd, "crystallize-ready"); err != nil {
		t.Fatalf("runAdviseDismiss: %v", err)
	}

	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("crystallize")) {
		t.Errorf("dismissed rule should not print, got: %q", buf.String())
	}
}

func TestAdviseNudge_DedupsAcrossRunsAndPersists(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5 })

	seedCrystallizeCluster(t, root)

	var buf1 bytes.Buffer
	adviseCmd.SetOut(&buf1)
	if err := runAdviseNudge(adviseCmd); err != nil {
		t.Fatalf("runAdviseNudge (first): %v", err)
	}
	if !bytes.Contains(buf1.Bytes(), []byte("tu-agent: ")) {
		t.Errorf("first nudge run should print a prefixed suggestion, got: %q", buf1.String())
	}
	if !bytes.Contains(buf1.Bytes(), []byte("tu-agent memory crystallize")) {
		t.Errorf("first nudge run missing crystallize suggestion, got: %q", buf1.String())
	}

	// Second run with unchanged evidence must print nothing (evidence-growth
	// dedup) even though the cluster still needs crystallizing.
	var buf2 bytes.Buffer
	adviseCmd.SetOut(&buf2)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdviseNudge(adviseCmd); err != nil {
		t.Fatalf("runAdviseNudge (second): %v", err)
	}
	if buf2.Len() != 0 {
		t.Errorf("second nudge run with unchanged evidence should be silent, got: %q", buf2.String())
	}

	// State was actually persisted to the store, not just held in memory.
	st, err := loadAdviseState(root)
	if err != nil {
		t.Fatalf("loadAdviseState: %v", err)
	}
	if st.Rules["crystallize-ready"].LastShownEvidence != 1 {
		t.Errorf("persisted state = %+v, want LastShownEvidence 1", st.Rules["crystallize-ready"])
	}
}

func TestAdviseDismiss_UnknownRuleErrors(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := runAdviseDismiss(adviseCmd, "not-a-real-rule"); err == nil {
		t.Fatal("runAdviseDismiss with an unknown rule id: want error, got nil")
	}
}

func TestAdviseDismiss_KnownRulePersists(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdviseDismiss(adviseCmd, "secret-guard"); err != nil {
		t.Fatalf("runAdviseDismiss: %v", err)
	}
	st, err := loadAdviseState(root)
	if err != nil {
		t.Fatalf("loadAdviseState: %v", err)
	}
	if !st.Rules["secret-guard"].Dismissed {
		t.Errorf("secret-guard should be dismissed in persisted state, got: %+v", st.Rules)
	}
}

func TestAdviseReset_ClearsState(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := runAdviseDismiss(adviseCmd, "secret-guard"); err != nil {
		t.Fatalf("runAdviseDismiss: %v", err)
	}
	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdviseReset(adviseCmd); err != nil {
		t.Fatalf("runAdviseReset: %v", err)
	}
	st, err := loadAdviseState(root)
	if err != nil {
		t.Fatalf("loadAdviseState: %v", err)
	}
	if len(st.Rules) != 0 {
		t.Errorf("reset state should be empty, got: %+v", st.Rules)
	}
}

func TestAdviseNudge_DegradesQuietlyOnMissingTelemetry(t *testing.T) {
	// No .tu-agent/telemetry.jsonl at all: must tolerate the missing file
	// (stats.ReadEntries already does), not fail the hook.
	root := t.TempDir()
	t.Chdir(root)
	var buf bytes.Buffer
	adviseCmd.SetOut(&buf)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	adviseNudge = true
	t.Cleanup(func() { adviseNudge = false })
	if err := adviseCmd.RunE(adviseCmd, nil); err != nil {
		t.Fatalf("advise --nudge must never fail the hook, got: %v", err)
	}
}
