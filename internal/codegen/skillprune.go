package codegen

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// ListEmptySkillDirs returns the sorted names of immediate subdirectories of
// dir that contain no SKILL.md. These are invalid skill directories — usually
// orphans left by an interrupted or failed generation run. A missing dir is
// not an error: it returns (nil, nil).
func ListEmptySkillDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("codegen.ListEmptySkillDirs: reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); statErr == nil {
			continue // valid skill dir
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// PruneEmptySkillDirs removes the genuinely empty subdirectories among those
// ListEmptySkillDirs reports, returning the sorted names it removed. os.Remove
// succeeds only on an empty directory, so a directory that lacks a SKILL.md but
// still holds other files is preserved and surfaced via a slog warning rather
// than deleted — the function can never destroy unexpected content.
func PruneEmptySkillDirs(dir string) ([]string, error) {
	candidates, err := ListEmptySkillDirs(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, name := range candidates {
		sub := filepath.Join(dir, name)
		if rmErr := os.Remove(sub); rmErr != nil {
			slog.Warn("codegen.PruneEmptySkillDirs: skill dir has no SKILL.md but is not empty; left in place",
				"path", sub, "err", rmErr)
			continue
		}
		removed = append(removed, name)
	}
	return removed, nil
}
