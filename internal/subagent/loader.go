package subagent

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SearchPaths returns the standard directories to scan for sub-agent definitions.
// Precedence (later entries override earlier on name collision):
//
//	~/.claude/agents < ~/.tu-agent/sub-agents < <cwd>/.claude/agents
//
// Home-based paths are omitted when home is empty.
// CWD-based paths are omitted when cwd is empty.
func SearchPaths(home, cwd string) []string {
	paths := make([]string, 0, 3)
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "agents"))
		paths = append(paths, filepath.Join(home, ".tu-agent", "sub-agents"))
	}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".claude", "agents"))
	}
	return paths
}

// projectAgentSafeTools is the tool subset applied to agents loaded from
// project-local directories. Excludes bash and write_file to prevent a
// malicious agent file from gaining code-execution capability.
var projectAgentSafeTools = []string{
	"read_file", "grep", "find", "list_dir",
	"load_skill", "mem_recent", "mem_search", "mem_save",
}

// Load reads all *.md files in dirs and returns parsed Definitions.
// readOnlyDirs maps cleaned directory paths to true; agents from those
// directories have their tool_subset replaced with projectAgentSafeTools.
// Files without a name field are silently skipped.
// Later dirs override earlier definitions on name collision.
// Non-existent directories are silently skipped.
func Load(dirs []string, readOnlyDirs map[string]bool) ([]*Definition, error) {
	byName := make(map[string]*Definition)
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			return nil, fmt.Errorf("subagent.Load: glob %s: %w", dir, err)
		}
		for _, path := range matches {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			def, parseErr := parseFrontmatter(path, f)
			f.Close()
			if parseErr != nil || def.Name == "" {
				continue
			}
			if readOnlyDirs[filepath.Clean(dir)] {
				def.ToolSubset = projectAgentSafeTools
			}
			byName[def.Name] = def
		}
	}
	result := make([]*Definition, 0, len(byName))
	for _, d := range byName {
		result = append(result, d)
	}
	return result, nil
}

type agentFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	DefaultModel string   `yaml:"default_model"`
	ToolSubset   []string `yaml:"tool_subset"`
}

// parseFrontmatter reads YAML frontmatter and body from a sub-agent markdown file.
// Files with no opening --- delimiter return a *Definition with empty Name.
func parseFrontmatter(path string, r io.Reader) (*Definition, error) {
	scanner := bufio.NewScanner(r)
	var inFrontmatter bool
	var foundOpening bool
	var fmLines, bodyLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if !foundOpening {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				foundOpening = true
				continue
			}
			return &Definition{}, nil
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("subagent: scanning %s: %w", path, err)
	}
	if len(fmLines) == 0 {
		return &Definition{}, nil
	}

	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm); err != nil {
		return nil, fmt.Errorf("subagent: parsing frontmatter in %s: %w", path, err)
	}
	return &Definition{
		Name:         fm.Name,
		Description:  fm.Description,
		DefaultModel: fm.DefaultModel,
		ToolSubset:   fm.ToolSubset,
		SystemPrompt: strings.TrimSpace(strings.Join(bodyLines, "\n")),
	}, nil
}
