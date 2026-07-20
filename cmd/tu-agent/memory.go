package main

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

var memReconcileMin int

var (
	memReconcileApply        bool
	memReconcileTopic        string
	memReconcileToCluster    string
	memReconcileName         string
	memReconcilePruneFolders bool
)

var memCrystallizeNudge bool

var crystallizeProvider string

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
	memoryCmd.AddCommand(memoryApproveSkillCmd)
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
	memoryReconcileCmd.Flags().IntVar(&memReconcileMin, "min", 5, "minimum notes for a cluster to be considered live")
	memoryReconcileCmd.Flags().BoolVar(&memReconcileApply, "apply", false, "apply the reconcile (rebind/rename); absent = dry-run")
	memoryReconcileCmd.Flags().StringVar(&memReconcileTopic, "topic", "", "selector: scope the apply to ONE orphan by topic key (e.g. skill/acme-orphan)")
	memoryReconcileCmd.Flags().StringVar(&memReconcileToCluster, "to-cluster", "", "force the re-point target cluster label (requires --topic)")
	memoryReconcileCmd.Flags().StringVar(&memReconcileName, "name", "", "rename skill/<old> -> skill/<new> (record + folder; requires --topic)")
	memoryReconcileCmd.Flags().BoolVar(&memReconcilePruneFolders, "prune-folders", false, "actually delete orphaned crystallize-marked skill folders (requires --apply); absent = dry-run, candidates are reported as \"would remove\" but left on disk")
	memoryCmd.AddCommand(memoryReconcileCmd)
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
