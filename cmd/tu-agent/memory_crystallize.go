package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

func runCrystallizeGenerate(cmd *cobra.Command, label string) error {
	root := repoRoot()
	s, err := memory.Open(memoryDBPath(root))
	if err != nil {
		return fmt.Errorf("crystallize generate: open store: %w", err)
	}
	obs, err := s.List()
	if err != nil {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
		return fmt.Errorf("crystallize generate: list observations: %w", err)
	}
	if cerr := s.Close(); cerr != nil {
		slog.Warn("memory store close failed", "err", cerr)
	}
	clusters := crystallize.Detect(obs, memCrystallizeMin)
	var notes []codegen.NoteInput
	labels := make([]string, 0, len(clusters))
	for _, c := range clusters {
		labels = append(labels, c.Label)
		if c.Label == label {
			for _, m := range c.Members {
				notes = append(notes, codegen.NoteInput{Topic: m.TopicKey, Type: m.Type, Content: m.Content})
			}
		}
	}
	if len(notes) == 0 {
		return fmt.Errorf("no current cluster labeled %q (available: %s)", label, strings.Join(labels, ", "))
	}
	// Mirror runSynthesize: reuse the package-global `cfg` and the synthesize
	// provider-routing slot (falling back like runSynthesize does).
	task := "synthesize"
	if _, ok := cfg.Routing.Tasks["synthesize"]; !ok {
		if _, ok := cfg.Routing.Tasks["consolidate"]; ok {
			task = "consolidate"
		} else if _, ok := cfg.Routing.Tasks["init"]; ok {
			task = "init"
		}
	}
	prov, err := selectProvider(cfg, task, crystallizeProvider)
	if err != nil {
		return fmt.Errorf("crystallize needs a configured provider for CLI generation (or use the plugin path): %w", err)
	}
	tel, err := telemetry.NewLogger(telemetryPath(root))
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}
	contextSize := effectiveContextSize(cfg.Providers[resolveProviderName(cfg, task, crystallizeProvider)].ContextSize, prov)
	body, err := codegen.GenerateSkill(cmd.Context(), label, notes, prov, tel, contextSize)
	if err != nil {
		return err
	}
	// saveCrystallizedSkill re-detects the cluster to compute provenance from the members at save time; do not pass these notes through to avoid that.
	path, err := saveCrystallizedSkill(label, body, 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "crystallized %s -> %s\n", label, path)
	return nil
}

var memoryCrystallizeCmd = &cobra.Command{
	Use:   "crystallize",
	Short: "List dense note clusters worth consolidating into a skill",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runCrystallizeGenerate(cmd, args[0])
		}
		start := time.Now()
		err := runMemoryCrystallizeList(cmd)
		if memCrystallizeNudge {
			recordHook("memory crystallize", time.Since(start), err)
		}
		return err
	},
}

func runMemoryCrystallizeList(cmd *cobra.Command) error {
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, err := s.List()
		if err != nil {
			return err
		}
		clusters := crystallize.Detect(obs, memCrystallizeMin)
		// Live clusters keyed by label, for classifying skill records against.
		byLabel := map[string]crystallize.Cluster{}
		for _, c := range clusters {
			byLabel[c.Label] = c
		}
		// Stored skill hashes keyed by the record's bound (parsed) label.
		stored := map[string]string{}
		for _, o := range obs {
			if o.Type == "skill" {
				stored[crystallize.RecordLabel(o)] = crystallize.ParseSourceHash(o.Content)
			}
		}
		status := map[string]crystallize.SkillStatus{}
		for _, c := range clusters {
			status[c.Label] = crystallize.Classify(c, stored[crystallize.SkillName(c.Label)])
		}
		if memCrystallizeNudge {
			needs, err := crystallizeNeeds(repoRoot())
			if err != nil {
				return err
			}
			if needs > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "tu-agent: %d note cluster(s) ready to crystallize — run `tu-agent memory crystallize`\n", needs)
			}
			return nil
		}
		fmt.Fprint(cmd.OutOrStdout(), crystallize.FormatWithStatus(clusters, status))
		orphanCount := 0
		for _, o := range obs {
			if o.Type == "skill" && crystallize.RecordStatus(o, byLabel) == crystallize.StatusOrphan {
				orphanCount++
			}
		}
		if orphanCount > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "%d orphaned skill record(s)\n", orphanCount)
		}
		return nil
	})
}

