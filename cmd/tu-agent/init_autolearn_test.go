package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

// ---------------------------------------------------------------------------
// RED-phase tests for feature `prepare-auto-learn`.
//
// Surface under test: runInitSetup (cmd/tu-agent/init.go) — the deterministic
// "prepare" setup path. When the concept store is empty AND the normal setup
// path runs (not --augment-agents), prepare must auto-run the deterministic
// learn pipeline (runLearn with SkipLLM=true — no provider/model calls, §10
// plugin-first) so the graph/knowledge is populated without a second manual step.
//
// CONTRACT for the implementer — the auto-learn announcement line:
//   When prepare kicks off the auto-learn run it MUST print ONE line to stdout
//   that contains BOTH the substrings "concept store" and "learn" (case-
//   insensitive), e.g.:
//       "prepare: concept store is empty — running the deterministic learn pipeline"
//   The tests key on autoLearnAnnouncementMarker ("concept store") as the stable
//   announcement fingerprint. Keep that phrase in the announcement line.
//
//   The auto-learn run MUST invoke runLearn with SkipLLM=true, which prints the
//   "--skip-llm: cards keep deterministic descriptions..." marker (learn.go) and
//   makes no model call (no telemetry row with a non-empty Model field).
//
//   On auto-learn FAILURE prepare MUST NOT abort: it returns nil (setup already
//   succeeded) and writes a warning to stderr containing both "warn" and "learn".
// ---------------------------------------------------------------------------

// autoLearnAnnouncementMarker is the stable substring the implementer must emit
// on stdout when prepare triggers the auto-learn run. Tests assert its presence
// (s1) and, on the idempotent/early-return paths, its absence (s3, s5).
const autoLearnAnnouncementMarker = "concept store"

// captureOutErr redirects os.Stdout AND os.Stderr for the duration of fn and
// returns what was written to each plus fn's error. Streams are drained in
// goroutines so a large amount of pipeline output cannot deadlock on a full pipe
// buffer. Not parallel-safe: it swaps process-global os.Stdout/os.Stderr, so
// callers must not use t.Parallel().
func captureOutErr(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, e1 := os.Pipe()
	if e1 != nil {
		t.Fatalf("os.Pipe (stdout): %v", e1)
	}
	rErr, wErr, e2 := os.Pipe()
	if e2 != nil {
		t.Fatalf("os.Pipe (stderr): %v", e2)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	var wg sync.WaitGroup
	var outBuf, errBuf []byte
	wg.Add(2)
	go func() { defer wg.Done(); outBuf, _ = io.ReadAll(rOut) }()
	go func() { defer wg.Done(); errBuf, _ = io.ReadAll(rErr) }()

	runErr := fn()
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	return string(outBuf), string(errBuf), runErr
}

// writeGoRepo lays down a minimal but realistic Go module under root: a go.mod
// plus two importable packages and a main that wires them, so the learn pipeline
// has real source units to map into at least one concept card.
func writeGoRepo(t *testing.T, root string) {
	t.Helper()
	writeFileTree(t, root, "go.mod", "module example.com/demo\n\ngo 1.22\n")
	writeFileTree(t, root, "main.go",
		"package main\n\nimport (\n\t\"example.com/demo/internal/greet\"\n\t\"example.com/demo/internal/store\"\n)\n\nfunc main() {\n\ts := store.New()\n\ts.Put(\"k\", greet.Hello(\"world\"))\n}\n")
	writeFileTree(t, root, "internal/greet/greet.go",
		"package greet\n\n// Hello returns a greeting for name.\nfunc Hello(name string) string { return \"hello \" + name }\n")
	writeFileTree(t, root, "internal/store/store.go",
		"package store\n\n// Store is a tiny in-memory key/value store.\ntype Store struct{ m map[string]string }\n\nfunc New() *Store { return &Store{m: map[string]string{}} }\n\nfunc (s *Store) Put(k, v string) { s.m[k] = v }\n")
}

// telemetryHasModelCall reports whether any telemetry row records a provider/
// model call (a non-empty Model field). Deterministic rows (e.g. graph_refresh)
// leave Model empty, so this isolates real model calls without a seam.
func telemetryHasModelCall(t *testing.T, root string) bool {
	t.Helper()
	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	for _, e := range entries {
		if e.Model != "" {
			return true
		}
	}
	return false
}

// @s1 — an empty concept store triggers a deterministic learn run: afterward the
// store holds >=1 concept and stdout announced the auto-learn run.
func TestPrepareAutoLearn_EmptyStoreTriggersLearn(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)

	// Precondition: the concept store starts empty.
	before, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills (before): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("precondition: concept store must start empty, got %d concept(s)", len(before))
	}

	stdout, _, err := captureOutErr(t, func() error {
		return runInitSetup(context.Background(), initSetupOpts{Lang: "go", NoHarden: true, Force: true})
	})
	if err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	after, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills (after): %v", err)
	}
	if len(after) == 0 {
		t.Errorf("expected auto-learn to populate the concept store (>=1 concept), got 0")
	}

	low := strings.ToLower(stdout)
	if !strings.Contains(low, autoLearnAnnouncementMarker) || !strings.Contains(low, "learn") {
		t.Errorf("stdout missing auto-learn announcement (want a line containing %q and %q):\n%s",
			autoLearnAnnouncementMarker, "learn", stdout)
	}
}

