package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeEngine is scriptable for the Run orchestration tests.
type fakeEngine struct {
	avail      bool
	report     Report
	perr       error
	workdir    string
	reportPath string
	gotSrc     *string // when set, Parse records the string it received
}

func (f fakeEngine) Name() string                  { return "fake" }
func (f fakeEngine) Available(_, _ string) bool    { return f.avail }
func (f fakeEngine) WorkDir(_, _ string) string    { return f.workdir }
func (f fakeEngine) Command(_, _ string) []string  { return []string{"fake", "run"} }
func (f fakeEngine) ReportPath(_, _ string) string { return f.reportPath }
func (f fakeEngine) Parse(s string) (Report, error) {
	if f.gotSrc != nil {
		*f.gotSrc = s
	}
	return f.report, f.perr
}

func TestRunToolAbsentSkips(t *testing.T) {
	eng := fakeEngine{avail: false}
	called := false
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		called = true
		return "", nil
	}
	rep := Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if !rep.Skipped || rep.Note == "" {
		t.Fatalf("absent tool should skip with a note: %+v", rep)
	}
	if called {
		t.Error("runner must not be called when the tool is absent")
	}
}

func TestRunParsesWhenPresent(t *testing.T) {
	eng := fakeEngine{avail: true, report: Report{Tool: "fake", Total: 4, Killed: 3, Survived: 1, Score: 0.75}}
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		return "ok", nil
	}
	rep := Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if rep.Skipped || rep.Killed != 3 || rep.Survived != 1 || rep.Score != 0.75 {
		t.Fatalf("present tool should parse: %+v", rep)
	}
}

func TestRunDegradesOnRunnerError(t *testing.T) {
	eng := fakeEngine{avail: true}
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		return "boom", errors.New("exit 2")
	}
	rep := Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if !rep.Skipped || rep.Note == "" {
		t.Fatalf("runner error should degrade to a skipped report: %+v", rep)
	}
}

func TestRunDegradesOnParseError(t *testing.T) {
	eng := fakeEngine{avail: true, perr: errors.New("bad report")}
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		return "garbage", nil
	}
	rep := Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if !rep.Skipped || rep.Note == "" {
		t.Fatalf("parse error should degrade to a skipped report: %+v", rep)
	}
}

func TestRun_executesInWorkDir(t *testing.T) {
	var gotDir string
	run := func(_ context.Context, dir string, _ []string, _ time.Duration) (string, error) {
		gotDir = dir
		return "ok", nil
	}
	_ = Run(context.Background(), fakeEngine{avail: true, workdir: "/tmp/pkg"}, "/repo", "packages/app", run, time.Second)
	if gotDir != "/tmp/pkg" {
		t.Errorf("Run cwd = %q, want /tmp/pkg (engine WorkDir)", gotDir)
	}
}

func TestEngineForUnknown(t *testing.T) {
	if _, ok := EngineFor("cobol"); ok {
		t.Error("unknown language should have no engine")
	}
}

func TestReadReport_exact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mutations.xml")
	if err := os.WriteFile(p, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readReport(p, time.Time{})
	if err != nil || got != "X" {
		t.Fatalf("readReport = %q,%v want X,nil", got, err)
	}
}

func TestReadReport_timestampedFallback(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pit-reports", "20260101")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "mutations.xml"), []byte("TS"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ask for the canonical (non-timestamped) path, which does not exist.
	got, err := readReport(filepath.Join(dir, "pit-reports", "mutations.xml"), time.Time{})
	if err != nil || got != "TS" {
		t.Fatalf("readReport fallback = %q,%v want TS,nil", got, err)
	}
}

func TestReadReport_newestWins(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "pit-reports")
	cases := []struct {
		sub, body string
		mod       time.Time
	}{
		{"a", "OLD", time.Now().Add(-time.Hour)},
		{"b", "NEW", time.Now()},
	}
	for _, c := range cases {
		d := filepath.Join(root, c.sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(d, "mutations.xml")
		if err := os.WriteFile(f, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, c.mod, c.mod); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readReport(filepath.Join(root, "mutations.xml"), time.Time{})
	if err != nil || got != "NEW" {
		t.Fatalf("readReport newest = %q,%v want NEW,nil", got, err)
	}
}

func TestReadReport_missing(t *testing.T) {
	if _, err := readReport(filepath.Join(t.TempDir(), "mutations.xml"), time.Time{}); err == nil {
		t.Fatal("readReport on missing report should error")
	}
}

func TestReadReport_rejectsStale(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mutations.xml")
	if err := os.WriteFile(p, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// after = now → the hour-old report is stale → treated as absent → error.
	if _, err := readReport(p, time.Now()); err == nil {
		t.Fatal("readReport should reject a report older than `after`")
	}
}

func TestReadReport_acceptsWithinSlack(t *testing.T) {
	// A report written during the run can get a modtime slightly BEFORE `after`
	// when the filesystem's timestamp granularity is coarser than time.Now()
	// (common on Linux/tmpfs; not on macOS APFS). It must still count as fresh.
	dir := t.TempDir()
	p := filepath.Join(dir, "mutations.xml")
	if err := os.WriteFile(p, []byte("FRESH"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	skew := after.Add(-500 * time.Millisecond) // truncated just below `after`
	if err := os.Chtimes(p, skew, skew); err != nil {
		t.Fatal(err)
	}
	got, err := readReport(p, after)
	if err != nil || got != "FRESH" {
		t.Fatalf("readReport within slack = %q,%v want FRESH,nil", got, err)
	}
}

func TestRunReadsReportFile(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "mutations.xml")
	var got string
	eng := fakeEngine{avail: true, reportPath: report, gotSrc: &got}
	// Write the report inside the runner so its modtime is >= start (which Run
	// stamps just before calling the runner).
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		if err := os.WriteFile(report, []byte("FILE-CONTENT"), 0o644); err != nil {
			t.Error(err)
		}
		return "STDOUT-CONTENT", nil
	}
	Run(context.Background(), eng, dir, "pkg", run, time.Second)
	if got != "FILE-CONTENT" {
		t.Fatalf("Parse received %q, want the report FILE content (not stdout)", got)
	}
}

func TestRunParsesStdoutWhenNoReportPath(t *testing.T) {
	var got string
	eng := fakeEngine{avail: true, gotSrc: &got} // reportPath "" → stdout
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		return "STDOUT-CONTENT", nil
	}
	Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if got != "STDOUT-CONTENT" {
		t.Fatalf("Parse received %q, want stdout when ReportPath is empty", got)
	}
}

func TestRunSkipsWhenReportMissing(t *testing.T) {
	eng := fakeEngine{avail: true, reportPath: filepath.Join(t.TempDir(), "nope", "mutations.xml")}
	run := func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
		return "ok", nil
	}
	rep := Run(context.Background(), eng, ".", "pkg", run, time.Second)
	if !rep.Skipped || rep.Note == "" {
		t.Fatalf("missing report should degrade to skipped: %+v", rep)
	}
}
