package reconcile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// ApplyOptions carries the opt-in rename-path flags. The zero value (both fields
// "") reproduces the default rebind-only ApplyPlan behaviour.
type ApplyOptions struct {
	// Name is the --name flag: rename skill/<old> -> skill/<Name> (and the
	// on-disk folder), minting a new sync_id via Retopic. Empty leaves the topic
	// key unchanged (rebind-only, sync_id stable).
	Name string
	// ToCluster is the --to-cluster flag: force the re-point target to this live
	// cluster's label, overriding AutoApplyTarget even for an ambiguous orphan.
	// Empty falls back to AutoApplyTarget.
	ToCluster string
	// PruneFolders is the --prune-folders flag (CLI) / prune_folders arg (MCP):
	// gates reconcileFolders' os.RemoveAll of orphaned crystallize-MARKED
	// folders. false (the default) is a dry-run — candidates are scanned and
	// reported in ApplyResult.WouldRemove but never removed; true removes them
	// and reports them in ApplyResult.Removed instead.
	PruneFolders bool
}

// ApplyPlanWithOptions is the rename-aware apply core. With a zero ApplyOptions
// it is exactly ApplyPlan (rebind-only, sync_id stable). Otherwise, for each
// orphan in the plan:
//   - if opts.ToCluster != "" and names a live cluster, that label is the forced
//     re-point target (it overrides AutoApplyTarget, even when the orphan is
//     ambiguous); with opts.ToCluster == "" the target is AutoApplyTarget;
//   - the record's provenance is rewritten to label=<target> with the
//     source-hash recomputed against the matched cluster's members;
//   - if opts.Name != "", the record is renamed skill/<old> -> skill/<Name> via
//     store.Retopic (which mints a new sync_id and bumps revision) and the
//     on-disk MARKED folder skillsDir/<old>/ is renamed to skillsDir/<Name>/; a
//     missing source folder is NOT an error.
//
// Independently of the orphan loop it reconciles folders under skillsDir: a
// crystallize-MARKED folder whose skill name matches no live record is a
// removal candidate — actually removed only when opts.PruneFolders is true
// (reported in ApplyResult.Removed); otherwise it is left on disk and reported
// in ApplyResult.WouldRemove (dry-run, the default). A marked folder backed by
// a live record is kept; a hand-written folder (no crystallize marker) is left
// byte-for-byte untouched regardless of the flag. Deterministic and
// idempotent.
func ApplyPlanWithOptions(store *memory.Store, plan Plan, clusters []crystallize.Cluster, skillsDir string, opts ApplyOptions) (ApplyResult, error) {
	byLabel := make(map[string]crystallize.Cluster, len(clusters))
	for _, c := range clusters {
		byLabel[c.Label] = c
	}

	obs, err := store.List()
	if err != nil {
		return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: %w", err)
	}
	byTopic := make(map[string]memory.Observation, len(obs))
	for _, o := range obs {
		byTopic[o.TopicKey] = o
	}

	res := ApplyResult{}
	for _, orphan := range plan.Orphans {
		target, ok := resolveTarget(opts, orphan, byLabel)
		if !ok {
			// ambiguous / no target / target not live — leave untouched, but report.
			res.Skipped = append(res.Skipped, orphan.Topic)
			continue
		}
		matched := byLabel[target] // resolveTarget guarantees this is present
		rec, ok := byTopic[orphan.Topic]
		if !ok {
			continue // record no longer present
		}
		// Guard on CURRENT state: if the record already binds to a live cluster,
		// it has been reconciled (idempotency).
		curLabel := crystallize.RecordLabel(rec)
		if _, live := byLabel[curLabel]; live {
			continue
		}

		name := strings.TrimPrefix(rec.TopicKey, "skill/")

		// Name-sanitize guard: opts.Name flows unsanitized into skillsDir/<Name>
		// and a store Retopic to skill/<Name>. Reject any path-traversal-shaped
		// value here — before the destination-collision check, the provenance
		// Upsert, and Retopic — so a rejected Name never reaches a store or
		// filesystem mutation. An empty Name means "no rename" and stays valid.
		if opts.Name != "" {
			if opts.Name == "." || opts.Name == ".." ||
				strings.ContainsAny(opts.Name, "/\\") ||
				strings.ContainsRune(opts.Name, filepath.Separator) {
				return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: rejected Name %q: must be a single path segment other than %q or %q", opts.Name, ".", "..")
			}
		}

		// Destination-collision safety: when --name would rename this record, the
		// target folder must be FREE before any mutation. Validate it here — before
		// the provenance Upsert and the Retopic — so a pre-existing destination
		// aborts atomically, leaving the record and both folders untouched.
		if opts.Name != "" && opts.Name != name {
			if newDir := filepath.Join(skillsDir, opts.Name); dirExists(newDir) {
				return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: destination folder %s already exists", newDir)
			}
			// A live (non-deleted) record already occupying skill/<Name> would be
			// clobbered by Retopic. Reject here — before the provenance Upsert — so
			// the op aborts atomically, leaving both records untouched. byTopic is
			// built from store.List(), which excludes deleted rows, matching
			// Store.Retopic's own collision semantics.
			if _, taken := byTopic["skill/"+opts.Name]; taken {
				return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: destination skill/%s already exists", opts.Name)
			}
		}

		// Capture the ORIGINAL topic key before any mutation: Retopic (below)
		// changes the record's topic in the store, and a rename must report the
		// pre-rename "skill/<old>" as OldTopic.
		oldTopic := rec.TopicKey

		newContent := crystallize.ProvenanceCommentRe.ReplaceAllString(
			rec.Content, crystallize.ProvenanceLine(target, matched.Members))
		if _, err := store.Upsert(rec.TopicKey, newContent, memory.UpsertOpts{Type: "skill"}); err != nil {
			return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: rebind %s: %w", rec.TopicKey, err)
		}

		// Opt-in rename path: move the record AND its on-disk folder to <Name>. A
		// renamed record is reported ONLY in Renamed, never also in Rebound.
		if opts.Name != "" && opts.Name != name {
			newTopic := "skill/" + opts.Name
			if _, _, err := store.Retopic(rec.TopicKey, newTopic, "project", ""); err != nil {
				return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: retopic %s: %w", rec.TopicKey, err)
			}
			res.Renamed = append(res.Renamed, RenameAction{
				OldTopic: oldTopic,
				NewTopic: newTopic,
				Label:    target,
			})
			oldDir := filepath.Join(skillsDir, name)
			newDir := filepath.Join(skillsDir, opts.Name)
			// Only relocate a crystallize-MANAGED folder. A hand-written SKILL.md
			// carries no marker; renaming it would silently move a file the user
			// owns, so it is left untouched (the record still moves via Retopic).
			// This mirrors the marker guard reconcileFolders and the rebind-only
			// path enforce on hand-written skills.
			if dirExists(oldDir) {
				if folderMarked(oldDir) {
					if err := os.Rename(oldDir, newDir); err != nil {
						return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: rename folder %s: %w", oldDir, err)
					}
				} else {
					// Hand-written source folder: not renamed, so it is now orphaned
					// relative to the renamed record. Report the divergence keyed by
					// the POST-rename topic and pointing at the old folder's SKILL.md.
					res.Divergent = append(res.Divergent, Divergence{
						Topic: newTopic,
						Path:  filepath.Join(oldDir, "SKILL.md"),
					})
				}
			}
			// The managed folder was renamed, the source was absent (not an error),
			// or a hand-written source was reported as a divergence above.
			continue
		}

		// Rebind-only path: a pure rebind (no --name) is reported ONLY in Rebound.
		res.Rebound = append(res.Rebound, ReboundAction{
			Topic:    rec.TopicKey,
			OldLabel: curLabel,
			NewLabel: target,
		})

		// A hand-written SKILL.md is preserved, not rewritten; the record moved but
		// the file did not, so report the divergence.
		skillPath := filepath.Join(skillsDir, name, "SKILL.md")
		existing, rerr := os.ReadFile(skillPath)
		if rerr != nil {
			if os.IsNotExist(rerr) {
				continue
			}
			return ApplyResult{}, fmt.Errorf("reconcile.ApplyPlanWithOptions: read %s: %w", skillPath, rerr)
		}
		if !crystallize.MaterializeDecision(existing) {
			res.Divergent = append(res.Divergent, Divergence{Topic: rec.TopicKey, Path: skillPath})
		}
	}

	removed, wouldRemove, err := reconcileFolders(store, skillsDir, opts.PruneFolders)
	if err != nil {
		return ApplyResult{}, err
	}
	res.Removed = append(res.Removed, removed...)
	res.WouldRemove = append(res.WouldRemove, wouldRemove...)
	return res, nil
}

