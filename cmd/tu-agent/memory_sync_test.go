package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

func TestMemoryExportImportCLI(t *testing.T) {
	// Author A records a decision and exports it to a chunk directory.
	chunks := t.TempDir()

	srcDB := filepath.Join(t.TempDir(), "memory.db")
	s, err := memory.Open(srcDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/cache", "use redis", memory.UpsertOpts{Author: "alice"}); err != nil {
		t.Fatal(err)
	}
	recs, err := s.ExportRecords("alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := memory.WriteChunk(chunks, "alice", recs); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Author B imports the same chunk dir into a fresh DB via ReadAllChunks.
	back, err := memory.ReadAllChunks(chunks)
	if err != nil {
		t.Fatal(err)
	}
	dstDB := filepath.Join(t.TempDir(), "memory.db")
	d, err := memory.Open(dstDB)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	res, err := d.ImportRecords(back)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 {
		t.Fatalf("want 1 inserted, got %+v", res)
	}
	got, _, _ := d.Search("redis", "", 0)
	if len(got) != 1 {
		t.Fatalf("imported decision not searchable: %+v", got)
	}
}

func TestMemoryChunksDirPath(t *testing.T) {
	got := memoryChunksDir("/repo")
	want := filepath.Join("/repo", ".tu-agent", "share", "memory", "chunks")
	if got != want {
		t.Fatalf("memoryChunksDir = %q, want %q", got, want)
	}
}

func TestMemoryImportQuiet(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	memImportQuiet = true
	t.Cleanup(func() { memImportQuiet = false })

	var out bytes.Buffer
	memoryImportCmd.SetOut(&out)
	if err := memoryImportCmd.RunE(memoryImportCmd, nil); err != nil {
		t.Fatalf("import --quiet: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("quiet import must print nothing, got %q", out.String())
	}
}

func TestMemoryExportQuiet(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	memExportQuiet = true
	t.Cleanup(func() { memExportQuiet = false })

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export --quiet: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("quiet export must print nothing, got %q", out.String())
	}
}

func TestMemoryExportCmdRun(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/x", "body", memory.UpsertOpts{Author: "tester"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}
	entries, _ := os.ReadDir(memoryChunksDir("."))
	if len(entries) != 1 {
		t.Fatalf("export must write one chunk file, got %d", len(entries))
	}

	// Second export with no changes must take the "up to date" branch and not error.
	var out2 bytes.Buffer
	memoryExportCmd.SetOut(&out2)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("second export run: %v", err)
	}
	if got := out2.String(); !strings.Contains(got, "up to date") {
		t.Fatalf("second export: want 'up to date' message, got %q", got)
	}
	entries2, _ := os.ReadDir(memoryChunksDir("."))
	if len(entries2) != 1 {
		t.Fatalf("second export must not create extra files, got %d", len(entries2))
	}
}

// TestMemoryExportStderrSummary verifies that export announces new team notes
// on stderr regardless of --quiet, and stays silent on stderr when the chunk
// is unchanged.
func TestMemoryExportStderrSummary(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/a", "body a", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/b", "body b", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out, errOut bytes.Buffer
	memoryExportCmd.SetOut(&out)
	memoryExportCmd.SetErr(&errOut)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}
	want := "2 new/updated team notes exported — review with 'tu-agent memory pending'"
	if !strings.Contains(errOut.String(), want) {
		t.Fatalf("first export: want stderr to contain %q, got %q", want, errOut.String())
	}

	// Second export, unchanged: stderr must stay silent.
	var out2, errOut2 bytes.Buffer
	memoryExportCmd.SetOut(&out2)
	memoryExportCmd.SetErr(&errOut2)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("second export run: %v", err)
	}
	if errOut2.Len() != 0 {
		t.Fatalf("unchanged export: want silent stderr, got %q", errOut2.String())
	}

	// Edit an existing note (same topic key, new content): computeSyncID is
	// content-independent, so decision/a keeps its sync_id and only bumps
	// Revision. Re-exporting must still announce it — a presence-only diff
	// would silently miss this.
	s4, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s4.Upsert("decision/a", "body a, revised", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	_ = s4.Close()

	var out4, errOut4 bytes.Buffer
	memoryExportCmd.SetOut(&out4)
	memoryExportCmd.SetErr(&errOut4)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("edited-note export run: %v", err)
	}
	wantEdited := "1 new/updated team notes exported — review with 'tu-agent memory pending'"
	if !strings.Contains(errOut4.String(), wantEdited) {
		t.Fatalf("edited-note export: want stderr to contain %q, got %q", wantEdited, errOut4.String())
	}

	// A brand-new note, exported with --quiet: stdout silent, stderr still announces it.
	s3, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Upsert("decision/c", "body c", memory.UpsertOpts{}); err != nil {
		t.Fatal(err)
	}
	_ = s3.Close()

	memExportQuiet = true
	t.Cleanup(func() { memExportQuiet = false })
	var out3, errOut3 bytes.Buffer
	memoryExportCmd.SetOut(&out3)
	memoryExportCmd.SetErr(&errOut3)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("quiet export run: %v", err)
	}
	if out3.Len() != 0 {
		t.Fatalf("quiet export: want empty stdout, got %q", out3.String())
	}
	if !strings.Contains(errOut3.String(), "1 new/updated team notes exported") {
		t.Fatalf("quiet export: want stderr summary for the new note, got %q", errOut3.String())
	}
}

