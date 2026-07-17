package tdd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/subagent"
)

func TestTddCheckMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := ValidateTddAgents(root)
	if len(missing) != 5 {
		t.Fatalf("empty repo should miss all 5 roles, got %v", missing)
	}
}

func TestTddCheckPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"analyst", "architect", "developer", "pr-reviewer", "scribe"} {
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte("---\nname: x\n---\nb\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if m := ValidateTddAgents(root); len(m) != 0 {
		t.Fatalf("all roles present but reported missing: %v", m)
	}
}

func TestTddOverlaySandwichStages(t *testing.T) {
	tw, ok := StageOverlay("test-writer")
	if !ok || !strings.Contains(tw, "NO production") {
		t.Fatalf("test-writer overlay must contain %q, got ok=%v %q", "NO production", ok, tw)
	}
	impl, ok := StageOverlay("implementer")
	if !ok || !strings.Contains(impl, "do NOT modify") {
		t.Fatalf("implementer overlay must contain %q, got ok=%v %q", "do NOT modify", ok, impl)
	}
}

func TestComposeStagePromptSandwich(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both sandwich stages map to the developer.md role, same as craftsman.
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tw, err := ComposeStagePrompt(root, "test-writer", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt(test-writer): %v", err)
	}
	if !strings.Contains(tw, "DEV-BODY") || !strings.Contains(tw, "NO production") {
		t.Fatalf("test-writer prompt must join body + RED overlay, got: %q", tw)
	}
	impl, err := ComposeStagePrompt(root, "implementer", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt(implementer): %v", err)
	}
	if !strings.Contains(impl, "DEV-BODY") || !strings.Contains(impl, "do NOT modify") {
		t.Fatalf("implementer prompt must join body + GREEN overlay, got: %q", impl)
	}
}

func TestTddStageDefsSubstitutesBaseDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// TddStageDefs loads every non-analyst role; give them all a body.
	for _, role := range []string{"architect", "developer", "pr-reviewer", "scribe"} {
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defs, err := TddStageDefs(root, ".tu-agent/tdd/ABC-1-x")
	if err != nil {
		t.Fatalf("tddStageDefs: %v", err)
	}
	var arch *subagent.Definition
	for _, d := range defs {
		if strings.Contains(d.SystemPrompt, TddDirToken) {
			t.Errorf("stage %s still contains unsubstituted token", d.Name)
		}
		if d.Name == "architect" {
			arch = d
		}
	}
	if arch == nil || !strings.Contains(arch.SystemPrompt, ".tu-agent/tdd/ABC-1-x/spec.md") {
		t.Errorf("architect def missing substituted base dir")
	}
}

func TestComposeStagePrompt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// craftsman maps to developer.md
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ComposeStagePrompt(root, "craftsman", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(out, "DEV-BODY") || !strings.Contains(out, "tu-agent TDD task") {
		t.Fatalf("compose must join body + overlay, got: %q", out)
	}
	if _, err := ComposeStagePrompt(root, "bogus", TddRelBase("", "x")); err == nil {
		t.Fatal("unknown stage must error")
	}
	// A missing agent file for a known role now falls back to the embedded
	// generic shell (F7-B) instead of erroring — the flow runs without a
	// materialized .claude/agents/<role>.md.
	fb, err := ComposeStagePrompt(root, "architect", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("missing agent file must fall back to the embedded shell, got error: %v", err)
	}
	if !strings.Contains(fb, "tu-agent TDD task") {
		t.Fatalf("architect fallback must still carry the TDD overlay, got: %q", fb)
	}
}

// @s7 — When rules exist the composed order is body then rules then overlay.
func TestComposeStagePromptIncludesRulesInOrder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".tu-agent", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".tu-agent", "rules", "all.md"), []byte("REPO-WIDE-RULE"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := ComposeStagePrompt(root, "craftsman", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}
	for _, want := range []string{"DEV-BODY", "REPO-WIDE-RULE", "tu-agent TDD task"} {
		if !strings.Contains(out, want) {
			t.Fatalf("composed prompt must contain %q, got: %q", want, out)
		}
	}
	bodyIdx := strings.Index(out, "DEV-BODY")
	ruleIdx := strings.Index(out, "REPO-WIDE-RULE")
	overlayIdx := strings.Index(out, "tu-agent TDD task")
	if bodyIdx >= ruleIdx {
		t.Errorf("DEV-BODY must appear before REPO-WIDE-RULE, got body@%d rule@%d", bodyIdx, ruleIdx)
	}
	if ruleIdx >= overlayIdx {
		t.Errorf("REPO-WIDE-RULE must appear before overlay marker, got rule@%d overlay@%d", ruleIdx, overlayIdx)
	}
}

