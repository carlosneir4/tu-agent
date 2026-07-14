package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/crystallize"
	"github.com/carlosneir4/tu-agent/internal/memory"
	"github.com/carlosneir4/tu-agent/internal/reconcile"
)

var memoryCmd = &cobra.Command{
	GroupID: "memory",
	Use:     "memory",
	Short:   "Inspect and update the project memory store",
}

var memorySaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save (upsert) an observation by topic key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if memSaveTopic == "" || memSaveContent == "" {
			return fmt.Errorf("--topic and --content are required")
		}
		return withMemStore(repoRoot(), func(s *memory.Store) error {
			obs, err := s.Upsert(memSaveTopic, memSaveContent, memory.UpsertOpts{
				Scope:  memSaveScope,
				Type:   memSaveType,
				Source: memSaveSource,
				Author: gitAuthor(),
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved %s rev:%d\n", obs.TopicKey, obs.Revision)
			return nil
		})
	},
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored observations",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withMemStore(repoRoot(), func(s *memory.Store) error {
			obs, err := s.List()
			if err != nil {
				return err
			}
			if len(obs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no observations stored")
				return nil
			}
			stale := recallStale(s, obs)
			for _, o := range obs {
				printObservationLine(cmd.OutOrStdout(), o, stale[o.ID], memShowIDs)
			}
			return nil
		})
	},
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search observations by keyword",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withMemStore(repoRoot(), func(s *memory.Store) error {
			obs, total, err := s.Search(args[0], memSearchType, memSearchLimit)
			if err != nil {
				return err
			}
			if len(obs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no observations found")
				return nil
			}
			stale := recallStale(s, obs)
			for _, o := range obs {
				printObservationLine(cmd.OutOrStdout(), o, stale[o.ID], memShowIDs)
			}
			if total > len(obs) {
				fmt.Fprintf(cmd.OutOrStdout(), "showing %d of %d — refine the query or raise --limit\n", len(obs), total)
			}
			return nil
		})
	},
}

// printObservationLine writes one observation as a single summary line. The hex
// ID is shown only when showID is set (it is noise for browsing but needed to
// copy into link/relate/delete). The type sits in a fixed-width column so the
// left edge stays aligned even when topic keys vary wildly in length. A positive
// staleCount appends a marker that the note links to graph symbols that no
// longer exist.
func printObservationLine(w io.Writer, o memory.Observation, staleCount int, showID bool) {
	key := o.TopicKey
	if key == "" {
		key = o.Title
	}
	typ := o.Type
	if typ == "" {
		typ = "-"
	}
	marker := ""
	if staleCount > 0 {
		marker = fmt.Sprintf("  ⚠stale:%d", staleCount)
	}
	prefix := ""
	if showID {
		prefix = o.ID + "  "
	}
	fmt.Fprintf(w, "%s%s  %-12s  rev:%d  %s  %s%s\n",
		prefix, o.UpdatedAt.Format("2006-01-02"), typ, o.Revision, key, firstLine(o.Content, 80), marker)
}

// firstLine returns the first line of s truncated to max runes.
func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func runMemoryLink(from, to, relType string, out io.Writer) error {
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		rel, err := s.Relate(from, to, relType)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "linked %s --%s--> %s\n", rel.FromID, rel.Type, rel.ToID)
		return nil
	})
}

func runMemoryLinks(of string, out io.Writer) error {
	if of == "" {
		return fmt.Errorf("memory links: --of <id> is required")
	}
	return withMemStore(repoRoot(), func(s *memory.Store) error {
		to, err := s.RelationsTo([]string{of})
		if err != nil {
			return err
		}
		from, err := s.RelationsFrom([]string{of})
		if err != nil {
			return err
		}
		rels := append(append([]memory.Relation{}, to...), from...)
		if len(rels) == 0 {
			fmt.Fprintln(out, "no relations")
			return nil
		}
		for _, r := range rels {
			fmt.Fprintf(out, "%s --%s--> %s\n", r.FromID, r.Type, r.ToID)
		}
		return nil
	})
}

var memoryLinkCmd = &cobra.Command{
	Use: "link", Short: "Link an observation to a graph node (or another observation)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if memLinkFrom == "" || memLinkTo == "" {
			return fmt.Errorf("--from and --to are required")
		}
		return runMemoryLink(memLinkFrom, memLinkTo, memLinkType, cmd.OutOrStdout())
	},
}

var memoryLinksCmd = &cobra.Command{
	Use: "links", Short: "List relations for an id (--of)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error { return runMemoryLinks(memLinksOf, cmd.OutOrStdout()) },
}

// runRescope changes an observation's scope in place and reports the outcome.
func runRescope(s *memory.Store, out io.Writer, topic, fromScope, toScope string) error {
	if topic == "" {
		return fmt.Errorf("--topic is required")
	}
	if toScope == "" {
		return fmt.Errorf("--scope is required")
	}
	obs, changed, err := s.Rescope(topic, fromScope, toScope, "")
	if err != nil {
		return err
	}
	switch {
	case changed:
		fmt.Fprintf(out, "rescoped %s: %s → %s\n", obs.TopicKey, fromScope, toScope)
	case obs.TopicKey != "":
		fmt.Fprintf(out, "%s already in scope %s\n", topic, toScope)
	default:
		fmt.Fprintf(out, "no observation found for %s in scope %s\n", topic, fromScope)
	}
	return nil
}

