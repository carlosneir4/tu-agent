package codegen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestGenerateSkill_PromptsWithNotesAndReturnsBody(t *testing.T) {
	rp := &recordProvider{reply: "---\nname: checkout\ndescription: use when testing checkout\n---\n# checkout\nstandard body"}
	notes := []codegen.NoteInput{
		{Topic: "testing/checkout-flow", Type: "testing", Content: "cover the checkout total branches"},
		{Topic: "gotcha/checkout-null-cart", Type: "gotcha", Content: "checkout panics on empty cart"},
	}
	out, err := codegen.GenerateSkill(context.Background(), "checkout", notes, rp, newDomaingenTelemetry(t), 0)
	if err != nil {
		t.Fatalf("GenerateSkill: %v", err)
	}
	if !strings.Contains(out, "name: checkout") {
		t.Errorf("body should be the model reply, got:\n%s", out)
	}
	// The prompt must carry the member notes' content so the model can synthesize.
	joined := strings.Join(rp.prompts, "\n")
	for _, want := range []string{"checkout total branches", "panics on empty cart", "checkout"} {
		if !strings.Contains(joined, want) {
			t.Errorf("prompt missing %q; got:\n%s", want, joined)
		}
	}
}
