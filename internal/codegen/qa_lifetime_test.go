package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qaTemplatePaths lists the qa.md surfaces that must all carry the "Lifetime &
// placement" test-discipline rule: the plugin skeleton and the codegen base
// template. The per-language codegen body templates were removed (base is now
// the single body source); language specialization lives in the runtime
// overlay, which carries no such prose. Paths are relative to this package's
// directory (internal/codegen).
func qaTemplatePaths() map[string]string {
	return map[string]string{
		"plugin": filepath.Join("..", "..", "plugin", "agents", "qa.md"),
		"base":   filepath.Join("templates", "base", "qa.md"),
	}
}

func readQATemplate(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestQATemplatesCarryLifetimePlacementRule verifies every qa.md surface
// carries the rule that a new test joins the existing test that owns its
// subject, never re-tests what the compiler/linter guarantees, marks
// legacy-comparison tests as strangler scaffolding, and never freezes a
// signature that isn't a published contract. Asserted per file (t.Run) so a
// single drifted template doesn't hide behind the others.
func TestQATemplatesCarryLifetimePlacementRule(t *testing.T) {
	for name, path := range qaTemplatePaths() {
		t.Run(name, func(t *testing.T) {
			content := readQATemplate(t, path)
			if !strings.Contains(content, "Lifetime & placement") {
				t.Errorf("%s: missing the \"Lifetime & placement\" rule heading/marker", path)
			}
			if !strings.Contains(content, "compiler or linter") {
				t.Errorf("%s: missing the compiler-or-linter cue (\"compiler or linter\")", path)
			}
			if !strings.Contains(content, "published contract") {
				t.Errorf("%s: missing the published-contract cue (\"published contract\")", path)
			}
			if !strings.Contains(content, "strangler") {
				t.Errorf("%s: missing the strangler-scaffolding cue (\"strangler\")", path)
			}
		})
	}
}