// crystallizeNeeds opens the memory store at root, detects clusters (at the
// memCrystallizeMin size threshold), and returns the count of clusters whose
// skill status is not crystallize.StatusCurrent — i.e. still need
// crystallizing. Shared by runMemoryCrystallizeList's --nudge branch and
// `tu-agent advise`'s crystallize-ready rule, so both surfaces agree on the
// same needs count.
func crystallizeNeeds(root string) (int, error) {
	s, err := memory.Open(memoryDBPath(root))
	if err != nil {
		return 0, fmt.Errorf("crystallizeNeeds: %w", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	obs, err := s.List()
	if err != nil {
		return 0, fmt.Errorf("crystallizeNeeds: %w", err)
	}
	clusters := crystallize.Detect(obs, memCrystallizeMin)
	// Stored skill hashes keyed by the record's bound (parsed) label.
	stored := map[string]string{}
	for _, o := range obs {
		if o.Type == "skill" {
			stored[crystallize.RecordLabel(o)] = crystallize.ParseSourceHash(o.Content)
		}
	}
	needs := 0
	for _, c := range clusters {
		if crystallize.Classify(c, stored[crystallize.SkillName(c.Label)]) != crystallize.StatusCurrent {
			needs++
		}
	}
	return needs, nil
}

var memoryMaterializeCmd = &cobra.Command{
	Hidden: true,
	Use:    "materialize",
	Short:  "Render crystallized skill records to local .claude/skills files",
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		start := time.Now()
		err := runMemoryMaterialize(cmd)
		if memMaterializeQuiet {
			recordHook("memory materialize", time.Since(start), err)
		}
		return err
	},
}

// invalidSkillName reports whether name is unsafe to use as a skill
// directory segment: empty, ".", "..", or containing a path separator
// (forward or backslash, matching reconcile.ApplyPlanWithOptions's guard).
// Shared so materialize and approve-skill cannot drift on the check.
func invalidSkillName(name string) bool {
	return name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, filepath.Separator)
}

// isLocalAuthor reports whether a skill record materializes under the
// local-authorship gate. A record is local when it never passed through
// Store.ImportRecords (o.Imported is false — the provenance marker, set only
// by import and never carried in a chunk, so it cannot be spoofed) AND its
// author is either empty (only local saves produce one) or matches the local
// git identity. localIdentity empty means no git identity is configured, so
// no non-empty author can match -- fail closed rather than treat everything
// as local. Checking both o.Imported and the author string is belt and
// suspenders: the marker alone stops a crafted chunk from forging local
// status, and the author check alone stops a record that was locally
// Upserted with a foreign author string. A foreign record can still
// materialize via the skill_approvals gate at the call site (approvals[name]
// == record.Revision).
func isLocalAuthor(o memory.Observation, localIdentity string) bool {
	return !o.Imported && (o.Author == "" || (localIdentity != "" && o.Author == localIdentity))
}

// materializeSkillFile writes content to <base>/<name>/SKILL.md, honoring
// crystallize.MaterializeDecision so a hand-edited file is never clobbered.
// Returns whether it wrote (false when an existing hand-edited file was
// preserved instead). Shared by the materialize loop and approve-skill's
// immediate materialization so both write skill files identically.
func materializeSkillFile(base, name, content string) (bool, error) {
	path := filepath.Join(base, name, "SKILL.md")
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, fmt.Errorf("read %s: %w", path, readErr)
	}
	if !crystallize.MaterializeDecision(existing) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func runMemoryMaterialize(cmd *cobra.Command) error {
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		obs, err := s.List()
		if err != nil {
			return err
		}
		approvals, err := loadSkillApprovals(s)
		if err != nil {
			return fmt.Errorf("memory materialize: %w", err)
		}
		localIdentity := gitAuthor()
		written := 0
		base := generatedSkillsDir(repoRoot())
		for _, o := range obs {
			if o.Type != "skill" {
				continue
			}
			name := strings.TrimPrefix(o.TopicKey, "skill/")
			if !isLocalAuthor(o, localIdentity) && approvals[name] != o.Revision {
				continue // foreign record; unapproved (or approved at a stale revision)
			}
			if invalidSkillName(name) {
				continue // defensive: skill names are a single path segment
			}
			ok, err := materializeSkillFile(base, name, o.Content)
			if err != nil {
				return fmt.Errorf("memory materialize: %w", err)
			}
			if ok {
				written++
			}
		}
		if !memMaterializeQuiet {
			fmt.Fprintf(cmd.OutOrStdout(), "materialized %d skill(s)\n", written)
		}
		return nil
	})
}

