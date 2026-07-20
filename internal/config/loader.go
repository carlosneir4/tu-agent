package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Loader reads and merges config from the three standard layers.
type Loader struct {
	claudeDir  string // ~/.claude — reserved for future Claude Code skill/agent discovery (not yet read by Load)
	userDir    string // ~/.tu-agent
	projectDir string // ./.tu-agent
}

// NewLoader creates a Loader with explicit directory paths (useful in tests).
func NewLoader(claudeDir, userDir, projectDir string) *Loader {
	return &Loader{
		claudeDir:  claudeDir,
		userDir:    userDir,
		projectDir: projectDir,
	}
}

// DefaultLoader creates a Loader using standard OS paths.
func DefaultLoader() (*Loader, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config.DefaultLoader: resolving home dir: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("config.DefaultLoader: resolving working dir: %w", err)
	}
	return &Loader{
		claudeDir:  filepath.Join(home, ".claude"),
		userDir:    filepath.Join(home, ".tu-agent"),
		projectDir: filepath.Join(cwd, ".tu-agent"),
	}, nil
}

// Load merges config layers and returns the resolved Config.
// Missing config files are silently skipped; parse errors are fatal.
func (l *Loader) Load() (Config, error) {
	cfg := defaultConfig()
	if err := mergeFromFile(&cfg, filepath.Join(l.userDir, "config.yaml"), true); err != nil {
		return Config{}, fmt.Errorf("config.Load: user config: %w", err)
	}
	// Project-local config may not override provider base URLs.
	// This prevents a malicious project config from redirecting API calls to
	// an attacker-controlled host, exfiltrating API keys and prompt content.
	if err := mergeFromFile(&cfg, filepath.Join(l.projectDir, "config.yaml"), false); err != nil {
		return Config{}, fmt.Errorf("config.Load: project config: %w", err)
	}
	return cfg, nil
}

func mergeFromFile(dst *Config, path string, allowBaseURL bool) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var overlay Config
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if !allowBaseURL {
		for k, v := range overlay.Providers {
			v.BaseURL = ""
			overlay.Providers[k] = v
		}
		// Project-local config may not set tdd.test_command unless it already
		// matches the user-layer value. This prevents a malicious project
		// config from executing arbitrary shell via sh -c (internal/tdd
		// runs Tdd.TestCommand as the deterministic gate's test runner).
		if overlay.Tdd.TestCommand != "" && overlay.Tdd.TestCommand != dst.Tdd.TestCommand {
			fmt.Fprintf(os.Stderr, "warning: ignoring untrusted tdd.test_command %q from %s; to trust it, copy the exact command into ~/.tu-agent/config.yaml under tdd.test_command\n", overlay.Tdd.TestCommand, path)
			overlay.Tdd.TestCommand = ""
		}
		// Project-local config may not enable tdd.auto_fix_review on its own:
		// only the trusted user layer may turn the auto-fixer on. The flag
		// gates whether review findings reach a human before the review-fixer
		// touches code (internal/tdd/review.go); a committed repo config
		// enabling it would let anyone opening the project silently re-enable
		// auto-fix and defeat that human gate. Same trust boundary as the
		// BaseURL and test_command strips above — silently cleared here (no
		// stderr warning, unlike test_command: there is no legitimate
		// migration path to preserve, since the user layer is always free to
		// set it directly).
		overlay.Tdd.AutoFixReview = false
	}
	mergeInto(dst, overlay)
	return nil
}

func mergeInto(dst *Config, src Config) {
	if src.Routing.Default != "" {
		dst.Routing.Default = src.Routing.Default
	}
	if src.Routing.Disabled {
		dst.Routing.Disabled = true
	}
	for k, v := range src.Routing.SubAgents {
		dst.Routing.SubAgents[k] = v
	}
	for k, v := range src.Routing.Tasks {
		dst.Routing.Tasks[k] = v
	}
	for k, v := range src.Providers {
		existing := dst.Providers[k]
		if v.BaseURL != "" {
			existing.BaseURL = v.BaseURL
		}
		if v.Model != "" {
			existing.Model = v.Model
		}
		if v.ContextSize != 0 {
			existing.ContextSize = v.ContextSize
		}
		if v.MaxOutputTokens != 0 {
			existing.MaxOutputTokens = v.MaxOutputTokens
		}
		if v.RequestTimeoutSeconds != 0 {
			existing.RequestTimeoutSeconds = v.RequestTimeoutSeconds
		}
		if v.MaxPromptTokens != 0 {
			existing.MaxPromptTokens = v.MaxPromptTokens
		}
		if v.Temperature != 0 {
			existing.Temperature = v.Temperature
		}
		if v.MaxConcurrent != 0 {
			existing.MaxConcurrent = v.MaxConcurrent
		}
		dst.Providers[k] = existing
	}
	if src.Learn.MinStandaloneFiles != 0 {
		dst.Learn.MinStandaloneFiles = src.Learn.MinStandaloneFiles
	}
	if src.Learn.MaxFilesPerDomain != 0 {
		dst.Learn.MaxFilesPerDomain = src.Learn.MaxFilesPerDomain
	}
	if src.Learn.ConceptRoot != "" {
		dst.Learn.ConceptRoot = src.Learn.ConceptRoot
	}
	if len(src.Learn.ConceptRoots) > 0 {
		dst.Learn.ConceptRoots = src.Learn.ConceptRoots
	}
	if src.Tdd.TestCommand != "" {
		dst.Tdd.TestCommand = src.Tdd.TestCommand
	}
	if src.Tdd.Language != "" {
		dst.Tdd.Language = src.Tdd.Language
	}
	if len(src.Tdd.BuildTags) > 0 {
		dst.Tdd.BuildTags = src.Tdd.BuildTags
	}
	if src.Tdd.Mutation {
		dst.Tdd.Mutation = true
	}
	if src.Tdd.MutationThreshold != 0 {
		dst.Tdd.MutationThreshold = src.Tdd.MutationThreshold
	}
	if src.Tdd.Archive != nil {
		dst.Tdd.Archive = src.Tdd.Archive
	}
	if src.Tdd.Strict {
		dst.Tdd.Strict = true
	}
	if src.Tdd.AutoFixReview {
		dst.Tdd.AutoFixReview = true
	}
	if src.Telemetry.Level != "" {
		dst.Telemetry.Level = src.Telemetry.Level
	}
}
