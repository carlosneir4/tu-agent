package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

var memoryExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Write your authored observations to a committed chunk file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		start := time.Now()
		err := runMemoryExport(cmd)
		if memExportQuiet {
			recordHook("memory export", time.Since(start), err)
		}
		return err
	},
}

func runMemoryExport(cmd *cobra.Command) error {
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		author := gitAuthor()
		recs, err := s.ExportRecords(author)
		if err != nil {
			return err
		}
		var excluded []memory.ChunkRecord
		recs, excluded = filterSecretRecords(recs)
		if len(excluded) > 0 {
			titles := make([]string, 0, len(excluded))
			for _, r := range excluded {
				titles = append(titles, r.Title)
			}
			// Printed to stderr unconditionally, even under --quiet: silently
			// dropping data the user asked to export is worse than a hook
			// printing a line.
			fmt.Fprintf(cmd.ErrOrStderr(), "tu-agent: excluded %d note(s) with apparent secrets from the export — %s\n",
				len(excluded), strings.Join(titles, ", "))
		}
		// Only demand a configured author when there is something to write —
		// an empty export must never fail a no-op run.
		if len(recs) > 0 {
			author, err = requireAuthor(author)
			if err != nil {
				return err
			}
		}
		chunksDir := memoryChunksDir(repoRoot())
		oldRecs, err := memory.ReadChunkFile(memory.ChunkPath(chunksDir, author))
		if err != nil {
			return fmt.Errorf("memory export: read existing chunk: %w", err)
		}
		chunkPath, written, err := memory.WriteChunk(chunksDir, author, recs)
		if err != nil {
			return err
		}
		if written {
			changed, _ := diffChunkRecords(recs, oldRecs)
			if len(changed) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "tu-agent: %d new/updated team notes exported — review with 'tu-agent memory pending'\n", len(changed))
			}
		}
		if memExportQuiet {
			return nil
		}
		if !written {
			fmt.Fprintf(cmd.OutOrStdout(), "no changes; %s is up to date (%d observations)\n", chunkPath, len(recs))
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d observations) — commit it to share\n", chunkPath, len(recs))
		return nil
	})
}

// memoryPendingCmd is the human pre-commit review surface for the team memory
// chunk: it diffs the working-tree chunk file (what `memory export` just
// wrote) against the version already committed at git HEAD, and lists the
// notes that would newly land on the team the next time this chunk is
// committed. Deliberately not exposed as an MCP tool — this gate is for a
// human reviewing before `git commit`, not for an agent.
var memoryPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "Show team notes exported but not yet committed (human pre-commit review; no MCP tool by design)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		author, err := requireAuthor(gitAuthor())
		if err != nil {
			return err
		}
		return runMemoryPending(cmd.OutOrStdout(), repoRoot(), author)
	},
}

// errChunkAbsentAtHead is a sentinel for headChunkRecords: it means git ran
// successfully and reported the chunk simply is not present at HEAD (not a
// repo, unborn HEAD, or the path does not exist there) — the normal, benign
// "not committed yet" state. Any OTHER error (git not installed, or a
// committed chunk that fails to parse) is a real problem the caller must
// surface, not swallow.
var errChunkAbsentAtHead = errors.New("chunk not present at HEAD")

// runMemoryPending is the testable body of memoryPendingCmd: it diffs the
// working-tree chunk file (what `memory export` just wrote) against the
// version already committed at git HEAD, and lists the notes that would
// newly land on the team the next time this chunk is committed.
func runMemoryPending(out io.Writer, root, author string) error {
	chunkPath := memory.ChunkPath(memoryChunksDir(root), author)
	working, err := memory.ReadChunkFile(chunkPath)
	if err != nil {
		return fmt.Errorf("memory pending: read working chunk: %w", err)
	}
	headRecs, err := headChunkRecords(root, memory.RelChunkPath(author))
	if err != nil {
		if errors.Is(err, errChunkAbsentAtHead) {
			fmt.Fprintf(out, "chunk not committed yet — showing all %d notes\n", len(working))
			printPendingRecords(out, working, nil)
			return nil
		}
		return fmt.Errorf("memory pending: %w", err)
	}
	pending, edited := diffChunkRecords(working, headRecs)
	removed := removedChunkRecords(working, headRecs)
	if len(pending) == 0 && len(removed) == 0 {
		fmt.Fprintln(out, "nothing pending — team chunk is committed")
		return nil
	}
	if len(pending) > 0 {
		printPendingRecords(out, pending, edited)
	}
	if len(removed) > 0 {
		fmt.Fprintf(out, "\nwould REMOVE from the team chunk (%d) — these are committed but absent from your working chunk:\n", len(removed))
		printPendingRecords(out, removed, nil)
	}
	return nil
}

// removedChunkRecords returns records present in old (git HEAD) but absent from
// cur (the working chunk), by SyncID — notes that a commit would DROP from the
// shared team chunk (e.g. excluded by the secret filter). Surfacing these keeps
// `memory pending` from hiding silent deletions.
func removedChunkRecords(cur, old []memory.ChunkRecord) []memory.ChunkRecord {
	curSyncIDs := make(map[string]bool, len(cur))
	for _, r := range cur {
		curSyncIDs[r.SyncID] = true
	}
	var removed []memory.ChunkRecord
	for _, r := range old {
		if !curSyncIDs[r.SyncID] {
			removed = append(removed, r)
		}
	}
	return removed
}

