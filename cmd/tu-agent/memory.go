package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/codegen"
	"github.com/tu/tu-agent/internal/crystallize"
	"github.com/tu/tu-agent/internal/memory"
	"github.com/tu/tu-agent/internal/telemetry"
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

var memMaterializeQuiet bool

var memCrystallizeMin int

var memCrystallizeNudge bool

var crystallizeProvider string

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
	tel, err := telemetry.NewLogger(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}
	contextSize := effectiveContextSize(cfg.Providers[resolveProviderName(cfg, task, crystallizeProvider)].ContextSize, prov)
	body, err := codegen.GenerateSkill(cmd.Context(), label, notes, prov, tel, contextSize)
	if err != nil {
		return err
	}
	// saveCrystallizedSkill re-detects the cluster to compute provenance from the members at save time; do not pass these notes through to avoid that.
	path, err := saveCrystallizedSkill(label, body)
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
		clusters := crystallize.Detect(obs, memCrystallizeMin)
		// Stored skill hashes by cluster label (topic skill/<label>).
		stored := map[string]string{}
		for _, o := range obs {
			if o.Type == "skill" {
				stored[strings.TrimPrefix(o.TopicKey, "skill/")] = crystallize.ParseSourceHash(o.Content)
			}
		}
		status := map[string]crystallize.SkillStatus{}
		needs := 0
		for _, c := range clusters {
			st := crystallize.Classify(c, stored[crystallize.SkillName(c.Label)])
			status[c.Label] = st
			if st != crystallize.StatusCurrent {
				needs++
			}
		}
		if memCrystallizeNudge {
			if needs > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "tu-agent: %d note cluster(s) ready to crystallize — run `tu-agent memory crystallize`\n", needs)
			}
			return nil
		}
		fmt.Fprint(cmd.OutOrStdout(), crystallize.FormatWithStatus(clusters, status))
		return nil
	},
}

var memoryMaterializeCmd = &cobra.Command{
	Use:   "materialize",
	Short: "Render crystallized skill records to local .claude/skills files",
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
		written := 0
		base := generatedSkillsDir(repoRoot())
		for _, o := range obs {
			if o.Type != "skill" {
				continue
			}
			name := strings.TrimPrefix(o.TopicKey, "skill/")
			if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
				continue // defensive: skill names are a single path segment
			}
			path := filepath.Join(base, name, "SKILL.md")
			existing, readErr := os.ReadFile(path)
			if readErr != nil && !os.IsNotExist(readErr) {
				return fmt.Errorf("memory materialize: read %s: %w", path, readErr)
			}
			if !crystallize.MaterializeDecision(existing) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("memory materialize: %w", err)
			}
			if err := os.WriteFile(path, []byte(o.Content), 0o644); err != nil {
				return fmt.Errorf("memory materialize: %w", err)
			}
			written++
		}
		if !memMaterializeQuiet {
			fmt.Fprintf(cmd.OutOrStdout(), "materialized %d skill(s)\n", written)
		}
		return nil
	},
}

// saveCrystallizedSkill stores a generated skill body as the canonical
// skill/<label> record (with binary-computed provenance) and materializes it to
// the local .claude/skills file. Shared by the CLI and the crystallize_save MCP
// tool so both paths produce an identical record + file. Returns the file path.
func saveCrystallizedSkill(label, body string) (string, error) {
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
	for _, c := range crystallize.Detect(obs, memCrystallizeMin) {
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

func init() {
	memorySaveCmd.Flags().StringVar(&memSaveTopic, "topic", "", "topic key for upsert, e.g. architecture/auth (required)")
	memorySaveCmd.Flags().StringVar(&memSaveContent, "content", "", "observation content (required)")
	memorySaveCmd.Flags().StringVar(&memSaveType, "type", "", "observation type label")
	memorySaveCmd.Flags().StringVar(&memSaveSource, "source", "", "where the observation came from")
	memorySaveCmd.Flags().StringVar(&memSaveScope, "scope", "", "scope: project (default, shared) or personal (local-only, not exported)")
	memoryCmd.AddCommand(memorySaveCmd)
	memoryListCmd.Flags().BoolVar(&memShowIDs, "ids", false, "show the observation ID (needed for link/relate/delete)")
	memoryCmd.AddCommand(memoryListCmd)
	memorySearchCmd.Flags().StringVar(&memSearchType, "type", "", "restrict to one observation type (bug-pattern|decision|architecture|testing|reference|gotcha|skill)")
	memorySearchCmd.Flags().BoolVar(&memShowIDs, "ids", false, "show the observation ID (needed for link/relate/delete)")
	memoryCmd.AddCommand(memorySearchCmd)
	memoryLinkCmd.Flags().StringVar(&memLinkFrom, "from", "", "source id (observation ID or graph node ID)")
	memoryLinkCmd.Flags().StringVar(&memLinkTo, "to", "", "target id (observation ID or graph node ID)")
	memoryLinkCmd.Flags().StringVar(&memLinkType, "type", "related", "relation type: related|supersedes|documents|conflicts_with")
	memoryLinksCmd.Flags().StringVar(&memLinksOf, "of", "", "list relations touching this id (required)")
	memoryCmd.AddCommand(memoryLinkCmd)
	memoryCmd.AddCommand(memoryLinksCmd)
	memoryCmd.AddCommand(memoryConflictsCmd)
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
	memoryCrystallizeCmd.Flags().BoolVar(&memCrystallizeNudge, "nudge", false, "print a one-line summary only if clusters need crystallizing (for hooks)")
	memoryCrystallizeCmd.Flags().StringVar(&crystallizeProvider, "provider", "", "provider override for CLI generation (claude|local)")
	memoryCmd.AddCommand(memoryCrystallizeCmd)
	memoryMaterializeCmd.Flags().BoolVar(&memMaterializeQuiet, "quiet", false, "suppress output (for hooks)")
	memoryCmd.AddCommand(memoryMaterializeCmd)
	rootCmd.AddCommand(memoryCmd)
}
