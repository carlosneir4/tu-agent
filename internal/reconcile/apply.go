package reconcile

import (
	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// ReboundAction is one rebind an apply run performed: the record's (unchanged)
// topic key, its previous bound label, and the live cluster label it now binds
// to.
type ReboundAction struct {
	Topic    string
	OldLabel string
	NewLabel string
}

// Divergence flags a rebound record whose on-disk SKILL.md was left
// byte-for-byte intact because it is hand-written (no crystallize marker): the
// record's provenance was updated but the file was not.
type Divergence struct {
	Topic string
	Path  string
}

// RenameAction is one topic RENAME an apply run performed (the --name path): the
// record's old topic key, its new topic key, and the live cluster label it was
// re-pointed to. A rename is reported HERE, NOT also in Rebound — a renamed
// record must NOT be double-counted as a plain rebound.
type RenameAction struct {
	OldTopic string
	NewTopic string
	Label    string
}

// ApplyResult reports what an apply run changed.
type ApplyResult struct {
	Rebound     []ReboundAction // pure rebinds (label re-point, sync_id stable)
	Renamed     []RenameAction  // --name renames (topic + folder moved)
	Divergent   []Divergence    // record moved, hand-written SKILL.md left intact
	Removed     []string        // crystallize-managed skill FOLDER NAMES deleted (PruneFolders: true)
	WouldRemove []string        // crystallize-managed skill FOLDER NAMES reported as removal candidates but left on disk (PruneFolders: false, the default)
	Skipped     []string        // plan-orphan TOPIC KEYS left untouched (no target)
}

// ApplyPlan executes the rebind-only reconcile for a pre-computed plan against
// the store + skills dir. For each orphan it consults
// AutoApplyTarget(orphan.Candidates); ONLY when that yields a single target does
// it rebind — reading the record's current content, rewriting the provenance
// line to label=<target> with source-hash recomputed against the matched
// cluster's members (looked up in clusters by label), and persisting via Upsert
// on the SAME topic key (sync_id stable). Ambiguous / no-target orphans, and
// records whose current label already matches a live cluster (already
// reconciled), are left untouched. The on-disk folder is the record's existing
// skill name (TrimPrefix topic "skill/"): a hand-written SKILL.md
// (MaterializeDecision == false) is preserved and reported as a Divergence.
// Idempotent: a second call finds nothing to change.
//
// ApplyPlan is the zero-option case of ApplyPlanWithOptions (the rename-aware
// core): it carries no --name / --to-cluster flags, so it stays rebind-only.
func ApplyPlan(store *memory.Store, plan Plan, clusters []crystallize.Cluster, skillsDir string) (ApplyResult, error) {
	return ApplyPlanWithOptions(store, plan, clusters, skillsDir, ApplyOptions{})
}
