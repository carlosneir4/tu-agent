package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// developerTemplatePaths lists the six developer.md surfaces that must all
// carry the "Surgical & simple" judge-hardening rule (P2/P3 Karpathy gaps):
// the plugin skeleton and the five codegen language templates. Paths are
// relative to this package's directory (internal/codegen).
func developerTemplatePaths() map[string]string {
	return map[string]string{
		"plugin":     filepath.Join("..", "..", "plugin", "agent-templates", "developer.md"),
		"base":       filepath.Join("templates", "base", "developer.md"),
		"go":         filepath.Join("templates", "go", "developer.md"),
		"java":       filepath.Join("templates", "java", "developer.md"),
		"python":     filepath.Join("templates", "python", "developer.md"),
		"typescript": filepath.Join("templates", "typescript", "developer.md"),
	}
}

func readDeveloperTemplate(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// @s4 — every developer.md carries a "Surgical & simple" rule heading/marker
// plus prose for both principles: match the repo's existing style, and do
// the minimum that solves the task without touching adjacent code. Asserted
// per file (t.Run) so a single drifted template doesn't hide behind the others.
func TestDeveloperTemplatesCarrySurgicalRule(t *testing.T) {
	for name, path := range developerTemplatePaths() {
		t.Run(name, func(t *testing.T) {
			content := readDeveloperTemplate(t, path)
			if !strings.Contains(content, "Surgical & simple") {
				t.Errorf("%s: missing the \"Surgical & simple\" rule heading/marker", path)
			}
			if !strings.Contains(content, "existing style") {
				t.Errorf("%s: missing the repo-style-wins cue (\"existing style\")", path)
			}
			if !strings.Contains(content, "minimum that solves") {
				t.Errorf("%s: missing the minimum-change cue (\"minimum that solves\")", path)
			}
			if !strings.Contains(content, "adjacent") {
				t.Errorf("%s: missing the adjacent-code-untouched cue (\"adjacent\")", path)
			}
		})
	}
}

// @s5 — the new Surgical rule coexists with the existing doc-comments
// section: "Doc-comments — keep them minimal" is still present, and appears
// exactly once (no drive-by duplication of its content under the new
// heading), so the two rules stay distinct.
func TestDeveloperTemplatesSurgicalCoexistsWithDocComments(t *testing.T) {
	const docCommentsHeading = "Doc-comments — keep them minimal"
	for name, path := range developerTemplatePaths() {
		t.Run(name, func(t *testing.T) {
			content := readDeveloperTemplate(t, path)
			if !strings.Contains(content, docCommentsHeading) {
				t.Fatalf("%s: missing the existing %q section", path, docCommentsHeading)
			}
			if got := strings.Count(content, docCommentsHeading); got != 1 {
				t.Errorf("%s: %q appears %d times, want exactly 1 (no restatement under the new Surgical heading)", path, docCommentsHeading, got)
			}
		})
	}
}

// TestDeveloperTemplatesCarryTimelessRule verifies every developer.md surface
// tells the agent to write comments that state a durable constraint, never
// tied to ticket/spec/decision provenance that git and project memory
// already hold. Asserted per file (t.Run) so a single drifted template
// doesn't hide behind the others.
func TestDeveloperTemplatesCarryTimelessRule(t *testing.T) {
	for name, path := range developerTemplatePaths() {
		t.Run(name, func(t *testing.T) {
			content := readDeveloperTemplate(t, path)
			if !strings.Contains(content, "Comments are timeless") {
				t.Errorf("%s: missing the \"Comments are timeless\" rule heading/marker", path)
			}
			if !strings.Contains(content, "provenance") {
				t.Errorf("%s: missing the provenance cue (\"provenance\")", path)
			}
		})
	}
}