// diffChunkRecords compares cur (the just-exported or working-tree records)
// against old (the chunk's prior contents — either the previous export or
// git HEAD's version), diffing by identity (SyncID) AND content (Revision):
// a record's SyncID is content-independent, so an edited note keeps its
// SyncID and only bumps Revision — a presence-only diff would miss it.
// Returns, in cur's order, every record that is new (SyncID absent from old)
// or edited (SyncID present but with a different Revision), plus a set
// naming which of those are edits (as opposed to brand new) so callers can
// annotate them.
func diffChunkRecords(cur, old []memory.ChunkRecord) (changed []memory.ChunkRecord, editedSyncIDs map[string]bool) {
	oldRevision := make(map[string]int, len(old))
	for _, r := range old {
		oldRevision[r.SyncID] = r.Revision
	}
	editedSyncIDs = make(map[string]bool)
	for _, r := range cur {
		rev, found := oldRevision[r.SyncID]
		switch {
		case !found:
			changed = append(changed, r)
		case rev != r.Revision:
			changed = append(changed, r)
			editedSyncIDs[r.SyncID] = true
		}
	}
	return changed, editedSyncIDs
}

// headChunkRecords returns the chunk records stored in root's git HEAD at
// relPath (a repo-relative, forward-slash path). It classifies the failure
// modes rather than treating every error as "not committed yet":
//   - git ran and refused (not a repo, unborn HEAD, or relPath absent at
//     HEAD) — an *exec.ExitError — wraps errChunkAbsentAtHead, the normal
//     "not committed yet" state.
//   - git itself could not run (binary missing, etc.) — NOT an
//     *exec.ExitError — returns the real error; the caller must not swallow
//     it as "not committed yet".
//   - git succeeded but the committed chunk fails to parse — a committed
//     chunk is corrupt and the user MUST see this, not a benign absence.
func headChunkRecords(root, relPath string) ([]memory.ChunkRecord, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "-C", root, "show", "HEAD:"+relPath)
	cmd.Stderr = &stderr
	blob, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Accepted tradeoff: ANY non-zero git exit here — including a genuine
			// local fault (corrupt object DB, index lock, .git permission error),
			// not just the benign "not committed yet" cases documented above — is
			// folded into errChunkAbsentAtHead. We cannot reliably distinguish
			// those from git's exit code/stderr alone. Only a missing/unrunnable
			// git binary (the non-ExitError branch below) and a corrupt COMMITTED
			// chunk (the parse failure after a successful `git show`) are
			// surfaced as real errors.
			return nil, fmt.Errorf("%w: %s", errChunkAbsentAtHead, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("memory pending: git show: %w", err)
	}
	recs, err := memory.ParseChunk(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("memory pending: parse HEAD chunk: %w", err)
	}
	return recs, nil
}

// printPendingRecords renders one review entry per record: a summary line
// with type/title/author, then its content's first line indented two spaces.
// A record whose SyncID is in edited gets an " (edited)" suffix after the
// author, distinguishing a revised note from a brand-new one — both flow to
// the shared chunk, but a human reviewing `memory pending` needs to know
// which. edited may be nil (e.g. the "not committed yet" case, where nothing
// is distinguished from anything else).
func printPendingRecords(w io.Writer, recs []memory.ChunkRecord, edited map[string]bool) {
	for _, r := range recs {
		suffix := ""
		if edited[r.SyncID] {
			suffix = " (edited)"
		}
		fmt.Fprintf(w, "- [%s] %s (%s)%s\n", r.Type, r.Title, r.Author, suffix)
		fmt.Fprintf(w, "  %s\n", firstLine(r.Content, 200))
	}
}

var memoryImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Merge committed chunk files into the local memory store",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		start := time.Now()
		err := runMemoryImport(cmd)
		if memImportQuiet {
			recordHook("memory import", time.Since(start), err)
		}
		return err
	},
}

func runMemoryImport(cmd *cobra.Command) error {
	recs, err := memory.ReadAllChunks(memoryChunksDir(repoRoot()))
	if err != nil {
		return err
	}
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		res, err := s.ImportRecords(recs)
		if err != nil {
			return err
		}
		if !memImportQuiet {
			fmt.Fprintf(cmd.OutOrStdout(), "imported: %d new, %d updated, %d unchanged\n",
				res.Inserted, res.Updated, res.Skipped)
		}
		return nil
	})
}

var memoryChunksCmd = &cobra.Command{
	Use:   "chunks",
	Short: "List committed memory chunk files",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := os.ReadDir(memoryChunksDir(repoRoot()))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("memory chunks: %w", err)
		}
		n := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "chunk-") && strings.HasSuffix(e.Name(), ".jsonl.gz") {
				fmt.Fprintln(cmd.OutOrStdout(), e.Name())
				n++
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d chunk file(s)\n", n)
		return nil
	},
}