// runGitIn runs a git command in dir, failing the test on error.
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestMemoryPending exercises the human pre-commit review surface: it must
// list exactly the notes exported to the working-tree chunk but not yet
// present in the git HEAD version of that chunk file.
func TestMemoryPending(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/a", "body a", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	memoryExportCmd.SetErr(io.Discard)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}

	runGitIn(t, dir, "add", ".tu-agent/share/memory/chunks")
	runGitIn(t, dir, "commit", "-m", "chunk: initial")

	var pendingOut bytes.Buffer
	memoryPendingCmd.SetOut(&pendingOut)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("pending run: %v", err)
	}
	if !strings.Contains(pendingOut.String(), "nothing pending") {
		t.Fatalf("want nothing pending right after commit, got %q", pendingOut.String())
	}

	// Save and export one more note.
	s2, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Upsert("decision/b", "body b", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()

	var out2 bytes.Buffer
	memoryExportCmd.SetOut(&out2)
	memoryExportCmd.SetErr(io.Discard)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("second export run: %v", err)
	}

	var pendingOut2 bytes.Buffer
	memoryPendingCmd.SetOut(&pendingOut2)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("second pending run: %v", err)
	}
	got := pendingOut2.String()
	if !strings.Contains(got, "decision/b") {
		t.Fatalf("want the new note listed, got %q", got)
	}
	if strings.Contains(got, "decision/a") {
		t.Fatalf("want only the new note listed, got %q", got)
	}

	// Commit again: nothing pending.
	runGitIn(t, dir, "add", ".tu-agent/share/memory/chunks")
	runGitIn(t, dir, "commit", "-m", "chunk: second")

	var pendingOut3 bytes.Buffer
	memoryPendingCmd.SetOut(&pendingOut3)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("third pending run: %v", err)
	}
	if !strings.Contains(pendingOut3.String(), "nothing pending") {
		t.Fatalf("want nothing pending after second commit, got %q", pendingOut3.String())
	}

	// Edit note a (same topic key, new content) and export again: sync_id is
	// content-independent so decision/a keeps it and only Revision bumps.
	// Pending must list it, distinguished from a new note by "(edited)".
	s3, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Upsert("decision/a", "body a, revised", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	_ = s3.Close()

	var out3 bytes.Buffer
	memoryExportCmd.SetOut(&out3)
	memoryExportCmd.SetErr(io.Discard)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("third export run: %v", err)
	}

	var pendingOut4 bytes.Buffer
	memoryPendingCmd.SetOut(&pendingOut4)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("fourth pending run: %v", err)
	}
	got4 := pendingOut4.String()
	if !strings.Contains(got4, "decision/a") {
		t.Fatalf("want the edited note listed, got %q", got4)
	}
	if !strings.Contains(got4, "(edited)") {
		t.Fatalf("want the edited note suffixed (edited), got %q", got4)
	}
	if strings.Contains(got4, "nothing pending") {
		t.Fatalf("want pending to report the edit, not 'nothing pending', got %q", got4)
	}
}

