package codegen

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is one entry in the skill index loaded from a skills directory
// (generated skills live in .claude/skills/).
type Skill struct {
	Name        string
	Description string
	Dir         string // absolute path to the skill directory
	Body        string // post-frontmatter content of SKILL.md
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// LoadIndex walks dir for subdirectories that contain a SKILL.md, parses
// their YAML frontmatter, and returns the resulting Skill list sorted by
// Name. Subdirectories without a SKILL.md are silently skipped; a SKILL.md
// with malformed frontmatter is skipped with a warning so one bad file does
// not take down the whole index. Returns an error only if dir or a SKILL.md
// is unreadable.
func LoadIndex(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("codegen.LoadIndex: reading %s: %w", dir, err)
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("codegen.LoadIndex: reading %s: %w", skillPath, err)
		}
		fm, body, err := splitFrontmatter(string(raw))
		if err != nil {
			slog.Warn("codegen.LoadIndex: skipping skill with malformed frontmatter",
				"path", skillPath, "err", err)
			continue
		}
		var meta skillFrontmatter
		if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
			slog.Warn("codegen.LoadIndex: skipping skill with malformed frontmatter",
				"path", skillPath, "err", err)
			continue
		}
		dirAbs, err := filepath.Abs(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("codegen.LoadIndex: resolving %s: %w", e.Name(), err)
		}
		skills = append(skills, Skill{
			Name:        meta.Name,
			Description: meta.Description,
			Dir:         dirAbs,
			Body:        body,
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// ParseSkillContent parses a SKILL.md document held in memory (e.g. a card
// stored in the graph) into a Skill. Dir is left empty — the content has no
// on-disk location. Mirrors the frontmatter parsing LoadIndex does for files.
func ParseSkillContent(content string) (Skill, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return Skill{}, fmt.Errorf("codegen.ParseSkillContent: %w", err)
	}
	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return Skill{}, fmt.Errorf("codegen.ParseSkillContent: frontmatter: %w", err)
	}
	return Skill{Name: meta.Name, Description: meta.Description, Body: body}, nil
}

// splitFrontmatter returns the YAML block between two "---" lines and the
// remaining body. Both leading and trailing whitespace are stripped from
// the frontmatter. Returns an error if the file does not begin with "---".
func splitFrontmatter(s string) (frontmatter, body string, err error) {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", "", fmt.Errorf("expected leading '---' frontmatter delimiter")
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(s, "---\n"), "---\r\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		end = strings.Index(rest, "\n---\r\n")
		if end < 0 {
			return "", "", fmt.Errorf("expected closing '---' frontmatter delimiter")
		}
	}
	fm := strings.TrimRight(rest[:end], "\r")
	body = rest[end:]
	body = strings.TrimPrefix(body, "\n---\n")
	body = strings.TrimPrefix(body, "\n---\r\n")
	return fm, body, nil
}
