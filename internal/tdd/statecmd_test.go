package tdd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests were moved verbatim (adapted to inject root/baseFlag/ticket/out
// as parameters instead of package-main globals + repoRoot()) from
// cmd/tu-agent/tdd_state_test.go, tdd_state_begin_guard_test.go, and
// tdd_review_state_test.go when the state/status CLI logic moved into
// internal/tdd (F8 item 5.3). Every assertion is preserved.

func TestTddStatusJSON(t *testing.T) {
	var buf bytes.Buffer
	// In a temp repo with no state, status reports not resumable.
	if err := RunStatus(t.TempDir(), "", "", &buf); err != nil {
		t.Fatalf("status: %v", err)
	}
	var out struct {
		Resumable bool `json:"resumable"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("status output not JSON: %q (%v)", buf.String(), err)
	}
}

// TestTddStateBeginRejectsDuplicateFeatures drives `tdd state begin` with
// duplicate feature values and asserts the surface surfaces BeginRun's
// duplicate-feature error, rather than silently building a state.json that
// would later infinite-loop NextPending/Mark on resume.
func TestTddStateBeginRejectsDuplicateFeatures(t *testing.T) {
	var buf bytes.Buffer
	err := RunStateBegin(t.TempDir(), "", "", "t", "b", []string{"x", "x"}, &buf)
	if err == nil {
		t.Fatalf("expected duplicate-feature error, got nil")
	}
	if !strings.Contains(err.Error(), `duplicate feature "x"`) {
		t.Fatalf("error = %q, want it to contain `duplicate feature \"x\"`", err.Error())
	}
}

