package main

import (
	"bytes"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestMemoryCrystallizeCLI_ListsClusters(t *testing.T) {
	t.Chdir(t.TempDir())

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ topic, typ, content string }{
		{"testing/checkout-flow", "testing", "checkout order total"},
		{"gotcha/checkout-null-cart", "gotcha", "checkout cart empty panic"},
		{"decision/checkout-tax", "decision", "checkout tax per region"},
	} {
		if _, err := ms.Upsert(tc.topic, tc.content, memory.UpsertOpts{Type: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	memCrystallizeMin = 3
	t.Cleanup(func() { memCrystallizeMin = 5; memoryCrystallizeCmd.SetOut(nil) })
	var buf bytes.Buffer
	memoryCrystallizeCmd.SetOut(&buf)
	if err := memoryCrystallizeCmd.RunE(memoryCrystallizeCmd, nil); err != nil {
		t.Fatalf("memory crystallize: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"checkout", "testing/checkout-flow", "1 crystallizable"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
