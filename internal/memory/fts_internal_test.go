package memory

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file covers Feature: Lazy, dirty-gated FTS rebuild
// (.tu-agent/tdd/fallos-silenciosos-y-re-layout-de-tu-age/features/fts-lazy-rebuild.feature).
//
// Design under test (not yet implemented — these tests are RED against
// today's code): the full observations_fts rebuild (DELETE + INSERT ...
// SELECT) leaves Open entirely and becomes lazy, running only on the search
// path, and only when a "dirty" flag is set. A writer without the FTS5
// module sets the flag; the next search rebuilds, then clears it. The
// rebuild transaction uses BEGIN IMMEDIATE so two concurrent rebuilders
// queue on busy_timeout instead of failing instantly with
// SQLITE_BUSY_SNAPSHOT. schema_version 2 -> 3 forces exactly one such
// rebuild to cure whatever an old binary left.
//
// Assumption (undocumented in the spec, chosen here as the test contract):
// the dirty flag is persisted in the metadata table under key "fts_dirty",
// value "true" when dirty, "" (absent) when clean — mirroring the existing
// "sync_ids_backfilled"/"types_backfilled" boolean-flag convention in
// store.go. The GREEN implementation must use this exact key/value pair, or
// these tests need updating together with it.

func TestOpen_FallbackWithoutFTS(t *testing.T) {
	ftsDisabled = true
	t.Cleanup(func() { ftsDisabled = false })
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.FTSEnabled() {
		t.Error("FTSEnabled = true, want false when the module probe is disabled")
	}
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _, err := s.Search("jwt", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("LIKE fallback results = %d, want 1", len(got))
	}
}

// TestSearchLiteralPercent guards the LIKE-escape fix: a query containing
// SQL wildcard characters (%, _) must match them literally, not as
// wildcards. Forced onto the substring fallback path (ftsDisabled) since
// FTS5 tokenizes rather than pattern-matches.
func TestSearchLiteralPercent(t *testing.T) {
	ftsDisabled = true
	t.Cleanup(func() { ftsDisabled = false })
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := s.Add("progress", "100% done", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add("progress", "100 x done", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, total, err := s.Search("100%", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("Search(%q) = %d results (total %d), want exactly 1 — an unescaped %% would also match \"100 x done\"", "100%", len(got), total)
	}
	if got[0].Content != "100% done" {
		t.Errorf("Search(%q) matched %q, want the literal \"100%% done\" note", "100%", got[0].Content)
	}
}

// TestOpen_WithoutSearchDoesNotRebuildStaleIndex is scenario @s1: opening
// (and closing) the store without ever searching must not touch
// observations_fts at all — not even to heal it. Formerly
// TestOpen_RebuildIndexesExistingRows, which pinned the OLD contract
// (reopening always fully rebuilds the index); rewritten here because that
// behavior is exactly what this feature removes.
//
// RED today: Open unconditionally calls initFTS -> setupFTS -> rebuildFTS,
// which deletes every row and reinserts from observations. That erases the
// tampered stale row this test plants, and the row-count assertion below
// (unchanged by the open) fails against current code.
func TestOpen_WithoutSearchDoesNotRebuildStaleIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !s.FTSEnabled() {
		s.Close()
		t.Skip("binary built without -tags sqlite_fts5")
	}
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Tamper: insert a row with no matching observation, simulating drift
	// the rebuild would normally erase.
	if _, err := s.db.Exec(`INSERT INTO observations_fts(id, title, content, topic_key)
		VALUES (?,?,?,?)`, "stale-id", "stale title", "stale content", "stale/key"); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	var before int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 2 {
		t.Fatalf("setup: observations_fts rows = %d, want 2 (1 real + 1 tampered)", before)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// When: the store is opened and closed without any search.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	var after int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != before {
		t.Errorf("observations_fts rows = %d after an open with no search, want %d (unchanged)", after, before)
	}
	var staleN int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, "stale-id").Scan(&staleN); err != nil {
		t.Fatalf("stale count: %v", err)
	}
	if staleN != 1 {
		t.Errorf("stale tampered row survived = %d instances, want 1 (an open without search must not rebuild the index)", staleN)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close after reopen: %v", err)
	}
}

