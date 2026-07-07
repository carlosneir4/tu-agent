package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
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
	memSearchLimit int
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
	},
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
		root := repoRoot()
		author := gitAuthor()
		chunkPath := memory.ChunkPath(memoryChunksDir(root), author)
		working, err := memory.ReadChunkFile(chunkPath)
		if err != nil {
			return fmt.Errorf("memory pending: read working chunk: %w", err)
		}
		out := cmd.OutOrStdout()
		headRecs, err := headChunkRecords(root, memory.RelChunkPath(author))
		if err != nil {
			fmt.Fprintf(out, "chunk not committed yet — showing all %d notes\n", len(working))
			printPendingRecords(out, working, nil)
			return nil
		}
		pending, edited := diffChunkRecords(working, headRecs)
		if len(pending) == 0 {
			fmt.Fprintln(out, "nothing pending — team chunk is committed")
			return nil
		}
		printPendingRecords(out, pending, edited)
		return nil
	},
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
// relPath (a repo-relative, forward-slash path). It errors when root is not a
// git repository, HEAD has no commits yet, or relPath is not present at HEAD —
// all of which the caller treats as "not committed yet".
func headChunkRecords(root, relPath string) ([]memory.ChunkRecord, error) {
	blob, err := exec.Command("git", "-C", root, "show", "HEAD:"+relPath).Output()
	if err != nil {
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
	memorySearchCmd.Flags().IntVar(&memSearchLimit, "limit", 20, "max results (0 = all)")
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
	memoryCmd.AddCommand(memoryPendingCmd)
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