// @s2 — the auto-learn run is deterministic: it invokes runLearn with
// SkipLLM=true (its "--skip-llm" marker appears on stdout) and makes no
// provider/model call (no telemetry row with a Model).
func TestPrepareAutoLearn_SkipsLLMNoProviderCall(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)

	stdout, _, err := captureOutErr(t, func() error {
		return runInitSetup(context.Background(), initSetupOpts{Lang: "go", NoHarden: true, Force: true})
	})
	if err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	// runLearn prints this marker ONLY when SkipLLM==true (learn.go), so its
	// presence proves the deterministic path was taken — no seam required.
	if !strings.Contains(stdout, "--skip-llm") {
		t.Errorf("stdout missing the runLearn --skip-llm marker (auto-learn must run with SkipLLM=true):\n%s", stdout)
	}
	if telemetryHasModelCall(t, root) {
		t.Errorf("prepare auto-learn must not make a provider/model call (found a telemetry row with a Model)")
	}
}

// @s3 — a populated concept store is left untouched (idempotent): learn is not
// re-invoked, no announcement is printed, and the concept count is unchanged.
func TestPrepareAutoLearn_PopulatedStoreUntouched(t *testing.T) {
	root := t.TempDir()
	writeGoRepo(t, root)
	t.Chdir(root)

	// Pre-populate the store with one concept.
	seedConcept(t)
	before, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills (before): %v", err)
	}
	if len(before) == 0 {
		t.Fatalf("precondition: expected a pre-seeded concept, got 0")
	}

	stdout, _, err := captureOutErr(t, func() error {
		return runInitSetup(context.Background(), initSetupOpts{Lang: "go", NoHarden: true})
	})
	if err != nil {
		t.Fatalf("runInitSetup: %v", err)
	}

	if strings.Contains(strings.ToLower(stdout), autoLearnAnnouncementMarker) {
		t.Errorf("populated store must not trigger the auto-learn announcement, but stdout contains %q:\n%s",
			autoLearnAnnouncementMarker, stdout)
	}
	after, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills (after): %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("concept count changed on a populated store: before=%d after=%d (auto-learn must be skipped)",
			len(before), len(after))
	}
}

// @s4 — auto-learn failure degrades to a warning and does not abort prepare.
// An empty repo with --lang go forces setup to succeed, but the learn pipeline
// then fails ("no source files found"): prepare must return nil and warn on stderr.
func TestPrepareAutoLearn_FailureDegradesToWarning(t *testing.T) {
	root := t.TempDir() // no source files → learn will fail
	t.Chdir(root)

	_, stderr, err := captureOutErr(t, func() error {
		return runInitSetup(context.Background(), initSetupOpts{Lang: "go", NoHarden: true})
	})
	if err != nil {
		t.Fatalf("runInitSetup must return nil even when auto-learn fails (setup succeeded), got: %v", err)
	}

	low := strings.ToLower(stderr)
	if !strings.Contains(low, "warn") || !strings.Contains(low, "learn") {
		t.Errorf("expected a stderr warning about the failed auto-learn (containing %q and %q):\n%s",
			"warn", "learn", stderr)
	}
}

// @s5 — the --augment-agents path does not trigger auto-learn: it takes the
// early-return branch before any learn logic, so no announcement is printed and
// the concept store is left empty.
func TestPrepareAutoLearn_AugmentAgentsSkipsLearn(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stdout, _, err := captureOutErr(t, func() error {
		return runInitSetup(context.Background(), initSetupOpts{AugmentAgents: true, NoHarden: true})
	})
	if err != nil {
		t.Fatalf("runInitSetup (augment-agents): %v", err)
	}

	if strings.Contains(strings.ToLower(stdout), autoLearnAnnouncementMarker) {
		t.Errorf("--augment-agents must not trigger auto-learn, but stdout contains %q:\n%s",
			autoLearnAnnouncementMarker, stdout)
	}
	concepts, err := loadConceptSkills()
	if err != nil {
		t.Fatalf("loadConceptSkills: %v", err)
	}
	if len(concepts) != 0 {
		t.Errorf("--augment-agents must not populate the concept store, got %d concept(s)", len(concepts))
	}
}