// TestSearch_CleanStoreDoesNotRebuildTamperedGap is scenario @s2: a search
// against a store whose every write went through an FTS5-enabled writer
// (dirty flag unset) must not repair index drift introduced after the last
// write — searching is not an implicit "always rebuild" operation.
//
// Risk (see contract): this assertion also holds against TODAY's code, so
// it is not RED by itself — current Search never touches observations_fts
// under any condition (the rebuild only ever runs inside Open). It still
// guards the new implementation against a plausible wrong fix ("rebuild on
// every search").
func TestSearch_CleanStoreDoesNotRebuildTamperedGap(t *testing.T) {
	s := openTestStore(t)
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}
	obsA, err := s.Add("auth", "JWT tokens expire hourly", "test")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add("deploy", "the pipeline promotes builds to staging", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	dirty, err := s.meta("fts_dirty")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if dirty != "" {
		t.Fatalf("setup: fts_dirty = %q after clean FTS5-enabled writes, want unset", dirty)
	}

	// Tamper: drop one row from the index after the last write.
	if _, err := s.db.Exec(`DELETE FROM observations_fts WHERE id = ?`, obsA.ID); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	got, _, err := s.Search("pipeline", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Search(pipeline) = %d results, want 1", len(got))
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, obsA.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("tampered gap was repaired by a search on a clean store: index rows for id = %d, want 0", n)
	}
}

// TestSearch_CuresDriftFromNonFTS5Writer is scenario @s3: a note written by
// a binary without the FTS5 module (the ftsDisabled switch) leaves a gap in
// observations_fts. The next FTS5-enabled store to open the same database
// must NOT cure it merely by opening (that would repeat @s1's violation);
// it must cure it on the following search, and clear the dirty flag
// afterwards.
//
// RED today: current Open() always fully rebuilds unconditionally, so the
// pre-search assertion below (the index does not yet contain the drifted
// note immediately after Open, before any Search) fails against current
// code — today it's already cured by the time Open returns.
func TestSearch_CuresDriftFromNonFTS5Writer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")

	ftsDisabled = true
	s1, err := Open(path)
	if err != nil {
		ftsDisabled = false
		t.Fatalf("Open (disabled): %v", err)
	}
	if s1.FTSEnabled() {
		s1.Close()
		ftsDisabled = false
		t.Fatal("setup: FTSEnabled = true with ftsDisabled set, want false")
	}
	obs, err := s1.Add("auth", "JWT tokens expire hourly", "test")
	if err != nil {
		s1.Close()
		ftsDisabled = false
		t.Fatalf("Add: %v", err)
	}
	if err := s1.Close(); err != nil {
		ftsDisabled = false
		t.Fatalf("Close: %v", err)
	}
	ftsDisabled = false

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}

	// Pre-search: the drift must still be present — proves the cure is lazy,
	// not performed eagerly by Open.
	var preN int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, obs.ID).Scan(&preN); err != nil {
		t.Fatalf("pre-search count: %v", err)
	}
	if preN != 0 {
		t.Errorf("observations_fts already contains the drifted note right after Open (n=%d), want 0 — the cure must wait for a search", preN)
	}

	got, _, err := s2.Search("jwt", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search after a non-FTS5 write returned %d results, want 1 (drift not cured by the search)", len(got))
	}

	dirty, err := s2.meta("fts_dirty")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if dirty != "" {
		t.Errorf("fts_dirty = %q after the curing search, want cleared", dirty)
	}
}