// TestMemoryPendingNoGit verifies that outside any git repo, `memory pending`
// lists every working-tree note rather than erroring.
func TestMemoryPendingNoGit(t *testing.T) {
	dir := t.TempDir()
	// A git repo with a configured identity but nothing committed: this still
	// exercises the "nothing committed yet" path (headChunkRecords hits an
	// unborn HEAD), while giving gitAuthor() a deterministic value regardless
	// of the test machine's global git config.
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/x", "body x", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	memoryExportCmd.SetErr(io.Discard)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}

	var pendingOut bytes.Buffer
	memoryPendingCmd.SetOut(&pendingOut)
	if err := memoryPendingCmd.RunE(memoryPendingCmd, nil); err != nil {
		t.Fatalf("pending run: %v", err)
	}
	got := pendingOut.String()
	if !strings.Contains(got, "not committed yet") || !strings.Contains(got, "showing all") {
		t.Fatalf("want a 'not committed yet ... showing all' message, got %q", got)
	}
	if !strings.Contains(got, "decision/x") {
		t.Fatalf("want the note listed, got %q", got)
	}
}

// TestMemoryPendingCorruptCommittedChunk verifies that a CORRUPT chunk file
// already committed at git HEAD (gzip decodes fine, but the JSON payload is
// garbage) surfaces as a real error from runMemoryPending — NOT silently
// swallowed as "not committed yet". A committed chunk that fails to parse is
// a team-wide problem the user must see.
func TestMemoryPendingCorruptCommittedChunk(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")

	author := "tester@example.com"
	chunksDir := memoryChunksDir(dir)
	if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chunkPath := memory.ChunkPath(chunksDir, author)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte("not valid json {")); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	runGitIn(t, dir, "add", ".tu-agent/share/memory/chunks")
	runGitIn(t, dir, "commit", "-m", "chunk: corrupt")

	var out bytes.Buffer
	err := runMemoryPending(&out, dir, author)
	if err == nil {
		t.Fatalf("want a non-nil error for a corrupt committed chunk, got nil (output: %q)", out.String())
	}
	if errors.Is(err, errChunkAbsentAtHead) {
		t.Fatalf("want a real parse error, got errChunkAbsentAtHead: %v", err)
	}
}

