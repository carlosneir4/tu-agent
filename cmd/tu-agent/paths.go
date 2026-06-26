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

// memoryDBPath returns the project-local memory database path under root.
// Memory is durable (unlike graph.db) — see internal/memory.
func memoryDBPath(root string) string {
	return filepath.Join(root, ".tu-agent", "memory.db")
}

// memoryChunksDir returns the directory holding committed memory chunk files.
// Unlike memory.db (gitignored), this directory is versioned so the team shares
// observations through git.
func memoryChunksDir(root string) string {
	return filepath.Join(root, ".tu-agent", "memory", "chunks")
}
