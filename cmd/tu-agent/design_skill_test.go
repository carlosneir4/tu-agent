package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readDesignSkillMD is the shared file-read helper for every design-skill
// parity test. It mirrors groundwork_skill_test.go's inline ReadFile: two
// levels up from cmd/tu-agent to the repo root, then into plugin/skills.
// Fatalf here is the honest-red signal until plugin/skills/design/SKILL.md
// is written.
func readDesignSkillMD(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "design", "SKILL.md"))
	if err != nil {
		t.Fatalf("read design SKILL.md: %v", err)
	}
	return string(raw)
}

// designStepHeadings is the pinned, ordered list of the eight flow-step
// headings from design.md's outline.
var designStepHeadings = []string{
	"## Step 1 — Anchor",
	"## Step 2 — Interrogate the forces",
	"## Step 3 — Size first",
	"## Step 4 — Pattern budget",
	"## Step 5 — Convene the guild",
	"## Step 6 — Human chooses",
	"## Step 7 — Rocket detector",
	"## Step 8 — Output",
}

// sectionBetween returns the slice of s from the start of startHeading up to
// the start of endHeading (or the end of s when endHeading is ""). It fails
// the test if either pinned heading is missing, since callers scope their
// assertions to a specific step section.
func sectionBetween(t *testing.T, s, startHeading, endHeading string) string {
	t.Helper()
	start := strings.Index(s, startHeading)
	if start == -1 {
		t.Fatalf("heading %q not found", startHeading)
	}
	if endHeading == "" {
		return s[start:]
	}
	end := strings.Index(s, endHeading)
	if end == -1 {
		t.Fatalf("heading %q not found", endHeading)
	}
	if end <= start {
		t.Fatalf("heading %q (idx %d) does not come after %q (idx %d)", endHeading, end, startHeading, start)
	}
	return s[start:end]
}

// @s1 — SKILL.md has valid frontmatter and the eight flow steps in order.
func TestDesignSkillFrontmatterAndStepOrder(t *testing.T) {
	s := readDesignSkillMD(t)

	if !strings.Contains(s, "name: design") {
		t.Error("SKILL.md missing `name: design` frontmatter")
	}
	if !strings.Contains(s, "description:") {
		t.Error("SKILL.md missing `description:` frontmatter key")
	}
	for _, kw := range []string{"from zero", "architecture", "brainstorm"} {
		if !strings.Contains(s, kw) {
			t.Errorf("SKILL.md description missing keyword %q", kw)
		}
	}

	indices := make([]int, len(designStepHeadings))
	for i, h := range designStepHeadings {
		idx := strings.Index(s, h)
		if idx == -1 {
			t.Errorf("SKILL.md missing step heading %q", h)
		}
		indices[i] = idx
	}
	for i := 1; i < len(indices); i++ {
		if indices[i-1] == -1 || indices[i] == -1 {
			continue
		}
		if indices[i] <= indices[i-1] {
			t.Errorf("step heading %q (index %d) must come after %q (index %d): order violated",
				designStepHeadings[i], indices[i], designStepHeadings[i-1], indices[i-1])
		}
	}
}

