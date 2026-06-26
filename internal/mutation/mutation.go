// Package mutation wraps external mutation-testing CLIs (go-mutesting, PIT,
// mutmut, Stryker) as subprocesses. It detects the tool and degrades to a
// logged, non-fatal "skipped" report when the tool is absent or misbehaves —
// mutation is an advisory quality gate, never a hard failure. We ship no
// mutation engine of our own (spec §9, §10).
package mutation

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Survivor is one mutant the test suite failed to kill.
type Survivor struct {
	File string
	Line int
	Desc string
}

// Report summarizes one mutation run. Skipped is true when the tool was absent
// or the run/parse degraded; Note explains why.
type Report struct {
	Tool      string
	Total     int
	Killed    int
	Survived  int
	Score     float64
	Survivors []Survivor
	Skipped   bool
	Note      string
}

// Runner executes argv in dir under a timeout, returning combined output.
// Injectable so tests never shell out.
type Runner func(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, error)

// Engine wraps one language's mutation CLI.
type Engine interface {
	Name() string
	Available(repoRoot, pkgDir string) bool
	WorkDir(repoRoot, pkgDir string) string
	Command(repoRoot, pkgDir string) []string
	// ReportPath returns the canonical path of the machine-readable report that
	// Parse consumes, or "" when the engine prints its report to stdout (Run
	// then passes the command's stdout to Parse). The path may not exist
	// verbatim — Run falls back to the newest file of the same basename under
	// the path's directory (covering timestamped subdirectories).
	ReportPath(repoRoot, pkgDir string) string
	Parse(output string) (Report, error)
}

// Run executes eng over pkgDir, degrading to a non-fatal skipped report on any
// of: tool absent, runner error, parse error. It never returns an error and
// never panics — callers treat mutation as advisory.
func Run(ctx context.Context, eng Engine, repoRoot, pkgDir string, run Runner, timeout time.Duration) Report {
	if !eng.Available(repoRoot, pkgDir) {
		note := fmt.Sprintf("%s not found; mutation skipped", eng.Name())
		slog.Info("mutation: tool absent, skipping", "tool", eng.Name())
		return Report{Tool: eng.Name(), Skipped: true, Note: note}
	}
	start := time.Now()
	out, err := run(ctx, eng.WorkDir(repoRoot, pkgDir), eng.Command(repoRoot, pkgDir), timeout)
	if err != nil {
		note := fmt.Sprintf("%s run failed: %v", eng.Name(), err)
		slog.Warn("mutation: run failed, skipping", "tool", eng.Name(), "err", err)
		return Report{Tool: eng.Name(), Skipped: true, Note: note}
	}
	src := out
	if rp := eng.ReportPath(repoRoot, pkgDir); rp != "" {
		data, rerr := readReport(rp, start)
		if rerr != nil {
			note := fmt.Sprintf("%s report not found (%v); mutation skipped", eng.Name(), rerr)
			slog.Warn("mutation: report unreadable, skipping", "tool", eng.Name(), "err", rerr)
			return Report{Tool: eng.Name(), Skipped: true, Note: note}
		}
		src = data
	}
	rep, perr := eng.Parse(src)
	if perr != nil {
		note := fmt.Sprintf("%s report parse failed: %v", eng.Name(), perr)
		slog.Warn("mutation: parse failed, skipping", "tool", eng.Name(), "err", perr)
		return Report{Tool: eng.Name(), Skipped: true, Note: note}
	}
	rep.Tool = eng.Name()
	return rep
}

// EngineFor returns the mutation engine for a graph language name. Real engines
// are registered in the per-language files; the default is none.
func EngineFor(language string) (Engine, bool) {
	switch language {
	case "go":
		return goEngine{}, true
	case "java":
		return javaEngine{}, true
	case "python":
		return pythonEngine{}, true
	case "typescript":
		return tsEngine{}, true
	default:
		return nil, false
	}
}

// stringReader avoids importing strings in every engine file.
func stringReader(s string) *strings.Reader { return strings.NewReader(s) }

// readReport returns the contents of the report at path, ignoring any report
// older than `after` (a stale report from a previous run is treated as absent).
// When the exact path is missing or stale, it returns the newest fresh file of
// the same basename anywhere under path's directory (timestamped-subdir
// fallback). Errors when no fresh report exists.
func readReport(path string, after time.Time) (string, error) {
	if info, err := os.Stat(path); err == nil && !info.ModTime().Before(after) {
		if data, rerr := os.ReadFile(path); rerr == nil {
			return string(data), nil
		}
	}
	base := filepath.Base(path)
	var newest string
	var newestMod time.Time
	_ = filepath.WalkDir(filepath.Dir(path), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != base {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.ModTime().Before(after) {
			return nil
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest, newestMod = p, info.ModTime()
		}
		return nil
	})
	if newest == "" {
		return "", fmt.Errorf("mutation.readReport: no fresh %s under %s", base, filepath.Dir(path))
	}
	data, err := os.ReadFile(newest)
	if err != nil {
		return "", fmt.Errorf("mutation.readReport: %w", err)
	}
	return string(data), nil
}