// resolveTarget picks the re-point target for an orphan. An explicit
// opts.ToCluster wins when it names a live cluster (overriding AutoApplyTarget,
// even for an ambiguous orphan); otherwise it falls back to AutoApplyTarget. The
// returned label is always a live cluster (present in byLabel).
func resolveTarget(opts ApplyOptions, orphan OrphanPlan, byLabel map[string]crystallize.Cluster) (string, bool) {
	if opts.ToCluster != "" {
		if _, ok := byLabel[opts.ToCluster]; ok {
			return opts.ToCluster, true
		}
		return "", false // named target is not a live cluster — nothing to bind to
	}
	target, ok := AutoApplyTarget(orphan.Candidates)
	if !ok {
		return "", false
	}
	if _, ok := byLabel[target]; !ok {
		return "", false
	}
	return target, true
}

// reconcileFolders enforces the D8 folder/record invariant under skillsDir: a
// crystallize-MARKED folder whose skill name maps to no live record is a
// removal candidate; a marked folder backed by a live record is kept; a
// hand-written folder (no marker) is always left untouched. It runs
// independently of the orphan loop. When prune is false it is a dry-run: every
// candidate is reported in the second return (wouldRemove) and zero
// os.RemoveAll calls are made. When prune is true, candidates are actually
// removed and reported in the first return (removed) instead. Both slices are
// bare folder names, in scan order.
func reconcileFolders(store *memory.Store, skillsDir string, prune bool) (removed, wouldRemove []string, err error) {
	folders, err := scanFolders(skillsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile.reconcileFolders: %w", err)
	}
	if len(folders) == 0 {
		return nil, nil, nil
	}
	obs, err := store.List()
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile.reconcileFolders: %w", err)
	}
	live := make(map[string]struct{}, len(obs))
	for _, o := range obs {
		live[o.TopicKey] = struct{}{}
	}
	removed = make([]string, 0, len(folders))
	wouldRemove = make([]string, 0, len(folders))
	for _, f := range folders {
		if !f.Marked {
			continue // hand-written: never touch
		}
		if _, ok := live["skill/"+f.Name]; ok {
			continue // backed by a live record
		}
		if !prune {
			wouldRemove = append(wouldRemove, f.Name)
			continue
		}
		if err := os.RemoveAll(filepath.Join(skillsDir, f.Name)); err != nil {
			return nil, nil, fmt.Errorf("reconcile.reconcileFolders: remove %s: %w", f.Name, err)
		}
		removed = append(removed, f.Name)
	}
	return removed, wouldRemove, nil
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// folderMarked reports whether the skill folder at dir is crystallize-managed:
// its SKILL.md carries the crystallize marker. A missing/unreadable SKILL.md
// counts as not managed (hand-written skill or not a skill folder), so it is
// never relocated. Mirrors scanFolders' Marked determination.
func folderMarked(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), crystallize.Marker)
}
