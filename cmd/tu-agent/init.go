package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/tdd"
)

const defaultInitPatterns = ".go,.java,.py,.ts,.js,.kt,.scala,.rb"

var (
	initPatterns       string
	initLang           string
	initNoLLM          bool
	initForce          bool
	initUpdate         bool
	initProvider       string
	initNoHarden       bool
	initPrivate        bool
	initPublic         bool
	initAugmentAgents  bool
	initAugmentExclude string
	initPlugin         bool
)

// initSetupOpts controls the setup-only init path.
type initSetupOpts struct {
	Subpath       string
	Lang          string
	NoLLM         bool
	Force         bool
	Update        bool
	Provider      string
	NoHarden      bool
	Private       bool
	AugmentAgents bool
	Exclude       string
	Plugin        bool
}

// runInitSetup performs setup-only init: detect language/build tooling, generate
// a CLAUDE.md knowledge block plus a hardened settings.json, and seed .tu-agent
// config. It does not materialize dev-flow agents. No LLM calls.
func runInitSetup(ctx context.Context, opts initSetupOpts) error {
	if opts.Update && opts.Force {
		return fmt.Errorf("init: --update and --force are mutually exclusive")
	}
	if opts.AugmentAgents {
		exclude := map[string]bool{}
		for _, name := range strings.Split(opts.Exclude, ",") {
			if name = strings.TrimSpace(name); name != "" {
				exclude[name] = true
			}
		}
		return augmentAgents(".", exclude)
	}
	info, err := codegen.Scan(".", opts.Subpath, true, parseExtensions(defaultInitPatterns))
	if err != nil {
		return fmt.Errorf("scanning codebase: %w", err)
	}
	lang, err := codegen.ResolveLanguage(opts.Lang, info.FilePaths)
	if err != nil {
		return fmt.Errorf("resolving language: %w", err)
	}
	buildTool := codegen.DetectBuildTool(".")
	testCmd := codegen.DetectTestCommandForRoot(".")
	// Emit the detected facts for the plugin orchestrator (informational).
	fmt.Printf("Detected language=%s build-tool=%s test-command=%q\n", lang, buildTool, testCmd)
	if changed, err := tdd.SeedProjectTestCommand(".", testCmd); err != nil {
		fmt.Fprintf(os.Stderr, "warning: seeding tdd.test_command: %v\n", err)
	} else if changed {
		fmt.Printf("Seeded tdd.test_command=%q into .tu-agent/config.yaml\n", testCmd)
	}
	if changed, err := tdd.SeedProjectLanguage(".", lang); err != nil {
		fmt.Fprintf(os.Stderr, "warning: seeding tdd.language: %v\n", err)
	} else if changed {
		fmt.Printf("Seeded tdd.language=%q into .tu-agent/config.yaml\n", lang)
	}

	if opts.Update {
		if err := refreshArtifacts("."); err != nil {
			return err
		}
	} else {
		if err := generateClaudeMD(info, lang, buildTool, testCmd, opts.Force); err != nil {
			fmt.Fprintf(os.Stderr, "warning: CLAUDE.md: %v\n", err)
		}
	}
	if !opts.NoHarden {
		home, _ := os.UserHomeDir()
		plugin := pluginContext(opts.Plugin, os.Getenv, home)
		if err := applyHardening(lang, buildTool, opts.Private, plugin); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hardening: %v\n", err)
		}
	}
	// Auto-learn: if the concept store is empty, populate it deterministically so
	// prepare leaves a usable knowledge index without a second manual step. A
	// missing/unreadable store counts as empty. Note this fires on --update too
	// when the store is empty: there is nothing to refresh, so a full build is
	// the right degrade (idempotent once populated — a later run is a no-op).
	if concepts, cerr := loadConceptSkills(); cerr != nil || len(concepts) == 0 {
		fmt.Println("prepare: concept store is empty — running the deterministic learn pipeline")
		if err := runLearn(ctx, learnOpts{SkipLLM: true, Subpath: opts.Subpath}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: auto-learn: %v\n", err)
		}
	}
	return nil
}

