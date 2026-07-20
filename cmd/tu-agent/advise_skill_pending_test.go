package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// seedForeignUnapprovedSkill upserts one type=skill record authored by a
// different git identity than the current repo's configured user.email, with
// no corresponding skill_approvals entry — the "one unapproved foreign skill
// record" fixture @s2 and @s3 both start from.
func seedForeignUnapprovedSkill(t *testing.T, root string) {
	t.Helper()
	ms, err := memory.Open(memoryDBPath(root))
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	if _, err := ms.Upsert("skill/deploy", "deploy skill body", memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestAdvisePlain_SkillPending_MentionsPendingCommands (@s2) verifies that
// with one unapproved foreign skill record sitting in the store, `tu-agent
// advise` mentions both `tu-agent memory pending` and `tu-agent memory
// approve-skill` — the review/approve path a human needs to run before that
// teammate's skill can ever reach disk. Red today: internal/advise.Evaluate
// has no rule reading a pending-skill count — the "skill-pending" rule and
// the Inputs field it needs (design.md) do not exist yet — so plain advise's
// output, built only from telemetry/crystallize/uncovered-file inputs, never
// contains either string no matter what is sitting in the memory store.
func TestAdvisePlain_SkillPending_MentionsPendingCommands(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	seedForeignUnapprovedSkill(t, dir)

	var out bytes.Buffer
	adviseCmd.SetOut(&out)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	got := out.String()
	for _, want := range []string{"tu-agent memory pending", "tu-agent memory approve-skill"} {
		if !strings.Contains(got, want) {
			t.Errorf("advise output missing %q with one unapproved foreign skill pending, got: %q", want, got)
		}
	}
}

// TestAdviseDismiss_SkillPending_SuppressesFollowingRun (@s3) verifies the
// dismiss lifecycle for the not-yet-added "skill-pending" rule end to end:
// `tu-agent advise dismiss skill-pending` must succeed, and a following
// `advise` run against the same pending record must then print no
// skill-pending suggestion. Red today on the FIRST assertion: "skill-pending"
// is absent from knownAdviseRules, so runAdviseDismiss returns an "unknown
// rule" error before the persisted-state and second-run assertions below are
// ever reached.
func TestAdviseDismiss_SkillPending_SuppressesFollowingRun(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	seedForeignUnapprovedSkill(t, dir)

	if err := runAdviseDismiss(adviseCmd, "skill-pending"); err != nil {
		t.Fatalf("advise dismiss skill-pending: want it to succeed once the rule is known, got: %v", err)
	}

	var out bytes.Buffer
	adviseCmd.SetOut(&out)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	if strings.Contains(out.String(), "memory approve-skill") {
		t.Errorf("dismissed skill-pending rule must not print its suggestion on a following run, got: %q", out.String())
	}
}

// TestApprovalLocalOnly_ExportedChunkCarriesNoApprovalState (@s1) verifies
// that approving a foreign skill record never leaks into the exported team
// chunk. The approval blob lives in the memory store's metadata table (the
// skillApprovalsKey "skill_approvals"), which Store.ExportRecords never
// reads — it queries only the observations table — and the approved record
// itself is authored by someone other than the exporter, so ExportRecords'
// ownership filter (author = exporter OR author = "") excludes the record
// too. Both are structural properties of the current code, so this scenario
// is a regression pin (green today, not red): it locks the "no approval
// state travels in the chunk" guarantee design.md relies on, so a future
// change to ExportRecords' WHERE clause or to ChunkRecord's fields cannot
// silently start leaking it.
func TestApprovalLocalOnly_ExportedChunkCarriesNoApprovalState(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")
	runGitIn(t, dir, "config", "user.email", "carlos@example.com")
	runGitIn(t, dir, "config", "user.name", "Carlos")
	t.Chdir(dir)

	ms, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		t.Fatal(err)
	}
	obs, err := ms.Upsert("skill/deploy", "deploy skill body", memory.UpsertOpts{
		Type:   "skill",
		Author: "mallory@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Approve it — the design.md-fixed shape: skill_approvals maps name ->
	// approved revision. This is the state that must never reach the chunk.
	if err := ms.SetMeta(skillApprovalsKey, fmt.Sprintf(`{"deploy":%d}`, obs.Revision)); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	t.Cleanup(func() { memoryExportCmd.SetOut(nil) })
	if err := runMemoryExport(memoryExportCmd); err != nil {
		t.Fatalf("memory export: %v", err)
	}

	chunkPath := memory.ChunkPath(memoryChunksDir(repoRoot()), "carlos@example.com")
	recs, err := memory.ReadChunkFile(chunkPath)
	if err != nil {
		t.Fatalf("ReadChunkFile: %v", err)
	}
	for _, r := range recs {
		if r.TopicKey == "skill/deploy" {
			t.Errorf("exported chunk must not carry the foreign skill record %q, got %+v", "skill/deploy", r)
		}
	}

	// Decompress and scan the raw bytes too, guarding against approval state
	// ever leaking as a stray field rather than a whole record.
	f, err := os.Open(chunkPath)
	if err != nil {
		t.Fatalf("open chunk file: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	raw, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	rawStr := string(raw)
	for _, forbidden := range []string{"skill_approvals", "mallory@example.com", "deploy"} {
		if strings.Contains(rawStr, forbidden) {
			t.Errorf("exported chunk bytes must not contain %q, got: %s", forbidden, rawStr)
		}
	}
}
