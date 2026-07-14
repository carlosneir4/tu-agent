package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// @s4 — review and review-fixer are resolvable stages with the right roles.

// StageOverlay resolves "review" to ReviewPrompt and "review-fixer" to
// ReviewFixerPrompt. Asserted via overlay-unique content markers so the test
// does not couple to the concrete constant identifiers.
func TestTddOverlayReviewStages(t *testing.T) {
	review, ok := StageOverlay("review")
	if !ok {
		t.Fatal(`tddOverlay("review") must resolve`)
	}
	if !strings.Contains(review, "merge-base") || !strings.Contains(review, "progress/review.md") {
		t.Fatalf(`tddOverlay("review") must return ReviewPrompt (merge-base + progress/review.md), got %q`, review)
	}

	fixer, ok := StageOverlay("review-fixer")
	if !ok {
		t.Fatal(`tddOverlay("review-fixer") must resolve`)
	}
	if !strings.Contains(fixer, "test file") || !strings.Contains(fixer, `"kind":"source"`) {
		t.Fatalf(`tddOverlay("review-fixer") must return ReviewFixerPrompt, got %q`, fixer)
	}
}

// ComposeStagePrompt composes "review" from the pr-reviewer agent body and
// "review-fixer" from the developer agent body, and substitutes the base dir for
// the __TDDDIR__ token (the `tdd prompt review --base <dir>` path).
func TestComposeStagePromptReview(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pr-reviewer.md"), []byte("---\nname: x\n---\nREVIEWER-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "developer.md"), []byte("---\nname: x\n---\nDEV-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := ".tu-agent/tdd/ABC-1-review"

	// review composes the pr-reviewer body with the review overlay.
	rev, err := ComposeStagePrompt(root, "review", base)
	if err != nil {
		t.Fatalf("composeStagePrompt(review): %v", err)
	}
	if !strings.Contains(rev, "REVIEWER-BODY") {
		t.Errorf("review prompt must include the pr-reviewer agent body")
	}
	if !strings.Contains(rev, "progress/review.md") {
		t.Errorf("review prompt must include the review overlay")
	}
	if strings.Contains(rev, TddDirToken) {
		t.Errorf("review prompt must substitute the __TDDDIR__ token")
	}
	if !strings.Contains(rev, base+"/progress/review.md") {
		t.Errorf("`tdd prompt review --base %s` must substitute the base dir into the report path, got %q", base, rev)
	}

	// review-fixer composes the developer body with the fixer overlay.
	fix, err := ComposeStagePrompt(root, "review-fixer", base)
	if err != nil {
		t.Fatalf("composeStagePrompt(review-fixer): %v", err)
	}
	if !strings.Contains(fix, "DEV-BODY") {
		t.Errorf("review-fixer prompt must include the developer agent body")
	}
	if !strings.Contains(fix, "test file") {
		t.Errorf("review-fixer prompt must include the fixer overlay")
	}
	if strings.Contains(fix, TddDirToken) {
		t.Errorf("review-fixer prompt must substitute the __TDDDIR__ token")
	}
}
