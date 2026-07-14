// Package reconcile builds the deterministic dry-run plan that reconciles
// orphaned skill records — those whose bound cluster label no longer matches any
// live cluster — against the current memory corpus. It is the shared core both
// `memory reconcile` (CLI) and `mem_reconcile` (MCP) drive, so the two surfaces
// report byte-identical plans (§10 parity).
//
// Everything here is dry-run: PlanFrom is pure and DryRun only reads. Applying a
// plan (renaming records/folders) is a later leg and lives elsewhere.
package reconcile

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// Candidate is a live cluster proposed as the re-point target for an orphan,
// scored by member-overlap Jaccard.
type Candidate struct {
	Label   string
	Overlap float64
}

// OrphanPlan is the reconcile action for a single orphaned skill record: its
// current topic key and bound label, plus the ranked candidate clusters it could
// be re-pointed at.
type OrphanPlan struct {
	Topic      string
	Label      string
	Candidates []Candidate
}

// Folder is a materialized skill directory on disk. Marked reports whether its
// SKILL.md carries the crystallize marker (so it is safe to rewrite). Folders are
// carried through the planner for the later --apply leg (folder renames); the
// dry-run plan derives orphans from the memory records, not from folder names.
type Folder struct {
	Name   string
	Marked bool
}

// Plan is the full dry-run reconcile report.
type Plan struct {
	Orphans []OrphanPlan
}

const overlapEps = 1e-9

// autoApplyFloor is the minimum member-overlap Jaccard a candidate must clear to
// be eligible for auto-apply (decision D5): a remap is auto-applyable only when
// exactly one candidate clears it.
const autoApplyFloor = 0.5

// Suggest ranks live clusters as candidate re-point targets for an orphan by the
// member-overlap Jaccard of their topic-key sets, ordered (Overlap desc, Label
// asc). Clusters with no overlap are omitted. Pure and deterministic.
func Suggest(orphanMembers []memory.Observation, clusters []crystallize.Cluster) []Candidate {
	orphanSet := topicSet(orphanMembers)
	out := make([]Candidate, 0, len(clusters))
	for _, c := range clusters {
		j := jaccard(orphanSet, topicSet(c.Members))
		if j <= 0 {
			continue
		}
		out = append(out, Candidate{Label: c.Label, Overlap: j})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if math.Abs(out[i].Overlap-out[k].Overlap) > overlapEps {
			return out[i].Overlap > out[k].Overlap
		}
		return out[i].Label < out[k].Label
	})
	return out
}

// AutoApplyTarget encodes the locked ambiguity rule (decision D5): a remap is
// auto-applyable ONLY when exactly one candidate clears the overlap floor;
// otherwise ("", false) and the orphan stays visibly orphaned.
func AutoApplyTarget(candidates []Candidate) (string, bool) {
	target := ""
	count := 0
	for _, c := range candidates {
		if c.Overlap >= autoApplyFloor {
			count++
			target = c.Label
		}
	}
	if count == 1 {
		return target, true
	}
	return "", false
}

// PlanFrom builds the dry-run reconcile plan from an in-memory corpus and the
// materialized skill folders. It is pure, mutates neither slice, and is
// order-invariant in both: all output (orphan order, candidate order) is sorted
// so the plan is byte-identical under input reordering.
func PlanFrom(obs []memory.Observation, folders []Folder, minSize int) Plan {
	_ = folders // reserved for the --apply leg; orphans derive from records.

	clusters := crystallize.Detect(obs, minSize)
	byLabel := make(map[string]crystallize.Cluster, len(clusters))
	for _, c := range clusters {
		byLabel[c.Label] = c
	}

	orphans := make([]OrphanPlan, 0)
	for _, o := range obs {
		if o.Type != "skill" {
			continue
		}
		if crystallize.RecordStatus(o, byLabel) != crystallize.StatusOrphan {
			continue
		}
		// Member sets are not stored yet (deferred to a later leg), so there are
		// no orphan members to overlap-score against live clusters. Suggest with
		// an empty member set yields no candidates until then.
		orphans = append(orphans, OrphanPlan{
			Topic:      o.TopicKey,
			Label:      crystallize.RecordLabel(o),
			Candidates: Suggest(nil, clusters),
		})
	}
	sort.SliceStable(orphans, func(i, k int) bool {
		if orphans[i].Topic != orphans[k].Topic {
			return orphans[i].Topic < orphans[k].Topic
		}
		return orphans[i].Label < orphans[k].Label
	})
	return Plan{Orphans: orphans}
}

// DryRun is the store/disk adapter over PlanFrom: it reads the live observations
// and scans skillsDir, then returns the plan. It WRITES NOTHING.
func DryRun(store *memory.Store, skillsDir string, minSize int) (Plan, error) {
	obs, err := store.List()
	if err != nil {
		return Plan{}, fmt.Errorf("reconcile.DryRun: %w", err)
	}
	folders, err := scanFolders(skillsDir)
	if err != nil {
		return Plan{}, fmt.Errorf("reconcile.DryRun: %w", err)
	}
	return PlanFrom(obs, folders, minSize), nil
}

// scanFolders reads (never writes) skillsDir, returning one Folder per
// subdirectory that carries a SKILL.md. A missing skillsDir is not an error.
func scanFolders(skillsDir string) ([]Folder, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reconcile.scanFolders: %w", err)
	}
	folders := make([]Folder, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		b, rerr := os.ReadFile(skillPath)
		if rerr != nil {
			if os.IsNotExist(rerr) {
				continue // a directory without SKILL.md is not a skill folder
			}
			return nil, fmt.Errorf("reconcile.scanFolders: read %s: %w", skillPath, rerr)
		}
		folders = append(folders, Folder{
			Name:   e.Name(),
			Marked: strings.Contains(string(b), crystallize.Marker),
		})
	}
	return folders, nil
}

// topicSet is the set of member topic keys.
func topicSet(obs []memory.Observation) map[string]struct{} {
	s := make(map[string]struct{}, len(obs))
	for _, o := range obs {
		s[o.TopicKey] = struct{}{}
	}
	return s
}

// jaccard is |a∩b| / |a∪b| over two topic-key sets (0 when both are empty).
func jaccard(a, b map[string]struct{}) float64 {
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	inter := 0
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
