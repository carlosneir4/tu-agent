package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

// resetMemoryCmds clears package-level flag vars and resets command output
// writers. Call at the start of each test (and register a Cleanup) so that
// global state from one test never bleeds into the next.
// NOTE: these tests must NOT call t.Parallel() because they write to shared
// package-level vars (memSaveTopic, memSaveContent, etc.).
func resetMemoryCmds(t *testing.T) {
	t.Helper()
	memSaveTopic, memSaveContent, memSaveType, memSaveSource = "", "", "", ""
	memSearchType = ""
	memShowIDs = false
	memorySaveCmd.SetOut(nil)
	memoryListCmd.SetOut(nil)
	memorySearchCmd.SetOut(nil)
	memRelinkQuiet = false
	memoryRelinkCmd.SetOut(nil)
}

func runMemorySave(t *testing.T, topic, content string) {
	t.Helper()
	memSaveTopic, memSaveContent = topic, content
	memSaveType, memSaveSource = "", ""
	if err := memorySaveCmd.RunE(memorySaveCmd, nil); err != nil {
		t.Fatalf("memory save: %v", err)
	}
}

func TestMemoryCLI_SaveListRevisionBump(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	runMemorySave(t, "architecture/auth", "v1 content")
	runMemorySave(t, "architecture/auth", "v2 content")

	var buf bytes.Buffer
	memoryListCmd.SetOut(&buf)
	if err := memoryListCmd.RunE(memoryListCmd, nil); err != nil {
		t.Fatalf("memory list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "rev:2") {
		t.Errorf("expected rev:2 in list output, got:\n%s", out)
	}
	if !strings.Contains(out, "architecture/auth") {
		t.Errorf("expected topic key in list output, got:\n%s", out)
	}
	if strings.Count(out, "architecture/auth") != 1 {
		t.Errorf("expected exactly one row for the topic, got:\n%s", out)
	}
}

// listObservationID saves one note and returns its full ID, for tests that
// assert on whether the ID appears in list output.
func listObservationID(t *testing.T, topic, content string) string {
	t.Helper()
	runMemorySave(t, topic, content)
	ms, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	recent, err := ms.Recent(1)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	_ = ms.Close()
	if len(recent) != 1 {
		t.Fatalf("want 1 observation, got %d", len(recent))
	}
	return recent[0].ID
}

// TestMemoryListShowsIDWithFlag ensures `memory list --ids` prints the full
// observation ID so a user can copy it into `memory link --from <id>` (the
// relation join is by exact ID).
func TestMemoryListShowsIDWithFlag(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	id := listObservationID(t, "gotcha/order", "orders have a subtle invariant")

	var buf bytes.Buffer
	memoryListCmd.SetOut(&buf)
	memShowIDs = true
	if err := memoryListCmd.RunE(memoryListCmd, nil); err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if !strings.Contains(buf.String(), id) {
		t.Errorf("memory list --ids must show the full observation ID %q:\n%s", id, buf.String())
	}
}

// TestMemoryListHidesIDByDefault ensures the default list output drops the noisy
// hex ID and surfaces the observation type instead.
func TestMemoryListHidesIDByDefault(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	id := listObservationID(t, "gotcha/order", "orders have a subtle invariant")

	var buf bytes.Buffer
	memoryListCmd.SetOut(&buf)
	if err := memoryListCmd.RunE(memoryListCmd, nil); err != nil {
		t.Fatalf("memory list: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, id) {
		t.Errorf("default memory list must NOT show the hex ID %q:\n%s", id, out)
	}
	if !strings.Contains(out, "gotcha") {
		t.Errorf("default memory list must show the type column:\n%s", out)
	}
}

func TestMemoryCLI_SaveRequiresFlags(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	if err := memorySaveCmd.RunE(memorySaveCmd, nil); err == nil {
		t.Fatal("expected error when --topic/--content missing")
	}
}

func TestMemoryCLI_Search(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	runMemorySave(t, "cache/invalidation", "TTL is 60 seconds")

	var buf bytes.Buffer
	memorySearchCmd.SetOut(&buf)
	// Search uses case-insensitive SQL lower() matching.
	if err := memorySearchCmd.RunE(memorySearchCmd, []string{"ttl"}); err != nil {
		t.Fatalf("memory search: %v", err)
	}
	if !strings.Contains(buf.String(), "cache/invalidation") {
		t.Errorf("expected matching topic in search output, got:\n%s", buf.String())
	}

	buf.Reset()
	if err := memorySearchCmd.RunE(memorySearchCmd, []string{"zzz-no-match"}); err != nil {
		t.Fatalf("memory search (no match): %v", err)
	}
	if !strings.Contains(buf.String(), "no observations") {
		t.Errorf("expected no-match message, got:\n%s", buf.String())
	}
}

func TestMemoryLinkCLI(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runMemoryLink("obs-1", "pkg/x.go::Foo", "related", io.Discard); err != nil {
		t.Fatalf("runMemoryLink: %v", err)
	}
	var out strings.Builder
	if err := runMemoryLinks("pkg/x.go::Foo", &out); err != nil {
		t.Fatalf("runMemoryLinks: %v", err)
	}
	if !strings.Contains(out.String(), "obs-1") || !strings.Contains(out.String(), "related") {
		t.Errorf("links output missing relation: %q", out.String())
	}
}

func TestMemoryCLI_SearchTypeFilter(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("trap/x", "alpha trap", memory.UpsertOpts{Type: "gotcha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Upsert("design/x", "alpha decision", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	ms.Close()

	var buf bytes.Buffer
	memorySearchCmd.SetOut(&buf)
	if err := memorySearchCmd.Flags().Set("type", "gotcha"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memorySearchCmd.Flags().Set("type", "") })
	if err := memorySearchCmd.RunE(memorySearchCmd, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "trap/x") || strings.Contains(out, "design/x") {
		t.Errorf("--type gotcha should show only trap/x, got:\n%s", out)
	}
}

func TestMemoryCLI_Relink_NoGraphIsNoop(t *testing.T) {
	resetMemoryCmds(t)
	t.Cleanup(func() { resetMemoryCmds(t) })
	t.Chdir(t.TempDir())

	runMemorySave(t, "bug-pattern/x", "OrderService note")

	var buf bytes.Buffer
	memoryRelinkCmd.SetOut(&buf)
	if err := memoryRelinkCmd.RunE(memoryRelinkCmd, nil); err != nil {
		t.Fatalf("memory relink: %v", err)
	}
	if !strings.Contains(buf.String(), "graph") {
		t.Errorf("expected a graph-related message, got: %q", buf.String())
	}
}

func TestPrintObservationLineStale(t *testing.T) {
	o := memory.Observation{ID: "id1", TopicKey: "decision/x", Revision: 2, Content: "first line\nsecond"}

	var flagged bytes.Buffer
	printObservationLine(&flagged, o, 2, false)
	if !strings.Contains(flagged.String(), "⚠stale:2") {
		t.Errorf("missing stale suffix: %q", flagged.String())
	}

	var clean bytes.Buffer
	printObservationLine(&clean, o, 0, false)
	if strings.Contains(clean.String(), "stale") {
		t.Errorf("staleCount 0 must not annotate: %q", clean.String())
	}
}