// refreshArtifacts re-applies the managed region of already-deployed artifacts
// in place: the CLAUDE.md knowledge block (if CLAUDE.md exists). It never creates
// CLAUDE.md, never touches agent files, and never runs the LLM.
func refreshArtifacts(root string) error {
	claudePath := filepath.Join(root, "CLAUDE.md")
	if _, err := os.Stat(claudePath); err == nil {
		if werr := writeKnowledgeBlock(claudePath); werr != nil {
			return fmt.Errorf("init --update: refreshing knowledge block: %w", werr)
		}
		fmt.Println("  Refreshed: CLAUDE.md knowledge block")
	} else if os.IsNotExist(err) {
		fmt.Println("  CLAUDE.md not found — skipped (run init first)")
	} else {
		return fmt.Errorf("init --update: stat CLAUDE.md: %w", err)
	}
	return nil
}

// writeBackupOnce writes content to bakPath with O_EXCL semantics: the first
// backup wins and a repeat run keeps it instead of clobbering it with content
// that is itself generated (same idiom as applyHardening's settings.json
// backup). Anything already at bakPath that is not a regular file — e.g. a
// directory — is an error, never treated as a usable backup.
func writeBackupOnce(bakPath string, content []byte) error {
	bak, err := os.OpenFile(bakPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	switch {
	case err == nil:
		_, werr := bak.Write(content)
		cerr := bak.Close()
		if werr != nil {
			return fmt.Errorf("writeBackupOnce: %w", werr)
		}
		if cerr != nil {
			return fmt.Errorf("writeBackupOnce: %w", cerr)
		}
		return nil
	case errors.Is(err, os.ErrExist):
		fi, serr := os.Stat(bakPath)
		if serr != nil {
			return fmt.Errorf("writeBackupOnce: %w", serr)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("writeBackupOnce: %s exists and is not a regular file", bakPath)
		}
		return nil // keep the existing original backup
	default:
		return fmt.Errorf("writeBackupOnce: %w", err)
	}
}

// generateClaudeMD renders CLAUDE.md from the static template using detected data.
func generateClaudeMD(info *codegen.ProjectInfo, lang, buildTool, testCmd string, force bool) error {
	fmt.Println("\nGenerating CLAUDE.md...")
	content, err := codegen.RenderCLAUDEMDTemplate(codegen.AgentTemplateData{
		ProjectName: info.Name,
		Language:    lang,
		BuildTool:   buildTool,
		TestCommand: testCmd,
		Structure:   topLevelDirs(info.TreeSummary),
	})
	if err != nil {
		return fmt.Errorf("generateClaudeMD: %w", err)
	}
	// Back up an existing CLAUDE.md ONLY when the overwrite will actually
	// happen (file exists AND force is set — writeAgentFile's own skip
	// condition). Write-once: a repeat --force must not clobber the first
	// backup with content that is itself generated. A failed backup
	// hard-fails the whole step so a bad --force regen never destroys a
	// hand-edited CLAUDE.md with no working backup.
	if existing, readErr := os.ReadFile("CLAUDE.md"); readErr == nil && force {
		if bakErr := writeBackupOnce("CLAUDE.md.bak", existing); bakErr != nil {
			return fmt.Errorf("generateClaudeMD: backing up CLAUDE.md: %w", bakErr)
		}
	}
	skipped, writeErr := writeAgentFile("CLAUDE.md", content, force)
	if writeErr != nil {
		return fmt.Errorf("generateClaudeMD: %w", writeErr)
	}
	if skipped {
		fmt.Println("  CLAUDE.md already exists — skipped (use --force to regenerate)")
	} else {
		fmt.Println("  Created: CLAUDE.md")
	}
	return nil
}

// applyHardening writes a hardened .claude/settings.json for the detected
// toolchain, deep-merging into any existing file (preserving user entries) and
// backing the original up to settings.json.bak. It also upserts the tu-agent
// managed block into .gitignore. pluginPresent skips the SessionStart/Stop/
// SessionEnd hooks the Claude Code plugin's own hooks.json already provides.
// Idempotent.
func applyHardening(lang, buildTool string, private, pluginPresent bool) error {
	fmt.Println("\nHardening Claude Code settings...")
	settingsPath := filepath.Join(".claude", "settings.json")

	var existing map[string]any
	raw, readErr := os.ReadFile(settingsPath)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("applyHardening: existing settings.json is not valid JSON: %w", err)
		}
		// Back up the original settings once. Use O_EXCL so a repeat run does
		// not clobber the first backup with already-hardened content.
		bak, bakErr := os.OpenFile(settingsPath+".bak", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		switch {
		case bakErr == nil:
			_, werr := bak.Write(raw)
			cerr := bak.Close()
			if werr != nil {
				return fmt.Errorf("applyHardening: writing backup: %w", werr)
			}
			if cerr != nil {
				return fmt.Errorf("applyHardening: closing backup: %w", cerr)
			}
		case errors.Is(bakErr, os.ErrExist):
			// keep the existing original backup
		default:
			return fmt.Errorf("applyHardening: backing up settings.json: %w", bakErr)
		}
	case errors.Is(readErr, os.ErrNotExist):
		existing = map[string]any{}
	default:
		return fmt.Errorf("applyHardening: reading settings.json: %w", readErr)
	}

	merged := codegen.MergeSettings(existing, codegen.HardenedSettings(lang, buildTool, pluginPresent))
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("applyHardening: marshaling settings: %w", err)
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("applyHardening: creating .claude dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return fmt.Errorf("applyHardening: writing settings.json: %w", err)
	}
	fmt.Println("  Wrote: .claude/settings.json (hardened, deny-wins)")

	// Private mode keeps tu-agent / Claude Code artifacts out of commits by writing
	// to .git/info/exclude (local per clone, never committed) instead of .gitignore
	// — so not even the ignore rules name "claude" in the repo history. The shared-
	// memory chunks dir is re-included so a team can still commit it if they choose.
	if private {
		return applyGitInfoExclude()
	}

	giPath := ".gitignore"
	giRaw, giErr := os.ReadFile(giPath)
	if giErr != nil && !errors.Is(giErr, os.ErrNotExist) {
		return fmt.Errorf("applyHardening: reading .gitignore: %w", giErr)
	}
	giMerged := codegen.MergeGitignore(string(giRaw))
	if giMerged != string(giRaw) {
		if err := os.WriteFile(giPath, []byte(giMerged), 0o644); err != nil {
			return fmt.Errorf("applyHardening: writing .gitignore: %w", err)
		}
		fmt.Println("  Updated: .gitignore (tu-agent artifacts)")
	}
	return nil
}

