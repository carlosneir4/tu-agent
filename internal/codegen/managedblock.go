package codegen

import (
	"strings"
)

const (
	gitignoreOpen  = "# >>> tu-agent >>>"
	gitignoreClose = "# <<< tu-agent <<<"

	gitExcludeOpen  = "# >>> tu-agent (private) >>>"
	gitExcludeClose = "# <<< tu-agent (private) <<<"
)

// GitInfoExcludeBlock returns the managed block for .git/info/exclude used by
// private mode: it keeps ALL tu-agent / Claude Code artifacts out of commits
// without naming them in a committed file. .git/info/exclude is local per clone
// and never committed, so the ignore rules themselves leave no trace in history.
func GitInfoExcludeBlock() string {
	return gitExcludeOpen + "\n" +
		"# Local-only excludes (this file is never committed). Same syntax as .gitignore.\n" +
		".claude/\n" +
		"CLAUDE.md\n" +
		".mcp.json\n" +
		"AGENTS.md\n" +
		"# Assistant-authored planning docs (specs/plans) — never committed in private repos.\n" +
		"docs/superpowers/\n" +
		"# Everything under .tu-agent stays local EXCEPT the shared-memory chunks,\n" +
		"# re-included (step by step, since git won't re-include under an excluded dir)\n" +
		"# so a team can still commit them. graph.db / memory.db / telemetry stay local.\n" +
		".tu-agent/*\n" +
		"!.tu-agent/memory/\n" +
		".tu-agent/memory/*\n" +
		"!.tu-agent/memory/chunks/\n" +
		gitExcludeClose
}

// MergeGitInfoExclude upserts the private managed block into existing
// .git/info/exclude content. Idempotent. Pure: no I/O.
func MergeGitInfoExclude(existing string) string {
	return mergeManagedBlock(existing, GitInfoExcludeBlock(), gitExcludeOpen, gitExcludeClose)
}

// GitignoreBlock returns the managed block listing tu-agent's derived artifacts.
func GitignoreBlock() string {
	return gitignoreOpen + "\n" +
		".tu-agent/graph.db\n" +
		".tu-agent/graph.db-wal\n" +
		".tu-agent/graph.db-shm\n" +
		".tu-agent/telemetry.jsonl\n" +
		".tu-agent/memory.db\n" +
		".tu-agent/memory.db-wal\n" +
		".tu-agent/memory.db-shm\n" +
		".tu-agent/graph.build.lock\n" +
		".claude/settings.json.bak\n" +
		"# .tu-agent/memory/chunks/ is intentionally versioned (shared team memory)\n" +
		gitignoreClose
}

// MergeGitignore upserts the tu-agent managed block into existing .gitignore
// content: replaced in place if the markers are present, appended otherwise.
// Idempotent. Pure: no I/O.
func MergeGitignore(existing string) string {
	return mergeManagedBlock(existing, GitignoreBlock(), gitignoreOpen, gitignoreClose)
}

// FindMarkedRegion returns the byte range [start,end) of the first open..close
// marked region in s, and whether one was found (open present and close after it).
// Pure: shared marker-scanning core for the managed-block wrappers.
func FindMarkedRegion(s, open, close string) (start, end int, found bool) {
	start = strings.Index(s, open)
	em := strings.Index(s, close)
	if start >= 0 && em > start {
		return start, em + len(close), true
	}
	return 0, 0, false
}

// mergeManagedBlock upserts a marker-delimited block into existing content:
// replaced in place if the markers are present, appended otherwise. Idempotent.
// Pure: no I/O. Shared by MergeGitignore and MergeGitInfoExclude.
func mergeManagedBlock(existing, block, open, close string) string {
	if start, end, found := FindMarkedRegion(existing, open, close); found {
		result := existing[:start] + block + existing[end:]
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result
	}
	sep := ""
	if existing != "" {
		if !strings.HasSuffix(existing, "\n") {
			sep = "\n"
		}
		sep += "\n"
	}
	return existing + sep + block + "\n"
}
