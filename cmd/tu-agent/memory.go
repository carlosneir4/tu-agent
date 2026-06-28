package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
)

var (
	memSaveTopic   string
	memSaveContent string
	memSaveType    string
	memSaveSource  string
	memSaveScope   string
	memSearchType  string
	memShowIDs     bool
)

var (
	memRescopeTopic string
	memRescopeScope string
	memRescopeFrom  string
	memDeleteTopic  string
	memDeleteScope  string
)

var (
	memLinkFrom string
	memLinkTo   string
	memLinkType string
	memLinksOf  string
)

var memImportQuiet bool

var memExportQuiet bool

var memRelinkQuiet bool

var memCrystallizeMin int

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Inspect and update the project memory store",
}

var memorySaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save (upsert) an observation by topic key",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if memSaveTopic == "" || memSaveContent == "" {
			return fmt.Errorf("--topic and --content are required")
		}
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
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
	},
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored observations",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
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
	},
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search observations by keyword",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		obs, err := s.Search(args[0], memSearchType)
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
		return nil
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
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
	rel, err := s.Relate(from, to, relType)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "linked %s --%s--> %s\n", rel.FromID, rel.Type, rel.ToID)
	return nil
}

func runMemoryLinks(of string, out io.Writer) error {
	if of == "" {
		return fmt.Errorf("memory links: --of <id> is required")
	}
	s, err := memory.Open(memoryDBPath(repoRoot()))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := s.Close(); cerr != nil {
			slog.Warn("memory store close failed", "err", cerr)
		}
	}()
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
}

var memoryExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Write your authored observations to a committed chunk file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		author := gitAuthor()
		recs, err := s.ExportRecords(author)
		if err != nil {
			return err
		}
		path, written, err := memory.WriteChunk(memoryChunksDir(repoRoot()), author, recs)
		if err != nil {
			return err
		}
		if memExportQuiet {
			return nil
		}
		if !written {
			fmt.Fprintf(cmd.OutOrStdout(), "no changes; %s is up to date (%d observations)\n", path, len(recs))
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d observations) — commit it to share\n", path, len(recs))
		return nil
	},
}

var memoryImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Merge committed chunk files into the local memory store",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		recs, err := memory.ReadAllChunks(memoryChunksDir(repoRoot()))
		if err != nil {
			return err
		}
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		res, err := s.ImportRecords(recs)
		if err != nil {
			return err
		}
		if !memImportQuiet {
			fmt.Fprintf(cmd.OutOrStdout(), "imported: %d new, %d updated, %d unchanged\n",
				res.Inserted, res.Updated, res.Skipped)
		}
		return nil
	},
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

var memoryRescopeCmd = &cobra.Command{
	Use:   "rescope",
	Short: "Change an existing observation's scope in place",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		return runRescope(s, cmd.OutOrStdout(), memRescopeTopic, memRescopeFrom, memRescopeScope)
	},
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Soft-delete an observation (drops from search and the shared chunk)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		return runDelete(s, cmd.OutOrStdout(), memDeleteTopic, memDeleteScope)
	},
}

var memoryRelinkCmd = &cobra.Command{
	Use:   "relink",
	Short: "Re-derive auto-links from note content to graph nodes",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return relinkObservations(cmd.OutOrStdout(), memRelinkQuiet)
	},
}

var memoryCrystallizeCmd = &cobra.Command{
	Use:   "crystallize",
	Short: "List dense note clusters worth consolidating into a skill",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := memory.Open(memoryDBPath(repoRoot()))
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil {
				slog.Warn("memory store close failed", "err", cerr)
			}
		}()
		obs, err := s.List()
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), crystallize.Format(crystallize.Detect(obs, memCrystallizeMin)))
		return nil
	},
}

func init() {
	memorySaveCmd.Flags().StringVar(&memSaveTopic, "topic", "", "topic key for upsert, e.g. architecture/auth (required)")
	memorySaveCmd.Flags().StringVar(&memSaveContent, "content", "", "observation content (required)")
	memorySaveCmd.Flags().StringVar(&memSaveType, "type", "", "observation type label")
	memorySaveCmd.Flags().StringVar(&memSaveSource, "source", "", "where the observation came from")
	memorySaveCmd.Flags().StringVar(&memSaveScope, "scope", "", "scope: project (default, shared) or personal (local-only, not exported)")
	memoryCmd.AddCommand(memorySaveCmd)
	memoryListCmd.Flags().BoolVar(&memShowIDs, "ids", false, "show the observation ID (needed for link/relate/delete)")
	memoryCmd.AddCommand(memoryListCmd)
	memorySearchCmd.Flags().StringVar(&memSearchType, "type", "", "restrict to one observation type (bug-pattern|decision|architecture|testing|reference|gotcha)")
	memorySearchCmd.Flags().BoolVar(&memShowIDs, "ids", false, "show the observation ID (needed for link/relate/delete)")
	memoryCmd.AddCommand(memorySearchCmd)
	memoryLinkCmd.Flags().StringVar(&memLinkFrom, "from", "", "source id (observation ID or graph node ID)")
	memoryLinkCmd.Flags().StringVar(&memLinkTo, "to", "", "target id (observation ID or graph node ID)")
	memoryLinkCmd.Flags().StringVar(&memLinkType, "type", "related", "relation type: related|supersedes|documents|conflicts_with")
	memoryLinksCmd.Flags().StringVar(&memLinksOf, "of", "", "list relations touching this id (required)")
	memoryCmd.AddCommand(memoryLinkCmd)
	memoryCmd.AddCommand(memoryLinksCmd)
	memoryCmd.AddCommand(memoryExportCmd)
	memoryExportCmd.Flags().BoolVar(&memExportQuiet, "quiet", false, "suppress output (for hooks)")
	memoryImportCmd.Flags().BoolVar(&memImportQuiet, "quiet", false, "suppress the summary line (for hooks)")
	memoryCmd.AddCommand(memoryImportCmd)
	memoryCmd.AddCommand(memoryChunksCmd)
	memoryRescopeCmd.Flags().StringVar(&memRescopeTopic, "topic", "", "topic key of the observation (required)")
	memoryRescopeCmd.Flags().StringVar(&memRescopeScope, "scope", "", "target scope, e.g. personal (required)")
	memoryRescopeCmd.Flags().StringVar(&memRescopeFrom, "from-scope", "project", "current scope to move from")
	memoryDeleteCmd.Flags().StringVar(&memDeleteTopic, "topic", "", "topic key of the observation (required)")
	memoryDeleteCmd.Flags().StringVar(&memDeleteScope, "scope", "project", "scope of the observation")
	memoryCmd.AddCommand(memoryRescopeCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)
	memoryRelinkCmd.Flags().BoolVar(&memRelinkQuiet, "quiet", false, "suppress output (for hooks)")
	memoryCmd.AddCommand(memoryRelinkCmd)
	memoryCrystallizeCmd.Flags().IntVar(&memCrystallizeMin, "min", 5, "minimum notes for a cluster to be suggested")
	memoryCmd.AddCommand(memoryCrystallizeCmd)
	rootCmd.AddCommand(memoryCmd)
}