// @s2 — the proportionality, rationalizations, and per-lens checklist tables
// are present, with all six lenses each backed by their OWN checklist table
// (not merely named in prose) and the Contracts conditional note.
func TestDesignSkillTables(t *testing.T) {
	s := readDesignSkillMD(t)

	if !strings.Contains(s, "| Tier | Guild roster | Scrutiny |") {
		t.Error("SKILL.md missing proportionality-rubric table header")
	}
	if !strings.Contains(s, "| Rationalization | Counter-question |") {
		t.Error("SKILL.md missing rationalizations table header")
	}
	if !strings.Contains(s, "| Concern | What to check |") {
		t.Error("SKILL.md missing per-lens checklist table header")
	}
	if n := strings.Count(s, "| Concern | What to check |"); n != 6 {
		t.Errorf("SKILL.md must have exactly 6 per-lens checklist tables (one per catalog lens), found %d", n)
	}

	catalog := sectionBetween(t, s, "### Lens catalog", "## Step 6 — Human chooses")

	// Each lens must be backed by its own checklist table: the lens's bold
	// caption followed (later in the catalog) by a distinctive first-column
	// concern row drawn from that lens's own table. Anchoring on the row, not
	// just the lens name, means dropping or thinning a whole table fails this
	// test even if the lens name survives elsewhere in prose (e.g. the Step 5
	// roster table).
	lensTables := []struct {
		lens    string
		concern string
	}{
		{"Security", "Trust boundaries"},
		{"Operations", "Config & environments"},
		{"Reliability & performance", "Failure modes & graceful degradation"},
		{"Data & privacy", "Persistence choice"},
		{"Contracts", "Contract clarity"},
		{"Quality", "Testability of boundaries"},
	}
	for _, lt := range lensTables {
		li := strings.Index(catalog, lt.lens)
		if li == -1 {
			t.Errorf("Lens catalog missing lens %q", lt.lens)
			continue
		}
		ci := strings.Index(catalog, lt.concern)
		if ci == -1 {
			t.Errorf("Lens catalog missing %q's checklist row %q — its table may have been dropped or thinned", lt.lens, lt.concern)
			continue
		}
		if ci <= li {
			t.Errorf("%q's checklist row %q (index %d) must come after the lens's own caption (index %d)", lt.lens, lt.concern, ci, li)
		}
	}

	if !strings.Contains(s, "only when the design exposes an API") {
		t.Error(`SKILL.md missing Contracts conditional note "only when the design exposes an API"`)
	}
}

// @s3 — the tier-to-roster rule and editable roster are pinned in Convene
// the guild.
func TestDesignSkillConveneGuildSection(t *testing.T) {
	s := readDesignSkillMD(t)
	section := sectionBetween(t, s, "## Step 5 — Convene the guild", "## Step 6 — Human chooses")

	checkMapsTo := func(from, to string) {
		t.Helper()
		fi := strings.Index(section, from)
		if fi == -1 {
			t.Errorf("Convene-the-guild section missing %q", from)
			return
		}
		ti := strings.Index(section, to)
		if ti == -1 {
			t.Errorf("Convene-the-guild section missing %q", to)
			return
		}
		if ti <= fi {
			t.Errorf("%q (index %d) must map to %q at a later index (got %d)", from, fi, to, ti)
		}
	}

	checkMapsTo("throwaway", "no guild")
	checkMapsTo("internal tool", "Security + Quality")
	checkMapsTo("production service", "full roster")

	if !strings.Contains(section, "before dispatch") ||
		!(strings.Contains(section, "add/remove") || strings.Contains(section, "add or remove")) {
		t.Error("Convene-the-guild section missing the editable-roster statement (add/remove lenses before dispatch)")
	}
	if !strings.Contains(section, "general-purpose") {
		t.Error("Convene-the-guild section missing the general-purpose dispatch mechanism")
	}

	if !(strings.Contains(section, "2 to 4") || strings.Contains(section, "2-4")) {
		t.Error("Convene-the-guild section missing the 2-4 ideas contract count")
	}
	if !strings.Contains(section, "force that justifies") {
		t.Error(`Convene-the-guild section missing the ideas contract's "force that justifies" element`)
	}
	if !strings.Contains(section, "cost of skipping") {
		t.Error(`Convene-the-guild section missing the ideas contract's "cost of skipping" element`)
	}
}