func TestTddStateMarkUnknown(t *testing.T) {
	var buf bytes.Buffer
	err := RunStateMark(t.TempDir(), "", "", "nope", "pass", &buf)
	if err == nil {
		t.Fatalf("expected error marking unknown feature on empty state, got nil")
	}
	if !strings.Contains(err.Error(), "nope") && !strings.Contains(err.Error(), "state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTddStatePathByTicket(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".tu-agent", "tdd", "ABC-1-x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := TddStatePath(root, "ABC-1")
	if got != filepath.Join(dir, "state.json") {
		t.Errorf("TddStatePath = %q, want under %q", got, dir)
	}
}

func TestTddStateBaseRel(t *testing.T) {
	root := "/repo"
	sp := filepath.Join(root, ".tu-agent", "tdd", "ABC-1-x", "state.json")
	if got := TddStateBaseRel(root, sp); got != filepath.Join(".tu-agent", "tdd", "ABC-1-x") {
		t.Errorf("TddStateBaseRel = %q", got)
	}
}

func TestTddStateBaseFlagWins(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, ".tu-agent", "tdd", "AAA-1-old")
	newer := filepath.Join(root, ".tu-agent", "tdd", "BBB-2-new")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "state.json"), []byte(`{"version":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// make "older" the mtime-newest so the fallback would pick it
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(older, future, future); err != nil {
		t.Fatal(err)
	}
	if got := TddStateFile(root, ".tu-agent/tdd/BBB-2-new", ""); got != filepath.Join(newer, "state.json") {
		t.Errorf("--base ignored: got %q", got)
	}
	if got := TddStateFile(root, "", ""); got != filepath.Join(older, "state.json") {
		t.Errorf("mtime fallback broken: got %q", got)
	}
}

func TestTddStatePathLegacyFlat(t *testing.T) {
	root := t.TempDir()
	tddDir := filepath.Join(root, ".tu-agent", "tdd")
	if err := os.MkdirAll(tddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tddDir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"features", "progress"} {
		if err := os.MkdirAll(filepath.Join(tddDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := TddStatePath(root, "")
	if got != filepath.Join(tddDir, "state.json") {
		t.Errorf("TddStatePath legacy = %q", got)
	}
}

// ---------------------------------------------------------------------------
// begin-guard scenarios (@f0-3 @s1-@s4) — moved from
// cmd/tu-agent/tdd_state_begin_guard_test.go. Contract pinned: `tdd state begin`
// invoked WITHOUT explicit --base/--ticket resolves its target dir via
// TddStateFile -> ResolveTddBase (newest-by-mtime). When that resolved dir
// already has a state.json whose State.Resumable() is true, begin must abort
// naming the resolved dir and the in-progress task, leaving state.json
// byte-for-byte unchanged. Explicit --base always proceeds/overwrites, and so
// does an implicit begin over a fully-done (non-resumable) state.
// ---------------------------------------------------------------------------

// seedTddRunDir materializes .tu-agent/tdd/<name>/state.json under root and
// stamps the dir with an explicit mtime so newest-by-mtime resolution in
// ResolveTddBase is deterministic regardless of filesystem mtime granularity
// (APFS/CI). mtimes are set explicitly via os.Chtimes (never via sleep).
func seedTddRunDir(t *testing.T, root, name string, st State, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, ".tu-agent", "tdd", name)
	if err := SaveState(filepath.Join(dir, "state.json"), st); err != nil {
		t.Fatalf("seed state.json for %s: %v", name, err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", dir, err)
	}
	return dir
}

// mustReadFile snapshots bytes for the byte-for-byte unchanged assertions.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// @s1 — implicit begin (no --base, no --ticket) aborts when the newest-by-
// mtime resolved run dir's state.json has a pending feature (Resumable() via
// NextPending). The error must name the resolved dir and the in-progress
// task, and the state.json on disk must be byte-for-byte unchanged.
func TestTddStateBeginGuard_S1_AbortsOnResumablePendingFeature(t *testing.T) {
	root := t.TempDir()

	now := time.Now()
	// An older, fully-done run — present so the newest-by-mtime resolution is
	// actually exercised (not just "the only dir").
	seedTddRunDir(t, root, "OLD-1-done", State{
		Version:  StateVersion,
		Task:     "old-finished-task",
		Features: []FeatureState{{Name: "old-feature", Status: "pass"}},
		Review:   "pass",
	}, now.Add(-2*time.Hour))

	// The newest run dir: one feature still pending -> Resumable() == true.
	resolvedDir := seedTddRunDir(t, root, "NEW-2-inprogress", State{
		Version:  StateVersion,
		Task:     "in-progress-task",
		Features: []FeatureState{{Name: "in-progress-feature", Status: "pending"}},
	}, now.Add(-1*time.Hour))
	statePath := filepath.Join(resolvedDir, "state.json")
	before := mustReadFile(t, statePath)

	var buf bytes.Buffer
	err := RunStateBegin(root, "", "", "fresh-task", "fresh-branch", []string{"fresh-feature"}, &buf)
	if err == nil {
		t.Fatalf("expected an abort error for an implicit begin over a resumable (pending-feature) run, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("NEW-2-inprogress")) {
		t.Errorf("error %q does not name the resolved dir (want it to mention %q)", err.Error(), "NEW-2-inprogress")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("in-progress-task")) {
		t.Errorf("error %q does not name the in-progress task (want it to mention %q)", err.Error(), "in-progress-task")
	}

	after := mustReadFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Errorf("state.json at %s was mutated by an aborted begin:\nbefore: %s\nafter:  %s", statePath, before, after)
	}
}

// @s2 — implicit begin aborts when every feature is "pass" but Review is
// still "pending" (Resumable() via the review-gate branch, not NextPending).
func TestTddStateBeginGuard_S2_AbortsOnResumableReviewPending(t *testing.T) {
	root := t.TempDir()

	now := time.Now()
	resolvedDir := seedTddRunDir(t, root, "REV-1-pending", State{
		Version:  StateVersion,
		Task:     "review-pending-task",
		Features: []FeatureState{{Name: "all-pass-feature", Status: "pass"}},
		Review:   "pending",
	}, now.Add(-30*time.Minute))
	statePath := filepath.Join(resolvedDir, "state.json")
	before := mustReadFile(t, statePath)

	var buf bytes.Buffer
	err := RunStateBegin(root, "", "", "fresh-task", "fresh-branch", []string{"fresh-feature"}, &buf)
	if err == nil {
		t.Fatalf("expected an abort error for an implicit begin over a resumable (review-pending) run, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("REV-1-pending")) {
		t.Errorf("error %q does not name the resolved dir (want it to mention %q)", err.Error(), "REV-1-pending")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("review-pending-task")) {
		t.Errorf("error %q does not name the in-progress task (want it to mention %q)", err.Error(), "review-pending-task")
	}

	after := mustReadFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Errorf("state.json at %s was mutated by an aborted begin:\nbefore: %s\nafter:  %s", statePath, before, after)
	}
}

// @s3 — an explicit --base naming the same resumable dir always proceeds and
// overwrites, regardless of the in-progress state (explicit intent wins).
func TestTddStateBeginGuard_S3_ExplicitBaseOverwritesResumableRun(t *testing.T) {
	root := t.TempDir()

	now := time.Now()
	resolvedDir := seedTddRunDir(t, root, "EXP-1-inprogress", State{
		Version:  StateVersion,
		Task:     "in-progress-task",
		Features: []FeatureState{{Name: "in-progress-feature", Status: "pending"}},
	}, now.Add(-1*time.Hour))
	statePath := filepath.Join(resolvedDir, "state.json")

	var buf bytes.Buffer
	baseFlag := filepath.Join(".tu-agent", "tdd", "EXP-1-inprogress")
	if err := RunStateBegin(root, baseFlag, "", "fresh-task-explicit", "fresh-branch", []string{"fresh-feature-explicit"}, &buf); err != nil {
		t.Fatalf("explicit --base begin over a resumable run should overwrite, got error: %v", err)
	}

	got, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("load overwritten state: %v", err)
	}
	if got.Task != "fresh-task-explicit" {
		t.Errorf("state.json Task = %q, want %q (overwritten)", got.Task, "fresh-task-explicit")
	}
	if _, ok := got.Feature("fresh-feature-explicit"); !ok {
		t.Errorf("state.json features = %+v, want the fresh feature from the overwriting begin", got.Features)
	}
	if _, ok := got.Feature("in-progress-feature"); ok {
		t.Errorf("state.json still has the old in-progress feature; explicit --base begin should have overwritten it")
	}
}

// @s4 — an implicit begin over a fully-done (non-resumable) state still
// proceeds and overwrites, exactly as before this fix.
func TestTddStateBeginGuard_S4_ImplicitOverwritesWhenFullyDone(t *testing.T) {
	root := t.TempDir()

	now := time.Now()
	resolvedDir := seedTddRunDir(t, root, "DONE-1-finished", State{
		Version:  StateVersion,
		Task:     "finished-task",
		Features: []FeatureState{{Name: "finished-feature", Status: "pass"}},
		Review:   "pass",
	}, now.Add(-1*time.Hour))
	statePath := filepath.Join(resolvedDir, "state.json")

	var buf bytes.Buffer
	if err := RunStateBegin(root, "", "", "fresh-task", "fresh-branch", []string{"fresh-feature"}, &buf); err != nil {
		t.Fatalf("implicit begin over a fully-done state should overwrite, got error: %v", err)
	}

	got, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("load overwritten state: %v", err)
	}
	if got.Task != "fresh-task" {
		t.Errorf("state.json Task = %q, want %q (overwritten)", got.Task, "fresh-task")
	}
	if _, ok := got.Feature("fresh-feature"); !ok {
		t.Errorf("state.json features = %+v, want the fresh feature from the overwriting begin", got.Features)
	}
	if _, ok := got.Feature("finished-feature"); ok {
		t.Errorf("state.json still has the old finished feature; implicit begin over a done run should have overwritten it")
	}
}

// ---------------------------------------------------------------------------
// review/status scenarios (@s3, @s4) — moved from
// cmd/tu-agent/tdd_review_state_test.go.
// ---------------------------------------------------------------------------

// writeReviewStateFile writes a state.json with the given review value into dir
// (an absolute path -> TddStateFile returns dir/state.json, ignoring root).
func writeReviewStateFile(t *testing.T, dir, review string) {
	t.Helper()
	raw := `{
		"version": 1,
		"task": "t",
		"review": "` + review + `",
		"features": [{"name": "a", "status": "pass"}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

// @s3 — `tdd status --base <dir>` JSON output exposes the review field with the
// on-disk value.
func TestTddStatusExposesReview(t *testing.T) {
	dir := t.TempDir()
	writeReviewStateFile(t, dir, "pending")

	var buf bytes.Buffer
	if err := RunStatus(t.TempDir(), dir, "", &buf); err != nil {
		t.Fatalf("status: %v", err)
	}
	var out struct {
		Review string `json:"review"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("status output not JSON: %q (%v)", buf.String(), err)
	}
	if out.Review != "pending" {
		t.Fatalf("status review = %q, want %q; output=%s", out.Review, "pending", buf.String())
	}
}

// @s4 — `tdd state review pass --base <dir>` persists review "pass".
func TestTddStateReviewPersists(t *testing.T) {
	dir := t.TempDir()
	writeReviewStateFile(t, dir, "pending")

	var buf bytes.Buffer
	if err := RunStateReview(t.TempDir(), dir, "", "pass", &buf); err != nil {
		t.Fatalf("state review pass: %v", err)
	}
	st, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.Review != "pass" {
		t.Fatalf("persisted Review = %q, want %q", st.Review, "pass")
	}
}

// @s4 — an invalid value is rejected with an error naming the allowed values.
func TestTddStateReviewRejectsInvalid(t *testing.T) {
	var buf bytes.Buffer
	err := RunStateReview(t.TempDir(), "", "", "bogus", &buf)
	if err == nil {
		t.Fatalf("expected error for bogus review value, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"pending", "pass", "skipped"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q must name allowed value %q", msg, want)
		}
	}
}