// runDelete soft-deletes an observation and reports the outcome.
func runDelete(s *memory.Store, out io.Writer, topic, scope string) error {
	if topic == "" {
		return fmt.Errorf("--topic is required")
	}
	ok, err := s.Delete(topic, scope, "")
	if err != nil {
		return err
	}
	if ok {
		fmt.Fprintf(out, "deleted %s\n", topic)
	} else {
		fmt.Fprintf(out, "no observation found for %s in scope %s\n", topic, scope)
	}
	return nil
}

// renderReconcilePlan is the single shared adapter both `memory reconcile` (CLI)
// and the `mem_reconcile` MCP tool call: it builds the dry-run plan over the live
// store + skills dir and renders it through reconcile.RenderPlan, so both
// surfaces emit byte-identical plan text (§10 parity). It reads only — no rows or
// files are mutated.
func renderReconcilePlan(s *memory.Store, skillsDir string, minSize int) (string, error) {
	plan, err := reconcile.DryRun(s, skillsDir, minSize)
	if err != nil {
		return "", err
	}
	return reconcile.RenderPlan(plan), nil
}

// applyReconcile is the single shared adapter both `memory reconcile --apply`
// (CLI) and `mem_reconcile` with apply=true (MCP) call, so both surfaces emit
// byte-identical apply text (§10 parity). It builds the dry-run plan, optionally
// narrows it to the single orphan named by selectorTopic (scoping --to-cluster /
// --name to one record), computes the live clusters for the apply core, then
// runs reconcile.ApplyPlanWithOptions and renders the result. A bare apply (no
// selector) auto-applies no REBIND: member-sets are deferred to a later leg, so
// every orphan carries empty candidates and lands in ApplyResult.Skipped.
// Orphaned crystallize-marked skill FOLDERS are a separate concern, gated by
// opts.PruneFolders (CLI --prune-folders / MCP prune_folders): false (the
// default, threaded through even on a bare apply) never deletes anything —
// candidates are scanned and reported in ApplyResult.WouldRemove; true deletes
// them and reports them in ApplyResult.Removed.
func applyReconcile(s *memory.Store, skillsDir string, minSize int, selectorTopic string, opts reconcile.ApplyOptions) (string, error) {
	plan, err := reconcile.DryRun(s, skillsDir, minSize)
	if err != nil {
		return "", err
	}
	if selectorTopic != "" {
		filtered := make([]reconcile.OrphanPlan, 0, 1)
		for _, o := range plan.Orphans {
			if o.Topic == selectorTopic {
				filtered = append(filtered, o)
			}
		}
		if len(filtered) == 0 {
			return "", fmt.Errorf("applyReconcile: no orphaned skill record %s", selectorTopic)
		}
		plan.Orphans = filtered
	}
	obs, err := s.List()
	if err != nil {
		return "", fmt.Errorf("applyReconcile: %w", err)
	}
	clusters := crystallize.Detect(obs, minSize)
	res, err := reconcile.ApplyPlanWithOptions(s, plan, clusters, skillsDir, opts)
	if err != nil {
		return "", err
	}
	return reconcile.RenderApplyResult(res), nil
}

// validateReconcileTargeting enforces the shared flag/field guards for the apply
// path: targeting requires --apply, and --to-cluster / --name each require the
// single-orphan --topic selector. Both CLI and MCP call it so the two surfaces
// reject the same inputs identically.
func validateReconcileTargeting(apply bool, topic, toCluster, name string) error {
	if !apply && (topic != "" || toCluster != "" || name != "") {
		return fmt.Errorf("--topic/--to-cluster/--name require --apply")
	}
	if (toCluster != "" || name != "") && topic == "" {
		return fmt.Errorf("--to-cluster/--name require --topic to select one orphan")
	}
	return nil
}

var memoryReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Report orphaned skill records against the current corpus (dry-run; --apply to rebind)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateReconcileTargeting(memReconcileApply, memReconcileTopic, memReconcileToCluster, memReconcileName); err != nil {
			return err
		}
		root := repoRoot()
		return withMemStore(root, func(s *memory.Store) error {
			skillsDir := generatedSkillsDir(root)
			if !memReconcileApply {
				text, err := renderReconcilePlan(s, skillsDir, memReconcileMin)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), text)
				return nil
			}
			text, err := applyReconcile(s, skillsDir, memReconcileMin, memReconcileTopic,
				reconcile.ApplyOptions{Name: memReconcileName, ToCluster: memReconcileToCluster, PruneFolders: memReconcilePruneFolders})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		})
	},
}

var memoryRescopeCmd = &cobra.Command{
	Use:   "rescope",
	Short: "Change an existing observation's scope in place",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withMemStore(repoRoot(), func(s *memory.Store) error {
			return runRescope(s, cmd.OutOrStdout(), memRescopeTopic, memRescopeFrom, memRescopeScope)
		})
	},
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Soft-delete an observation (drops from search and the shared chunk)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withMemStore(repoRoot(), func(s *memory.Store) error {
			return runDelete(s, cmd.OutOrStdout(), memDeleteTopic, memDeleteScope)
		})
	},
}

var memoryRelinkCmd = &cobra.Command{
	Hidden: true,
	Use:    "relink",
	Short:  "Re-derive auto-links from note content to graph nodes",
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		start := time.Now()
		err := relinkObservations(cmd.OutOrStdout(), memRelinkQuiet)
		if memRelinkQuiet {
			recordHook("memory relink", time.Since(start), err)
		}
		return err
	},
}
