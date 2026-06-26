package skill

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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

// parseFrontmatter reads YAML frontmatter between the first pair of --- delimiters.
// Returns a zero Entry (Name == "") if no frontmatter is found or if the closing delimiter is missing.
func parseFrontmatter(path string, r io.Reader) (Entry, error) {
	scanner := bufio.NewScanner(r)
	var inFrontmatter, closed bool
	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		if !inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return Entry{}, fmt.Errorf("skill.parseFrontmatter: scanning %s: %w", path, err)
	}
	if !inFrontmatter || !closed || len(lines) == 0 {
		return Entry{}, nil
	}

	var fm frontmatterYAML
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &fm); err != nil {
		return Entry{}, fmt.Errorf("skill.parseFrontmatter: %s: %w", path, err)
	}
	return Entry{
		Name:        fm.Name,
		Description: fm.Description,
		Triggers:    fm.Triggers,
		Path:        path,
	}, nil
}