// applyGitInfoExclude upserts the private managed block into .git/info/exclude
// at the repo root. The file is local to each clone and never committed, so the
// ignore rules leave no trace of tu-agent/Claude in the repository's history.
// Only the standard ".git directory" layout is handled; if .git is missing or a
// worktree pointer file, it warns and skips rather than failing init.
func applyGitInfoExclude() error {
	gitDir := filepath.Join(repoRoot(), ".git")
	info, statErr := os.Stat(gitDir)
	switch {
	case statErr == nil && info.IsDir():
		// normal clone — proceed
	case statErr == nil && !info.IsDir():
		fmt.Println("  Skipped: .git/info/exclude (worktree/submodule .git file not supported — add the rules manually)")
		return nil
	case errors.Is(statErr, os.ErrNotExist):
		fmt.Println("  Skipped: .git/info/exclude (not a git repository)")
		return nil
	default:
		return fmt.Errorf("applyGitInfoExclude: stat .git: %w", statErr)
	}

	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("applyGitInfoExclude: creating .git/info: %w", err)
	}
	exPath := filepath.Join(infoDir, "exclude")
	exRaw, exErr := os.ReadFile(exPath)
	if exErr != nil && !errors.Is(exErr, os.ErrNotExist) {
		return fmt.Errorf("applyGitInfoExclude: reading exclude: %w", exErr)
	}
	merged := codegen.MergeGitInfoExclude(string(exRaw))
	if merged != string(exRaw) {
		if err := os.WriteFile(exPath, []byte(merged), 0o644); err != nil {
			return fmt.Errorf("applyGitInfoExclude: writing exclude: %w", err)
		}
		fmt.Println("  Updated: .git/info/exclude (local-only; tu-agent/Claude artifacts kept out of commits)")
	}
	return nil
}

