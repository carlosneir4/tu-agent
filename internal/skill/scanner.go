package skill

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/frontmatter"
	"gopkg.in/yaml.v3"
)

// SearchPaths returns the ordered directories to scan for skills.
// Precedence (later entries override earlier on name collision):
//
//	~/.claude/skills < ~/.tu-agent/skills < <cwd>/.claude/skills < <cwd>/.tu-agent/skills
//
// Home-based paths are omitted when home is empty (avoids relative-path scanning).
func SearchPaths(home, cwd string) []string {
	paths := make([]string, 0, 4)
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "skills"))
		paths = append(paths, filepath.Join(home, ".tu-agent", "skills"))
	}
	paths = append(paths, filepath.Join(cwd, ".claude", "skills"))
	paths = append(paths, filepath.Join(cwd, ".tu-agent", "skills"))
	return paths
}

// Scan walks each dir in dirs looking for */SKILL.md files, parses frontmatter,
// and returns a populated Index. Files that are unreadable or have no frontmatter
// are silently skipped. Later dirs override earlier entries on name collision.
func Scan(dirs []string) (*Index, error) {
	idx := New()
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("skill.Scan: glob %s: %w", dir, err)
		}
		for _, path := range matches {
			f, err := os.Open(path)
			if err != nil {
				slog.Debug("skill.Scan: skipping unreadable file", "path", path, "err", err)
				continue
			}
			e, parseErr := parseFrontmatter(path, f)
			f.Close()
			if parseErr != nil || e.Name == "" {
				if parseErr != nil {
					slog.Debug("skill.Scan: skipping unparseable file", "path", path, "err", parseErr)
				}
				continue
			}
			idx.Add(e)
		}
	}
	return idx, nil
}

type frontmatterYAML struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers"`
}

// parseFrontmatter reads YAML frontmatter from the leading --- delimited
// block of a SKILL.md file. It uses frontmatter.SplitLoose (not the strict
// Split) because crystallized/materialized skills are written with a
// provenance preamble line before the opening "---" (see
// cmd/tu-agent/memory.go saveCrystallizedSkill and `memory materialize`):
//
//	<!-- tu-agent:crystallize source-hash=... label=... -->
//	---
//	name: ...
//	---
//
// Returns a zero Entry (Name == "") if no "---" line is found at all, if the
// closing delimiter is missing, or if the frontmatter block is empty.
func parseFrontmatter(path string, r io.Reader) (Entry, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Entry{}, fmt.Errorf("skill.parseFrontmatter: scanning %s: %w", path, err)
	}
	fmBlock, _, ok := frontmatter.SplitLoose(string(raw))
	if !ok || strings.TrimSpace(fmBlock) == "" {
		return Entry{}, nil
	}

	var fm frontmatterYAML
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return Entry{}, fmt.Errorf("skill.parseFrontmatter: %s: %w", path, err)
	}
	return Entry{
		Name:        fm.Name,
		Description: fm.Description,
		Triggers:    fm.Triggers,
		Path:        path,
	}, nil
}