// TestMemoryPendingRunHelperNotCommittedYet is the companion "green" case: a
// fresh git repo with nothing committed at the chunk path must report "not
// committed yet" and return nil, not error.
func TestMemoryPendingRunHelperNotCommittedYet(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")

	author := "tester@example.com"
	s, err := memory.Open(memoryDBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/x", "body x", memory.UpsertOpts{Type: "decision", Author: author}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	recs, err := memory.ReadAllChunks(memoryChunksDir(dir))
	_ = recs
	if err != nil {
		t.Fatal(err)
	}
	// Export via the store directly, mirroring memoryExportCmd, to populate the
	// working-tree chunk without requiring a commit.
	s2, err := memory.Open(memoryDBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	exported, err := s2.ExportRecords(author)
	if err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()
	if _, _, err := memory.WriteChunk(memoryChunksDir(dir), author, exported); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMemoryPending(&out, dir, author); err != nil {
		t.Fatalf("runMemoryPending: %v", err)
	}
	if !strings.Contains(out.String(), "not committed yet") {
		t.Fatalf("want 'not committed yet' message, got %q", out.String())
	}
}

// TestMemoryExportExcludesSecretNotes verifies that a note whose content
// appears to embed a live secret is never written to the shared chunk file —
// team chunks are shared via git, so a leaked secret must never land there —
// while an ordinary clean note is still exported, and a warning is printed to
// stderr naming the excluded note.
func TestMemoryExportExcludesSecretNotes(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/clean", "an ordinary decision note", memory.UpsertOpts{Type: "decision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("gotcha/leaked", "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", memory.UpsertOpts{Type: "gotcha"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out, errOut bytes.Buffer
	memoryExportCmd.SetOut(&out)
	memoryExportCmd.SetErr(&errOut)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}

	author := "tester-example-com"
	chunkPath := memory.ChunkPath(memoryChunksDir("."), author)
	recs, err := memory.ReadChunkFile(chunkPath)
	if err != nil {
		t.Fatalf("read written chunk: %v", err)
	}
	foundClean, foundSecret := false, false
	for _, r := range recs {
		switch r.TopicKey {
		case "decision/clean":
			foundClean = true
		case "gotcha/leaked":
			foundSecret = true
		}
	}
	if !foundClean {
		t.Errorf("want clean note present in exported chunk, got %+v", recs)
	}
	if foundSecret {
		t.Errorf("want secret note EXCLUDED from exported chunk, got %+v", recs)
	}
	if !strings.Contains(errOut.String(), "excluded") {
		t.Errorf("want a secret-exclusion warning on stderr, got %q", errOut.String())
	}
}

// TestMemoryPendingSurfacesRemovedNotes verifies that a note present in the
// git-HEAD-committed chunk but absent from the current WORKING chunk (e.g.
// because the secret filter dropped it on the last export) is surfaced by
// `memory pending` under a removal section — not silently invisible.
// diffChunkRecords alone only iterates cur (the working chunk), so a note
// that dropped OUT of the working chunk never appears; this is the red-first
// regression case for that gap.
func TestMemoryPendingSurfacesRemovedNotes(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "tester@example.com")
	runGitIn(t, dir, "config", "user.name", "Tester")

	author := "tester@example.com"
	chunksDir := memoryChunksDir(dir)

	noteA := memory.ChunkRecord{
		SyncID: "sync-a", TopicKey: "decision/a", Title: "decision/a",
		Content: "body a", Type: "decision", Author: author, Revision: 1,
	}
	noteB := memory.ChunkRecord{
		SyncID: "sync-b", TopicKey: "gotcha/leaked", Title: "gotcha/leaked",
		Content: "body b", Type: "gotcha", Author: author, Revision: 1,
	}

	// Commit a chunk with BOTH notes at HEAD.
	if _, _, err := memory.WriteChunk(chunksDir, author, []memory.ChunkRecord{noteA, noteB}); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", ".tu-agent/share/memory/chunks")
	runGitIn(t, dir, "commit", "-m", "chunk: both notes")

	// Overwrite the WORKING chunk with only note A — note B silently dropped
	// (e.g. by the secret filter on a subsequent export).
	if _, _, err := memory.WriteChunk(chunksDir, author, []memory.ChunkRecord{noteA}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMemoryPending(&out, dir, author); err != nil {
		t.Fatalf("runMemoryPending: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "gotcha/leaked") {
		t.Fatalf("want removed note gotcha/leaked surfaced under a removal section, got %q", got)
	}
	if !strings.Contains(strings.ToUpper(got), "REMOVE") {
		t.Fatalf("want a clearly-labeled removal section, got %q", got)
	}
}

// TestSkillRecordExportImportRoundTrip verifies that a skill record (type
// "skill", content carrying the tu-agent:crystallize marker) survives an
// export→chunk→import cycle into a fresh store with its content intact.
func TestSkillRecordExportImportRoundTrip(t *testing.T) {
	chunks := t.TempDir()

	// Source store: create and export a skill record.
	srcDB := filepath.Join(t.TempDir(), "memory.db")
	src, err := memory.Open(srcDB)
	if err != nil {
		t.Fatal(err)
	}
	skillContent := crystallize.ProvenanceLine("checkout", nil) + "\n---\nname: checkout\n---\nSkill body.\n"
	if _, err := src.Upsert("skill/checkout", skillContent, memory.UpsertOpts{
		Author: "alice",
		Type:   "skill",
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := src.ExportRecords("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 exported record, got %d", len(recs))
	}
	if _, _, err := memory.WriteChunk(chunks, "alice", recs); err != nil {
		t.Fatal(err)
	}
	_ = src.Close()

	// Destination store: import from the chunk and verify the skill record.
	back, err := memory.ReadAllChunks(chunks)
	if err != nil {
		t.Fatal(err)
	}
	dstDB := filepath.Join(t.TempDir(), "memory.db")
	dst, err := memory.Open(dstDB)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	res, err := dst.ImportRecords(back)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 {
		t.Fatalf("want 1 inserted, got %+v", res)
	}
	got, _, err := dst.Search(crystallize.Marker, "skill", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("imported skill record not searchable: %+v", got)
	}
	if !strings.Contains(got[0].Content, crystallize.Marker) {
		t.Errorf("skill record content missing marker after round-trip; got %q", got[0].Content)
	}
	if got[0].TopicKey != "skill/checkout" {
		t.Errorf("skill record topic key = %q, want skill/checkout", got[0].TopicKey)
	}
}
