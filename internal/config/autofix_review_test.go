package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// autofix_review_test.go — RED-phase tests for feature auto-fix-review-flag,
// scenarios @s1 and @s2 (spec.md section B, design.md D1). TddConfig does not
// carry an AutoFixReview field yet, so these read it through reflection rather
// than naming it directly — a direct field reference would fail to COMPILE
// today, but the RED gate requires today's failure to be a runtime/assertion
// failure. reflectAutoFixReview fails the test with a clear message when the
// field is not yet present (today's honest RED); once config.TddConfig grows
// the field, it resolves the merged bool exactly as any other field access
// would.
func reflectAutoFixReview(t *testing.T, tdd *config.TddConfig) bool {
	t.Helper()
	f := reflect.ValueOf(tdd).Elem().FieldByName("AutoFixReview")
	if !f.IsValid() {
		t.Fatalf("config.TddConfig has no AutoFixReview field yet (spec.md section B, design.md D1) — RED until the GREEN change adds `AutoFixReview bool` with yaml tag `auto_fix_review`")
	}
	return f.Bool()
}

// @s1 — auto_fix_review defaults to false when no tu-agent config yaml exists
// at all: a repo with neither user nor project config layers must still
// resolve TddConfig.AutoFixReview to false (the zero value).
func TestAutoFixReview_DefaultsFalse(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reflectAutoFixReview(t, &cfg.Tdd); got {
		t.Fatalf("TddConfig.AutoFixReview = true, want false (default) when no config yaml exists at all")
	}
}

// @s2 — true in the user layer survives a silent project layer: the user
// config sets auto_fix_review: true, the project config is present but never
// mentions the key, and the merged config must still resolve true (true-wins
// merge semantics, per gotcha/config-mergeinto-new-field and mirroring
// Tdd.Strict/Tdd.Mutation in loader.go's mergeInto).
func TestAutoFixReview_TrueInUserLayerSurvivesSilentProjectLayer(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte("tdd:\n  auto_fix_review: true\n"), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	// Project layer is present but silent on auto_fix_review — it must not
	// reset the user layer's true back to false.
	if err := os.WriteFile(filepath.Join(projDir, "config.yaml"), []byte("tdd:\n  strict: false\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reflectAutoFixReview(t, &cfg.Tdd); !got {
		t.Fatalf("TddConfig.AutoFixReview = false, want true (user layer's true must survive a silent project layer)")
	}
}