// @s4 — human arbitrates: the choice contract, the rocket-detector question,
// and the informs-vs-decides split.
func TestDesignSkillHumanChoiceAndRocketDetector(t *testing.T) {
	s := readDesignSkillMD(t)

	humanChooses := sectionBetween(t, s, "## Step 6 — Human chooses", "## Step 7 — Rocket detector")
	for _, want := range []string{"accept", "defer", "discard", "Non-goals", "reason", "recorded"} {
		if !strings.Contains(humanChooses, want) {
			t.Errorf("Human-chooses section missing %q", want)
		}
	}

	rocketDetector := sectionBetween(t, s, "## Step 7 — Rocket detector", "## Step 8 — Output")
	// Pinned as full standalone lines (not mid-paragraph substrings): a
	// paragraph reflow around either sentence cannot silently split the
	// pinned phrase across a line break and pass anyway.
	if !strings.Contains(rocketDetector, `> "delete it; what breaks today?"`) {
		t.Error(`Rocket-detector section missing the pinned question as its own standalone line: > "delete it; what breaks today?"`)
	}
	if !strings.Contains(rocketDetector, "> the guild adds; the detector prunes; the human arbitrates both.") {
		t.Error("Rocket-detector section missing the informs-vs-decides split statement as its own standalone line")
	}
}

// @s5 — the Output step pins the mermaid sketch, the ADR record, and the tdd
// hand-off.
func TestDesignSkillOutputStep(t *testing.T) {
	s := readDesignSkillMD(t)
	output := sectionBetween(t, s, "## Step 8 — Output", "")

	if !strings.Contains(output, "architecture sketch") {
		t.Error("Output section missing the architecture-sketch requirement")
	}
	if n := strings.Count(output, "```mermaid"); n != 1 {
		t.Errorf("Output section must require exactly one mermaid diagram, found %d ```mermaid fences", n)
	}
	if !strings.Contains(output, "mem_save") {
		t.Error("Output section missing the mem_save call")
	}
	if !strings.Contains(output, "decision/<area>-architecture") {
		t.Error("Output section missing the mem_save topic `decision/<area>-architecture`")
	}
	for _, want := range []string{"chosen", "rejected", "deferred", "forces"} {
		if !strings.Contains(output, want) {
			t.Errorf("Output section's ADR record missing %q", want)
		}
	}
	if !strings.Contains(output, "/tu-agent:tdd") {
		t.Error("Output section missing the /tu-agent:tdd hand-off")
	}
	if !strings.Contains(output, "Step 1") {
		t.Error("Output section missing the tdd Step 1 design-doc seeding path reference")
	}
}

// @s6 — the Go parity test lives beside groundwork's and pins acceptance 1
// through 6 by substring. This is a meta-check: it confirms the parity test
// itself (this file) reads the SKILL.md via the pinned relative path, and
// that it actually carries the assertions the other five tests claim to
// make. It still gates on the SKILL.md's existence, via the shared helper,
// so it is honest red today like every other test in this file.
func TestDesignSkillParityTestStructure(t *testing.T) {
	_ = readDesignSkillMD(t)

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's own path")
	}
	src, err := os.ReadFile(thisFile)
	if err != nil {
		t.Fatalf("read own source %s: %v", thisFile, err)
	}
	own := string(src)

	if !strings.Contains(own, `filepath.Join("..", "..", "plugin", "skills", "design", "SKILL.md")`) {
		t.Error("design_skill_test.go does not read SKILL.md via the pinned filepath.Join(\"..\", \"..\", \"plugin\", \"skills\", \"design\", \"SKILL.md\") form")
	}

	for _, marker := range []string{
		`"name: design"`,                       // frontmatter
		`"## Step 1 — Anchor"`,                 // step headings, in order
		`"| Tier | Guild roster | Scrutiny |"`, // table groups
		`"no guild"`,                           // tier-to-roster rule
		`"accept"`,                             // human-choice contract
		`"delete it; what breaks today?"`,      // rocket-detector question
		"decision/<area>-architecture",         // Output pins
	} {
		if !strings.Contains(own, marker) {
			t.Errorf("design_skill_test.go is missing an assertion covering %s", marker)
		}
	}
}