// saveCrystallizedSkill stores a generated skill body as the canonical
// skill/<label> record (with binary-computed provenance) and materializes it to
// the local .claude/skills file. Shared by the CLI and the crystallize_save MCP
// tool so both paths produce an identical record + file. Returns the file path.
//
// min is the minimum cluster size used to re-detect the cluster at save time;
// min <= 0 falls back to the package default (memCrystallizeMin), so the CLI
// caller (which already has its own --min flag state) can pass 0 to keep its
// existing behavior, while the MCP caller can pass the same min it used to
// discover the cluster via mem_clusters.
func saveCrystallizedSkill(label, body string, min int) (string, error) {
	if min <= 0 {
		min = memCrystallizeMin
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return "", fmt.Errorf("saveCrystallizedSkill: open store: %w", err)
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	obs, err := s.List()
	if err != nil {
		return "", fmt.Errorf("saveCrystallizedSkill: list observations: %w", err)
	}
	var members []memory.Observation
	for _, c := range crystallize.Detect(obs, min) {
		if c.Label == label {
			members = c.Members
			break
		}
	}
	if members == nil {
		return "", fmt.Errorf("saveCrystallizedSkill: no current cluster labeled %q", label)
	}
	content := crystallize.ProvenanceLine(label, members) + "\n" + body
	if _, err := s.Upsert(crystallize.SkillTopic(label), content, memory.UpsertOpts{Type: "skill"}); err != nil {
		return "", fmt.Errorf("saveCrystallizedSkill: %w", err)
	}
	name := crystallize.SkillName(label)
	path := filepath.Join(generatedSkillsDir(repoRoot()), name, "SKILL.md")
	existing, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", fmt.Errorf("saveCrystallizedSkill: read %s: %w", path, rerr)
	}
	if crystallize.MaterializeDecision(existing) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("saveCrystallizedSkill: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("saveCrystallizedSkill: %w", err)
		}
	} else {
		slog.Warn("crystallize: preserved existing hand-written skill file; record updated but file not overwritten", "path", path)
	}
	return path, nil
}

// formatConflicts renders conflicts_with edges, one per line, resolving each
// endpoint to its topic key (falling back to the raw id when an endpoint no
// longer resolves, e.g. a deleted note).
func formatConflicts(rels []memory.Relation, byID map[string]string) string {
	if len(rels) == 0 {
		return "no conflicts recorded\n"
	}
	label := func(id string) string {
		if k := byID[id]; k != "" {
			return k
		}
		return id
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d conflict(s):\n", len(rels))
	for _, r := range rels {
		fmt.Fprintf(&b, "  %s <-> %s\n", label(r.FromID), label(r.ToID))
	}
	return b.String()
}

// conflictTopicMap resolves the endpoints of the given relations to a map of
// observation id -> topic key (non-observation ids are simply absent).
func conflictTopicMap(s *memory.Store, rels []memory.Relation) (map[string]string, error) {
	idset := map[string]bool{}
	for _, r := range rels {
		idset[r.FromID] = true
		idset[r.ToID] = true
	}
	ids := make([]string, 0, len(idset))
	for id := range idset {
		ids = append(ids, id)
	}
	obs, err := s.ObservationsByID(ids)
	if err != nil {
		return nil, fmt.Errorf("conflictTopicMap: %w", err)
	}
	m := make(map[string]string, len(obs))
	for _, o := range obs {
		m[o.ID] = o.TopicKey
	}
	return m, nil
}

var memoryConflictsCmd = &cobra.Command{
	Use:   "conflicts",
	Short: "List recorded conflicts between notes (conflicts_with edges)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return fmt.Errorf("memory conflicts: %w", err)
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		rels, err := s.RelationsByType("conflicts_with")
		if err != nil {
			return fmt.Errorf("memory conflicts: %w", err)
		}
		byID, err := conflictTopicMap(s, rels)
		if err != nil {
			return fmt.Errorf("memory conflicts: %w", err)
		}
		fmt.Fprint(cmd.OutOrStdout(), formatConflicts(rels, byID))
		return nil
	},
}