// TestSearch_AfterCureDoesNotRebuildAgain is scenario @s4: once a search has
// cured drift (clearing the dirty flag), a following search must not
// rebuild again — a fresh tamper made after the cure must survive a second
// search untouched.
//
// Risk (see contract): like @s2, this is not RED against today's code —
// current Search never rebuilds under any condition, so the "not repaired"
// assertion holds trivially either way. Kept as a regression guard against
// an implementation that rebuilds unconditionally on every search.
func TestSearch_AfterCureDoesNotRebuildAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")

	ftsDisabled = true
	s1, err := Open(path)
	if err != nil {
		ftsDisabled = false
		t.Fatalf("Open (disabled): %v", err)
	}
	obsA, err := s1.Add("auth", "JWT tokens expire hourly", "test")
	if err != nil {
		s1.Close()
		ftsDisabled = false
		t.Fatalf("Add: %v", err)
	}
	if err := s1.Close(); err != nil {
		ftsDisabled = false
		t.Fatalf("Close: %v", err)
	}
	ftsDisabled = false

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}

	// Cure the drift left by the non-FTS5 writer.
	if _, _, err := s2.Search("jwt", "", 0); err != nil {
		t.Fatalf("curing Search: %v", err)
	}
	dirty, err := s2.meta("fts_dirty")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if dirty != "" {
		t.Fatalf("setup: fts_dirty = %q after the curing search, want cleared", dirty)
	}

	// Tamper again, now on the cured/clean store.
	if _, err := s2.db.Exec(`DELETE FROM observations_fts WHERE id = ?`, obsA.ID); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if _, _, err := s2.Search("tokens", "", 0); err != nil {
		t.Fatalf("second Search: %v", err)
	}

	var n int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, obsA.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("second search repaired the tampered gap (n=%d), want 0 — a clean store must not rebuild on every search", n)
	}
}

