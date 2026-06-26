package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var setupDefaults = struct {
	Model       string
	ContextSize int
	Temperature float64
}{
	Model:       "qwen/qwen3-coder-30b",
	ContextSize: 16384,
	Temperature: 0.2,
}

// promptString prints a prompt with an optional default in brackets and reads
// one line from in. Returns the default if the user presses Enter with no input.
func promptString(in *bufio.Reader, out io.Writer, prompt, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("promptString: %w", err)
	}
	v := strings.TrimSpace(line)
	if v == "" {
		return def, nil
	}
	return v, nil
}

// runSetup drives the interactive setup, reading from in and writing prompts
// to out. cfgPath is the target file (normally ~/.tu-agent/config.yaml).
func runSetup(in io.Reader, out io.Writer, cfgPath string) error {
	r := bufio.NewReader(in)

	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(out, "Config already exists at %s.\nOverwrite? [y/N]: ", cfgPath)
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("setup: reading confirmation: %w", err)
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("setup aborted — existing config kept")
		}
	}

	fmt.Fprintf(out, "\ntu-agent setup — press Enter to accept defaults shown in [brackets]\n\n")

	baseURL, err := promptString(r, out, "Model endpoint base_url (e.g. http://192.168.1.31:1234)", "")
	if err != nil {
		return err
	}
	if baseURL == "" {
		return fmt.Errorf("base_url is required")
	}

	model, err := promptString(r, out, "Model name", setupDefaults.Model)
	if err != nil {
		return err
	}

	ctxStr, err := promptString(r, out, "Context size (tokens)", strconv.Itoa(setupDefaults.ContextSize))
	if err != nil {
		return err
	}
	contextSize, err := strconv.Atoi(ctxStr)
	if err != nil || contextSize <= 0 {
		return fmt.Errorf("invalid context_size %q: must be a positive integer", ctxStr)
	}

	tempStr, err := promptString(r, out, "Temperature (0.0-1.0, 0 = server default)", strconv.FormatFloat(setupDefaults.Temperature, 'f', 1, 64))
	if err != nil {
		return err
	}
	temperature, err := strconv.ParseFloat(tempStr, 64)
	if err != nil || temperature < 0 || temperature > 1 {
		return fmt.Errorf("invalid temperature %q: must be between 0.0 and 1.0", tempStr)
	}

	cfg := map[string]interface{}{
		"routing": map[string]interface{}{
			"default": "local",
			"tasks": map[string]interface{}{
				"chat": "local",
				"init": "local",
				"run":  "local",
			},
		},
		"providers": map[string]interface{}{
			"local": map[string]interface{}{
				"base_url":                baseURL,
				"model":                   model,
				"context_size":            contextSize,
				"max_output_tokens":       4096,
				"temperature":             temperature,
				"request_timeout_seconds": 600,
			},
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("setup: marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("setup: create config dir: %w", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return fmt.Errorf("setup: write config: %w", err)
	}

	fmt.Fprintf(out, "\nConfig written to %s\n", cfgPath)
	return nil
}

// runSetupHooks merges the PostToolUse graph-freshness hook into the repo's
// ./.claude/settings.json, preserving any existing settings and hooks.
func runSetupHooks(repoRoot string, out io.Writer) error {
	path := filepath.Join(repoRoot, ".claude", "settings.json")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("setup --hooks: reading %s: %w", path, err)
	}
	merged, changed, err := mergePostToolUseHook(existing)
	if err != nil {
		return fmt.Errorf("setup --hooks: merge hook: %w", err)
	}
	if !changed {
		fmt.Fprintf(out, "PostToolUse hook already installed in %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("setup --hooks: create .claude dir: %w", err)
	}
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		return fmt.Errorf("setup --hooks: write %s: %w", path, err)
	}
	fmt.Fprintf(out, "Installed PostToolUse hook (graph auto-update) into %s\n", path)
	return nil
}

var setupHooks bool

func init() {
	setupCmd.Flags().BoolVar(&setupHooks, "hooks", false,
		"skip the config wizard; install the PostToolUse graph-update hook into ./.claude/settings.json")
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create ~/.tu-agent/config.yaml interactively",
	Long: `Walks you through creating the global tu-agent config file.
Run this once per machine after installing tu-agent.`,
	Args: cobra.NoArgs,
	// PersistentPreRunE overrides the root hook to skip config loading —
	// the config file may not exist yet when the user runs setup.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	RunE: func(cmd *cobra.Command, args []string) error {
		if setupHooks {
			return runSetupHooks(".", os.Stdout)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("setup: resolving home dir: %w", err)
		}
		return runSetup(os.Stdin, os.Stdout, filepath.Join(home, ".tu-agent", "config.yaml"))
	},
}