var initCmd = &cobra.Command{
	GroupID: "setup",
	Use:     "prepare [path]",
	Aliases: []string{"init"},
	Short:   "Prepare a repository: CLAUDE.md knowledge block + hardened settings",
	Long: `Detects the project language (or takes --lang for empty/new repos) and
writes a CLAUDE.md knowledge block at the repo root, a hardened
.claude/settings.json (permissions, security/quality hooks, MCP allowlist), and
seeds .tu-agent config — all deterministically (no API calls). By default
(private) it keeps tu-agent/Claude artifacts out of commits via
.git/info/exclude; pass --public to commit a .gitignore block instead, or
--no-harden to skip hardening entirely.

Dev-flow agents are NOT materialized: the tdd flow resolves each role to an
embedded generic shell at runtime. Role knowledge comes from the graph
(get_context) plus those shells, not per-repo enrichment; override a role by
adding your own .claude/agents/<role>.md.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		subpath := ""
		if len(args) == 1 {
			subpath = args[0]
		}
		return runInitSetup(cmd.Context(), initSetupOpts{
			Subpath:       subpath,
			Lang:          initLang,
			NoLLM:         initNoLLM,
			Force:         initForce,
			Update:        initUpdate,
			Provider:      initProvider,
			NoHarden:      initNoHarden,
			Private:       resolvePrivate(initPrivate, initPublic),
			AugmentAgents: initAugmentAgents,
			Exclude:       initAugmentExclude,
			Plugin:        initPlugin,
		})
	},
}

// resolvePrivate maps the init flags to the effective private mode. Private is
// the default (safe for company/shared repos): ignore rules go to
// .git/info/exclude, never committed. --public opts into the committed .gitignore
// block; an explicit --private still wins, even alongside --public.
func resolvePrivate(private, public bool) bool {
	return private || !public
}

// writeAgentFile writes content to path, creating parent directories as needed.
// If the file already exists and force is false, it returns (true, nil) without writing.
// Returns (false, nil) on successful write.
func writeAgentFile(path, content string, force bool) (skipped bool, err error) {
	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			return true, nil
		}
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return false, fmt.Errorf("writeAgentFile: creating dir: %w", mkErr)
	}
	if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
		return false, fmt.Errorf("writeAgentFile: writing %s: %w", path, writeErr)
	}
	return false, nil
}

func init() {
	initCmd.Flags().StringVar(&initLang, "lang", "",
		"project language (go|java|python|typescript); required for empty repos, overrides detection")
	initCmd.Flags().BoolVar(&initNoLLM, "no-llm", false,
		"skip LLM calls; deterministic setup only (CLAUDE.md + hardened settings)")
	initCmd.Flags().BoolVar(&initForce, "force", false,
		"overwrite an existing CLAUDE.md")
	initCmd.Flags().BoolVar(&initUpdate, "update", false,
		"refresh the CLAUDE.md knowledge block in place")
	initCmd.Flags().StringVar(&initProvider, "provider", "",
		"provider override (claude|local)")
	initCmd.Flags().BoolVar(&initNoHarden, "no-harden", false,
		"skip generating a hardened .claude/settings.json")
	initCmd.Flags().BoolVar(&initPublic, "public", false,
		"share mode: commit a tu-agent block to .gitignore so the team sees which artifacts are ignored. Default is private (local-only): ignore rules go to .git/info/exclude, never committed, so no tu-agent/Claude artifacts reach the repo (shared-memory chunks stay committable).")
	initCmd.Flags().BoolVar(&initPrivate, "private", false,
		"(deprecated: private is now the default; use --public to opt out)")
	_ = initCmd.Flags().MarkDeprecated("private", "private is now the default; pass --public to share via .gitignore")
	initCmd.Flags().BoolVar(&initAugmentAgents, "augment-agents", false,
		"augment existing .claude/agents/*.md with graph MCP tools + protocol (additive, idempotent)")
	initCmd.Flags().StringVar(&initAugmentExclude, "exclude", "",
		"comma-separated agent names to skip with --augment-agents")
	initCmd.Flags().BoolVar(&initPlugin, "plugin", false,
		"provisioning under the Claude Code plugin: skip hooks its hooks.json already provides")
}

// parseExtensions splits a comma-separated extension string, ensures each
// starts with '.', and returns lowercase extensions with empty entries removed.
func parseExtensions(patterns string) []string {
	parts := strings.Split(patterns, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.ToLower(p)
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		result = append(result, p)
	}
	return result
}

// topLevelDirs filters a multi-level tree summary down to top-level entries
// only, stripping indented child lines. This keeps CLAUDE.md concise: the
// template needs orientation, not a full subtree map.
func topLevelDirs(tree string) string {
	var out []string
	for _, line := range strings.Split(tree, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