// TestSearch_ConcurrentSearchesOnDirtyStoreSucceed is scenario @s5: two
// goroutines that each open the database and search while the store is
// marked dirty must both succeed — neither may see "database is locked",
// and both must return every match.
//
// RED today: rebuildFTS runs unconditionally inside Open using a DEFERRED
// transaction (s.db.Begin()). Two overlapping opens race to upgrade the
// same read snapshot to a writer at the DELETE, and in WAL mode the loser
// gets SQLITE_BUSY_SNAPSHOT immediately — _busy_timeout never engages
// because that error bypasses the busy handler. This reproduces the P1 bug
// from the design doc (5/5 rounds observed).
func TestSearch_ConcurrentSearchesOnDirtyStoreSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	seed, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !seed.FTSEnabled() {
		seed.Close()
		t.Skip("binary built without -tags sqlite_fts5")
	}
	const n = 50
	for i := 0; i < n; i++ {
		if _, err := seed.Add(fmt.Sprintf("topic-%d", i), "gizmoflux payload for the concurrency race", "test"); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if err := seed.setMeta("fts_dirty", "true"); err != nil {
		t.Fatalf("setMeta: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	const rounds = 5
	for r := 0; r < rounds; r++ {
		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, openErr := Open(path)
				if openErr != nil {
					errCh <- fmt.Errorf("open: %w", openErr)
					return
				}
				defer s.Close()
				got, _, searchErr := s.Search("gizmoflux", "", 0)
				if searchErr != nil {
					errCh <- fmt.Errorf("search: %w", searchErr)
					return
				}
				if len(got) != n {
					errCh <- fmt.Errorf("got %d matches, want %d", len(got), n)
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for opErr := range errCh {
			if strings.Contains(opErr.Error(), "database is locked") {
				t.Errorf("round %d: concurrent search hit \"database is locked\": %v", r, opErr)
			} else {
				t.Errorf("round %d: %v", r, opErr)
			}
		}

		// Re-mark the store dirty so the next round exercises the same
		// concurrent-rebuild race instead of degenerating into a no-op
		// clean search.
		s, err := Open(path)
		if err != nil {
			t.Fatalf("reopen to remark dirty: %v", err)
		}
		if err := s.setMeta("fts_dirty", "true"); err != nil {
			t.Fatalf("setMeta: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

// TestOpen_SchemaVersion2MigratesToThreeWithSingleRebuild is scenario @s6.
// Formerly TestOpen_FTSBumpsSchemaVersion, which pinned the OLD contract
// (every open sets schema_version to the fixed string "2"); rewritten here
// because "2" is no longer the current version once the dirty-gated design
// lands. A database left at schema_version "2" by an old binary, with drift
// in its FTS index, must be migrated to "3" by a single rebuild triggered
// through Open + one search.
//
// RED today: current code has no notion of schema_version "3" anywhere —
// setupFTS always (re)writes "2" — so the final schema_version assertion
// fails against current code regardless of the search result.
func TestOpen_SchemaVersion2MigratesToThreeWithSingleRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !s.FTSEnabled() {
		s.Close()
		t.Skip("binary built without -tags sqlite_fts5")
	}
	obsA, err := s.Add("auth", "JWT tokens expire hourly", "test")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add("deploy", "the pipeline promotes builds to staging", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Simulate drift left by an old binary: drop one row from the index...
	if _, err := s.db.Exec(`DELETE FROM observations_fts WHERE id = ?`, obsA.ID); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	// ...and roll the stored schema_version back to "2".
	if err := s.setMeta("schema_version", "2"); err != nil {
		t.Fatalf("setMeta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	got, _, err := s2.Search("jwt", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Search after a schema_version 2->3 migration returned %d results, want 1 (drifted note not cured)", len(got))
	}

	v, err := s2.meta("schema_version")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if v != "3" {
		t.Errorf("schema_version = %q after migration, want %q", v, "3")
	}
}

// TestOpen_FourConcurrentNonSearchOpensNeverFail is scenario @s7: it mirrors
// the real SessionStart shape — plugin/hooks/hooks.json fires memory
// import, memory relink, memory materialize, and advise --nudge together,
// and none of them ever call Search. Four goroutines open the same
// database, do non-search work, and close, for 5 rounds (20 operations
// total); every operation must succeed.
//
// RED today: every Open unconditionally rebuilds the FTS index inside a
// DEFERRED transaction, so these four concurrent opens race for the same
// upgrade and intermittently fail with "database is locked" — this is the
// exact P1 bug, reproduced 5/5 rounds against the real hook shape.
func TestOpen_FourConcurrentNonSearchOpensNeverFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	seed, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := seed.Add("seed", "existing note for the session-start race", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// One worker per real SessionStart hook, none of them calling Search:
	// memory import (write), memory relink (read), memory materialize
	// (read), advise --nudge (read).
	workers := []func(*Store) error{
		func(s *Store) error {
			_, addErr := s.Add("import", "note from the import hook", "test")
			return addErr
		},
		func(s *Store) error { _, listErr := s.List(); return listErr },
		func(s *Store) error { _, recentErr := s.Recent(5); return recentErr },
		func(s *Store) error { _, lenErr := s.Len(); return lenErr },
	}

	const rounds = 5
	var failures int32
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		for _, work := range workers {
			wg.Add(1)
			go func(work func(*Store) error) {
				defer wg.Done()
				s, openErr := Open(path)
				if openErr != nil {
					atomic.AddInt32(&failures, 1)
					t.Errorf("round %d: Open: %v", r, openErr)
					return
				}
				defer s.Close()
				if workErr := work(s); workErr != nil {
					atomic.AddInt32(&failures, 1)
					t.Errorf("round %d: work: %v", r, workErr)
				}
			}(work)
		}
		wg.Wait()
	}
	if failures != 0 {
		t.Fatalf("%d of %d SessionStart-shaped operations failed, want 0", failures, rounds*len(workers))
	}
}

// openFTSStoreInternal opens a fresh store and skips unless FTS is active.
func openFTSStoreInternal(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}
	return s
}

func TestAdd_IndexesIntoFTS(t *testing.T) {
	s := openFTSStoreInternal(t)
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("observations_fts rows = %d, want 1 (indexed without reopen)", n)
	}
}

func TestUpsert_ReindexesFTS(t *testing.T) {
	s := openFTSStoreInternal(t)
	obs, err := s.Upsert("arch/queue", "messages flow through the broker", UpsertOpts{})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if _, err := s.Upsert("arch/queue", "events flow through the dispatcher", UpsertOpts{}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE id = ?`, obs.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("index rows for id = %d, want 1 (delete + reinsert, no duplicates)", n)
	}
	var content string
	if err := s.db.QueryRow(`SELECT content FROM observations_fts WHERE id = ?`, obs.ID).Scan(&content); err != nil {
		t.Fatalf("select content: %v", err)
	}
	if content != "events flow through the dispatcher" {
		t.Errorf("indexed content = %q, want the revised content", content)
	}
	var stale int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observations_fts WHERE content = 'messages flow through the broker'`).Scan(&stale); err != nil {
		t.Fatalf("stale count: %v", err)
	}
	if stale != 0 {
		t.Errorf("old content still in index: want 0 rows, got %d", stale)
	}
}

// TestRebuildFTS_ClearsDirtyFlagInsideItsOwnTransaction is the JUDGE-FOUND
// REGRESSION from the design review of fts-lazy-rebuild — it has no @s tag
// because the feature file's scenarios (@s1..@s7) are frozen; this pins a
// defect the interrogation missed.
//
// The defect: maybeRebuildFTS (fts.go:101-116) reads the "fts_dirty" meta
// flag, calls rebuildFTS() (whose transaction COMMITS), and only THEN clears
// the flag with a SEPARATE, later, autocommit write:
// s.setMeta("fts_dirty", ""). That leaves a window between rebuildFTS's
// COMMIT and the clear. A writer with no FTS5 module (the ftsDisabled
// switch) that commits its observation plus its own markDirty call inside
// that window gets its mark ERASED by the clear that follows — silently: no
// error, no log, and the next search will not cure it either, because the
// flag already reads clean. That defeats the whole point of this feature,
// which exists to stop drift from being silent.
//
// The fix (per the design review): move the clear INSIDE rebuildFTS's own
// transaction — a clearDirty(execer) helper mirroring the existing
// markDirty(execer) — so it commits atomically with the DELETE+INSERT.
// Because the store's DSN sets _txlock=immediate (store.go ~125), that
// transaction holds the write lock for its entire BEGIN-to-COMMIT span, so a
// concurrent writer's commit can no longer land "between" the rebuild and
// the clear once they are the same commit — the window closes entirely.
//
// Why this test drives rebuildFTS() directly instead of racing goroutines:
// the actual bug window sits between two separate, unlocked, single-writer
// SQL statements inside ONE call to maybeRebuildFTS. Nothing holds the write
// lock across that gap (that is exactly the bug), so a second goroutine
// racing to land inside it has no synchronization primitive to pin the
// interleaving to — it would land before, inside, or after the gap
// depending on OS scheduling, making the test flaky and worthless as a
// regression guard. Calling maybeRebuildFTS/rebuildFTS a second time (rather
// than racing) does not reproduce the bug either: any full call performs a
// fresh, correct, whole-table rebuild from current state and would silently
// paper over the very loss under test. The only deterministic way to pin
// this is to assert the INVARIANT the fix establishes: that rebuildFTS
// leaves the flag clean by itself, atomic with its own commit, with no
// second write left for a caller (or a race) to perform later. That is
// exactly the mechanism that closes the window, and it requires no
// goroutines to observe.
//
// RED today: rebuildFTS (fts.go:122-139) never touches the "fts_dirty" meta
// key at all — only the separate call in maybeRebuildFTS does — so the
// assertion below fails against current code. Confirmed failing with:
//
//	go test -race -tags sqlite_fts5 ./internal/memory/ -run TestRebuildFTS_ClearsDirtyFlagInsideItsOwnTransaction
//	--- FAIL: TestRebuildFTS_ClearsDirtyFlagInsideItsOwnTransaction (0.00s)
//	    fts_internal_test.go:...: fts_dirty = "true" immediately after rebuildFTS returns, want "" (cleared) —
//	    the clear must be rebuildFTS's own responsibility, atomic with its DELETE+INSERT transaction, not a
//	    separate later write performed by its caller (maybeRebuildFTS); a separate write leaves a gap where a
//	    concurrent non-FTS5 writer's own dirty mark can be silently erased
func TestRebuildFTS_ClearsDirtyFlagInsideItsOwnTransaction(t *testing.T) {
	s := openFTSStoreInternal(t)
	if _, err := s.Add("auth", "JWT tokens expire hourly", "test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Simulate the trigger condition maybeRebuildFTS reacts to: a prior
	// writer (or the schema_version 2->3 migration) marked the store dirty.
	if err := s.setMeta("fts_dirty", "true"); err != nil {
		t.Fatalf("setMeta: %v", err)
	}

	if err := s.rebuildFTS(); err != nil {
		t.Fatalf("rebuildFTS: %v", err)
	}

	dirty, err := s.meta("fts_dirty")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if dirty != "" {
		t.Errorf(`fts_dirty = %q immediately after rebuildFTS returns, want "" (cleared) — `+
			"the clear must be rebuildFTS's own responsibility, atomic with its DELETE+INSERT "+
			"transaction, not a separate later write performed by its caller (maybeRebuildFTS); "+
			"a separate write leaves a gap where a concurrent non-FTS5 writer's own dirty mark "+
			"can be silently erased", dirty)
	}
}

// TestSearch_ProbeDetectsPreBranchNonFTS5CountDrift pins FINDING 1 from the
// whole-branch review of fts-lazy-rebuild (fts.go:74): setupFTS early-returns
// as soon as it sees schema_version == "3", which means it never re-checks
// for drift on any later open of an already-migrated store.
//
// Reproduction (matches the reviewer's repro exactly):
//  1. A current binary (with the FTS5 module) opens a brand-new store ->
//     setupFTS migrates it to schema_version "3", index empty and in sync
//     (no live rows yet, so nothing is flagged dirty).
//  2. A binary built BEFORE this branch, without the FTS5 module, writes a
//     note. Its ftsInsert was a bare `return nil` (markDirty did not exist
//     yet) and its initFTS returns before ever touching schema_version, so
//     the row lands in `observations` with NO matching row in
//     `observations_fts` and NO "fts_dirty" flag set. We cannot call
//     today's write path (Add/Upsert) to simulate this — it would call
//     markDirty and defeat the scenario — so this test inserts the row
//     directly via SQL against `observations`, exactly as the task brief
//     directs, leaving `observations_fts` and the meta flag untouched.
//  3. A current binary opens the store three consecutive times (the
//     reviewer's repro number). setupFTS sees schema_version == "3" every
//     time and returns immediately (fts.go:74-76), so none of the three
//     opens ever notices the gap; the dirty flag stays unset throughout.
//
// THE AGREED FIX (see task brief — pin this, not an alternative design): a
// cheap drift probe on the SEARCH path, not in Open (Open must stay
// write-free for a non-searching caller — see @s1/@s7 above). Before
// deciding whether to rebuild, compare `count(*) FROM observations WHERE
// deleted_at IS NULL` against `count(*) FROM observations_fts`; a mismatch
// means the store is dirty and must be rebuilt before answering the search.
//
// RED today: maybeRebuildFTS only ever consults the "fts_dirty" meta flag
// (fts.go:104-114), which this scenario never sets, so Search returns 0 hits
// for a note that plainly exists in `observations`. Confirmed failing with:
//
//	go test -race -tags sqlite_fts5 ./internal/memory/ -run TestSearch_ProbeDetectsPreBranchNonFTS5CountDrift
//	--- FAIL: TestSearch_ProbeDetectsPreBranchNonFTS5CountDrift (0.00s)
//	    fts_internal_test.go:...: Search(jwt) after a pre-branch non-FTS5 write = 0 results, want 1 —
//	    a count-drift probe on the search path must detect the observations/observations_fts mismatch
//	    and rebuild
func TestSearch_ProbeDetectsPreBranchNonFTS5CountDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")

	// Step 1: a current binary opens a brand-new store -> migrated to v3,
	// index empty and in sync (no live rows yet).
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !s1.FTSEnabled() {
		s1.Close()
		t.Skip("binary built without -tags sqlite_fts5")
	}
	v, err := s1.meta("schema_version")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if v != "3" {
		t.Fatalf("setup: schema_version = %q after first open, want %q", v, "3")
	}

	// Step 2: simulate the pre-branch, non-FTS5 writer by inserting directly
	// into `observations`, bypassing insert()/ftsInsert() (and so bypassing
	// markDirty) entirely.
	now := time.Now().UTC().Format(timeFormat)
	if _, err := s1.db.Exec(insertObsSQL,
		"pre-branch-id", "auth", "project", "", "auth", "JWT tokens expire hourly",
		"", "legacy-writer", 1, now, now, "", "obs-pre-branch", false); err != nil {
		t.Fatalf("raw insert (simulating a pre-branch non-FTS5 writer): %v", err)
	}
	var ftsRows int
	if err := s1.db.QueryRow(`SELECT COUNT(*) FROM observations_fts`).Scan(&ftsRows); err != nil {
		t.Fatalf("count observations_fts: %v", err)
	}
	if ftsRows != 0 {
		t.Fatalf("setup: observations_fts has %d rows right after the raw insert, want 0 (untouched)", ftsRows)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close s1: %v", err)
	}

	// Step 3: three consecutive opens by a current binary. None may cure the
	// gap merely by opening — that would repeat @s1's contract violation —
	// so the dirty flag must stay unset through all three.
	var s3 *Store
	for i := 1; i <= 3; i++ {
		s, openErr := Open(path)
		if openErr != nil {
			t.Fatalf("open #%d: %v", i, openErr)
		}
		dirty, metaErr := s.meta("fts_dirty")
		if metaErr != nil {
			t.Fatalf("meta after open #%d: %v", i, metaErr)
		}
		if dirty != "" {
			s.Close()
			t.Fatalf("fts_dirty = %q after open #%d, want unset (opening alone must not flag drift)", dirty, i)
		}
		if i < 3 {
			if closeErr := s.Close(); closeErr != nil {
				t.Fatalf("close open #%d: %v", i, closeErr)
			}
			continue
		}
		s3 = s
	}
	defer s3.Close()

	v, err = s3.meta("schema_version")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if v != "3" {
		t.Fatalf("setup: schema_version = %q before search, want %q", v, "3")
	}

	got, _, err := s3.Search("jwt", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Search(jwt) after a pre-branch non-FTS5 write = %d results, want 1 — a count-drift probe on the search path must detect the observations/observations_fts mismatch and rebuild", len(got))
	}
}

// TestSetupFTS_FlagsDirtyWhenIndexTableExistsButEmpty pins FINDING 2 from the
// whole-branch review of fts-lazy-rebuild (fts.go:78): the `existed == 0`
// check treats "the observations_fts table already exists" as proof the
// index is in sync with `observations`. That is false for a store at
// schema_version "1" whose observations_fts table EXISTS but is EMPTY — the
// exact shape an OLDER setupFTS leaves behind when it commits its `CREATE
// VIRTUAL TABLE` and then aborts before it can count and flag live rows
// (e.g. on the "database is locked" error this whole feature exists to fix).
//
// Reproduction: seed one live observation by inserting directly via SQL
// against `observations` (bypassing insert()/ftsInsert(), so no markDirty
// call happens and observations_fts is never created — leaving
// schema_version at Open's fresh-database default of "1"), then create
// observations_fts directly via a raw connection so it exists but stays
// empty — exactly the state an aborted older setupFTS would leave. Note this
// must NOT go through Store.Add with ftsDisabled: that already calls
// ftsInsert -> markDirty (this branch's own, already-correct, mechanism),
// which would flag dirty for the wrong reason and mask the bug under test. A
// current, FTS5-enabled binary then opens the store: setupFTS sees
// existed != 0, so the `existed == 0` branch that would otherwise count live
// rows and flag dirty (fts.go:78-84) never runs, and schema_version is
// bumped straight to "3" without ever flagging dirty. Every later search
// then returns nothing, forever — the same terminal silent-drift failure as
// FINDING 1.
//
// THE FIX (per the task brief): setupFTS must also flag dirty when the index
// table exists but is empty while `observations` holds live rows.
//
// RED today: fts_dirty reads "" immediately after Open, because the
// existed != 0 branch never runs the count(*) check that would set it.
// Confirmed failing with:
//
//	go test -race -tags sqlite_fts5 ./internal/memory/ -run TestSetupFTS_FlagsDirtyWhenIndexTableExistsButEmpty
//	--- FAIL: TestSetupFTS_FlagsDirtyWhenIndexTableExistsButEmpty (0.00s)
//	    fts_internal_test.go:...: fts_dirty = "" after opening a store whose observations_fts table
//	    existed but was empty while observations held a live row, want "true" — setupFTS must not
//	    assume an existing table means an in-sync index
func TestSetupFTS_FlagsDirtyWhenIndexTableExistsButEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")

	// Seed one live observation with the FTS5 module disabled and by writing
	// directly into `observations` via SQL — NOT via Store.Add, which would
	// route through ftsInsert -> markDirty and flag dirty for the wrong
	// reason (see comment above). This leaves schema_version at Open's
	// fresh-database default "1" and never creates observations_fts, because
	// initFTS returns before touching either.
	ftsDisabled = true
	seed, err := Open(path)
	if err != nil {
		ftsDisabled = false
		t.Fatalf("Open (seed, disabled): %v", err)
	}
	now := time.Now().UTC().Format(timeFormat)
	if _, err := seed.db.Exec(insertObsSQL,
		"seed-id", "auth", "project", "", "auth", "JWT tokens expire hourly",
		"", "test", 1, now, now, "", "obs-seed", false); err != nil {
		seed.Close()
		ftsDisabled = false
		t.Fatalf("raw insert: %v", err)
	}
	seedDirty, err := seed.meta("fts_dirty")
	if err != nil {
		seed.Close()
		ftsDisabled = false
		t.Fatalf("meta: %v", err)
	}
	if seedDirty != "" {
		seed.Close()
		ftsDisabled = false
		t.Fatalf("setup: fts_dirty = %q right after the raw seed insert, want unset (the raw insert must not go through markDirty)", seedDirty)
	}
	if err := seed.Close(); err != nil {
		ftsDisabled = false
		t.Fatalf("close seed: %v", err)
	}
	ftsDisabled = false

	// Reproduce the abort mode directly, via a raw connection, rather than
	// through the code under test: an older setupFTS committed its CREATE
	// VIRTUAL TABLE, then aborted before it could count live rows or migrate
	// schema_version.
	raw, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`CREATE VIRTUAL TABLE observations_fts USING fts5(id UNINDEXED, title, content, topic_key)`); err != nil {
		raw.Close()
		// This raw CREATE is the one FTS5 call in this file that runs before any
		// Store exists to ask FTSEnabled(), so it is also the only place the
		// module can be missing without a skip already having fired. A binary
		// built without -tags sqlite_fts5 has no fts5 module and cannot reach the
		// state under test at all — skip rather than fail, matching every other
		// FTS test here. Failing would break the degraded build, which is exactly
		// the configuration this whole feature exists to keep working.
		if strings.Contains(err.Error(), "no such module: fts5") {
			t.Skip("binary built without -tags sqlite_fts5")
		}
		t.Fatalf("raw create observations_fts: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if !s.FTSEnabled() {
		t.Skip("binary built without -tags sqlite_fts5")
	}

	dirty, err := s.meta("fts_dirty")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if dirty != "true" {
		t.Errorf(`fts_dirty = %q after opening a store whose observations_fts table existed but was `+
			`empty while observations held a live row, want "true" — setupFTS must not assume an `+
			"existing table means an in-sync index", dirty)
	}

	got, _, err := s.Search("jwt", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Search(jwt) = %d results, want 1 once the dirty flag correctly triggers a rebuild", len(got))
	}
}