// @s8 — When no rules exist the composed prompt is unchanged (body then overlay).
func TestComposeStagePromptUnchangedWithoutRules(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := ComposeStagePrompt(root, "craftsman", TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}
	if !strings.Contains(out, "DEV-BODY") {
		t.Fatalf("composed prompt must contain %q, got: %q", "DEV-BODY", out)
	}
	if !strings.Contains(out, "tu-agent TDD task") {
		t.Fatalf("composed prompt must contain overlay marker %q, got: %q", "tu-agent TDD task", out)
	}
	if strings.Contains(out, "Project rules") {
		t.Errorf("composed prompt must NOT contain %q when no rules files exist, got: %q", "Project rules", out)
	}
	bodyIdx := strings.Index(out, "DEV-BODY")
	overlayIdx := strings.Index(out, "tu-agent TDD task")
	if bodyIdx >= overlayIdx {
		t.Errorf("DEV-BODY must appear before overlay marker, got body@%d overlay@%d", bodyIdx, overlayIdx)
	}
}

func TestTddOverlayRefactor(t *testing.T) {
	o, ok := StageOverlay("refactor")
	if !ok || !strings.Contains(o, "REFACTOR") {
		t.Fatalf("refactor overlay missing: ok=%v", ok)
	}
}

func TestComposeStagePromptRefactor(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ComposeStagePrompt(root, "refactor", ".tu-agent/tdd/x")
	if err != nil {
		t.Fatalf("composeStagePrompt(refactor): %v", err)
	}
	if !strings.Contains(out, "BODY") || !strings.Contains(out, "REFACTOR") {
		t.Errorf("composed refactor prompt incomplete")
	}
}

func TestComposeStagePromptSubstitutesBaseDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "architect.md"), []byte("---\nname: x\n---\nBODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ComposeStagePrompt(root, "architect", ".tu-agent/tdd/ABC-1-x")
	if err != nil {
		t.Fatalf("composeStagePrompt: %v", err)
	}
	if strings.Contains(out, TddDirToken) {
		t.Errorf("token not substituted")
	}
	if !strings.Contains(out, ".tu-agent/tdd/ABC-1-x/spec.md") {
		t.Errorf("base dir not applied")
	}
}

func TestStripFrontmatter(t *testing.T) {
	in := "---\nname: x\ntools: Read\n---\nBODY line 1\nBODY line 2\n"
	if got := stripFrontmatter(in); got != "BODY line 1\nBODY line 2\n" {
		t.Fatalf("stripFrontmatter = %q", got)
	}
	if got := stripFrontmatter("no frontmatter\n"); got != "no frontmatter\n" {
		t.Fatalf("passthrough = %q", got)
	}
}

func TestValidateTddAgents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only architect present: the other four are missing.
	if err := os.WriteFile(filepath.Join(dir, "architect.md"), []byte("---\nname: a\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := ValidateTddAgents(root)
	for _, want := range []string{"analyst", "developer", "pr-reviewer", "scribe"} {
		if !slices.Contains(missing, want) {
			t.Errorf("expected %q in missing, got %v", want, missing)
		}
	}
	if slices.Contains(missing, "architect") {
		t.Errorf("architect present but reported missing: %v", missing)
	}
}

func TestTddStageDefsComposesBodyAndOverlay(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"architect", "developer", "pr-reviewer", "scribe"} {
		body := "---\nname: " + role + "\n---\nKNOWLEDGE-" + role + "\n"
		if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defs, err := TddStageDefs(root, TddRelBase("", "x"))
	if err != nil {
		t.Fatalf("tddStageDefs: %v", err)
	}
	byName := map[string]*subagent.Definition{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	if byName["analyst"] != nil {
		t.Error("analyst runs in the foreground, not via the dispatcher")
	}
	arch := byName["architect"]
	if arch == nil || !strings.Contains(arch.SystemPrompt, "KNOWLEDGE-architect") ||
		!strings.Contains(arch.SystemPrompt, "tu-agent TDD task") {
		t.Fatalf("architect def must compose body + overlay: %+v", arch)
	}
	craft := byName["craftsman"]
	if craft == nil || !strings.Contains(craft.SystemPrompt, "KNOWLEDGE-developer") {
		t.Fatalf("craftsman def must load the developer body: %+v", craft)
	}
	if !slices.Contains(craft.ToolSubset, "write_file") {
		t.Errorf("craftsman must grant write_file")
	}
}
