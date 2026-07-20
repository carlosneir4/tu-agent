package config_test

import (
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// autofix_review_strip_test.go — RED-phase tests for feature
// project-layer-autofix-strip (approved review-finding fix, mirroring the
// BaseURL/test_command untrusted-project-layer strips in loader.go's
// mergeFromFile): the untrusted project layer (./.tu-agent/config.yaml) must
// not be able to ENABLE tdd.auto_fix_review on its own — only the user layer
// (~/.tu-agent/config.yaml) may turn the auto-fixer on. TddConfig.AutoFixReview
// exists already (added by the earlier auto-fix-review-flag GREEN change), so
// these reference it directly — no reflection needed.
//
// writeFile is defined in loader_test.go (same config_test package).

// @s1 — a project-layer-only "auto_fix_review: true" is ignored: mergeInto's
// current unconditional true-wins merge does not yet distinguish the
// untrusted project layer from the trusted user layer, so this fails today.
func TestProjectLayerOnlyAutoFixReviewIsIgnored(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	writeFile(t, projDir, "config.yaml", "tdd:\n  auto_fix_review: true\n")

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tdd.AutoFixReview {
		t.Fatalf("Tdd.AutoFixReview = true from a project-layer-only yaml, want false — the untrusted project layer must not be able to enable the auto-fixer (mirrors the tdd.test_command / provider base_url strips)")
	}
}

// @s2 — a user-layer "auto_fix_review: true" is honored when the project
// layer says nothing: the trusted layer can still turn the flag on.
func TestUserLayerAutoFixReviewIsHonored(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	writeFile(t, userDir, "config.yaml", "tdd:\n  auto_fix_review: true\n")

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Tdd.AutoFixReview {
		t.Fatalf("Tdd.AutoFixReview = false, want true — the trusted user layer must still be able to enable the auto-fixer")
	}
}

// @s3 — true in both layers stays true: stripping the project layer's
// contribution must not defeat a project layer that merely echoes an
// already-trusted user-layer true.
func TestBothLayersAutoFixReviewTrueStaysTrue(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	writeFile(t, userDir, "config.yaml", "tdd:\n  auto_fix_review: true\n")
	writeFile(t, projDir, "config.yaml", "tdd:\n  auto_fix_review: true\n")

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Tdd.AutoFixReview {
		t.Fatalf("Tdd.AutoFixReview = false, want true — a project layer echoing an already-true user layer must not flip it off")
	}
}
