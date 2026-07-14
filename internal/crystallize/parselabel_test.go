package crystallize

import (
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// @s1 ParseLabel extracts the label from the provenance marker line embedded in
// a realistic SKILL.md body (frontmatter + prose). Hyphenated label pins that a
// kebab-case value is captured whole.
func TestParseLabel_ExtractsFromMarker(t *testing.T) {
	members := []memory.Observation{ob("testing/x", 1), ob("gotcha/y", 2)}
	body := "---\nname: acme-checkout\ndescription: Standard for the checkout flow.\n---\n\n" +
		ProvenanceLine("acme-checkout", members) + "\n\n" +
		"# Checkout\n\nStart here for the acme checkout area.\n"
	if got := ParseLabel(body); got != "acme-checkout" {
		t.Errorf("ParseLabel = %q, want %q", got, "acme-checkout")
	}
}

// @s2 ParseLabel returns "" when there is no label to read: (a) no provenance
// marker at all, and (b) a marker line without a label= field.
func TestParseLabel_EmptyWhenNoLabel(t *testing.T) {
	noMarker := "---\nname: something\n---\n\n# Body\n\nNo provenance marker here.\n"
	if got := ParseLabel(noMarker); got != "" {
		t.Errorf("ParseLabel(no marker) = %q, want empty", got)
	}

	markerNoLabel := "---\nname: something\n---\n\n" +
		"<!-- tu-agent:crystallize source-hash=deadbeef -->\n\n# Body\n"
	if got := ParseLabel(markerNoLabel); got != "" {
		t.Errorf("ParseLabel(marker without label=) = %q, want empty", got)
	}
}

// @s3 ParseLabel reads the provenance label, not the frontmatter name, even when
// the two diverge.
func TestParseLabel_UnaffectedByDivergedFrontmatterName(t *testing.T) {
	members := []memory.Observation{ob("testing/x", 1)}
	body := "---\nname: something-else\ndescription: diverged on purpose.\n---\n\n" +
		ProvenanceLine("acme-checkout", members) + "\n\n" +
		"# Skill\n\nProse mentioning something-else and acme-checkout.\n"
	if got := ParseLabel(body); got != "acme-checkout" {
		t.Errorf("ParseLabel = %q, want %q (provenance label, not frontmatter name)", got, "acme-checkout")
	}
}
