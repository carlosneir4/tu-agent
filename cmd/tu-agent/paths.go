package main

import (
	"os"
	"path/filepath"
)

// repoRoot walks up from the current working directory and returns the nearest
// ancestor that contains a .git entry — the repo root. A .git entry may be a
// directory (normal clone) or a file (worktree / submodule), so it is detected
// with os.Stat without an IsDir check. When no ancestor contains .git (not
// inside a git repo) or the working directory cannot be determined, it returns
// "." so behavior matches the historical CWD-relative store resolution.
//
// Anchoring on .git — not on an existing .tu-agent — is deliberate: a stray
// nested .tu-agent is exactly the failure mode this fixes, and anchoring on it
// would let an orphan become a false root.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// generatedSkillsDir returns the directory where tu-agent writes generated
// project-knowledge skills, relative to repo root. Per the .claude artifact
// policy this is <root>/.claude/skills.
func generatedSkillsDir(root string) string {
	return filepath.Join(root, ".claude", "skills")
}

// tuAgentDir returns the project-local tu-agent data directory under root.
// Every helper below builds on it, so the project layout has a single
// definition here instead of a ".tu-agent" literal per call site.
func tuAgentDir(root string) string {
	return filepath.Join(root, ".tu-agent")
}

// memoryDBPath returns the project-local memory database path under root.
// Memory is durable (unlike graph.db) — see internal/memory. It lives under a
// per-subsystem memory/ subdir; memory.Open MkdirAll's the parent, so no caller
// needs to create it.
func memoryDBPath(root string) string {
	return filepath.Join(tuAgentDir(root), "memory", "memory.db")
}

// memoryChunksDir returns the directory holding committed memory chunk files.
// Unlike memory.db (gitignored), this directory is versioned so the team shares
// observations through git.
//
// The repo-relative twin of this path lives in internal/memory (RelChunkPath):
// that package cannot import cmd, so the two must be changed together.
func memoryChunksDir(root string) string {
	return filepath.Join(tuAgentDir(root), "share", "memory", "chunks")
}

// telemetryPath returns the project-local telemetry log path under root. It
// lives under a per-subsystem logs/ subdir; telemetry.NewLogger's Log MkdirAll's
// the parent on first write, so no caller needs to create it.
//
// Callers pass the root they already resolve today: generated-artifact writers
// and the live stats command pass repoRoot(), while the frozen standalone
// commands (chat, run) pass "." and stay CWD-relative — filepath.Join drops the
// leading "." so their result is byte-identical to the
// ".tu-agent/logs/telemetry.jsonl" path they resolve. That CWD-relative
// anchoring is pre-existing behavior in those two frozen commands, not a choice
// made here.
func telemetryPath(root string) string {
	return filepath.Join(tuAgentDir(root), "logs", "telemetry.jsonl")
}

// promptsDir returns the directory holding per-provider prompt suffix files for
// the frozen standalone chat command. dir is the directory being probed, which
// chat walks up from the working directory — not necessarily the repo root.
func promptsDir(dir string) string {
	return filepath.Join(tuAgentDir(dir), "prompts")
}
