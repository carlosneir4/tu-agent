package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/config"
	"github.com/tu/tu-agent/internal/memory"
	"github.com/tu/tu-agent/internal/skill"
)

func TestResolveProviderName(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.Config
		task     string
		override string
		want     string
	}{
		{
			name:     "override beats everything",
			cfg:      config.Config{Routing: config.RoutingConfig{Default: "claude", Tasks: map[string]string{"chat": "claude"}}},
			task:     "chat",
			override: "local",
			want:     "local",
		},
		{
			name: "task-specific routing wins over default",
			cfg: config.Config{Routing: config.RoutingConfig{
				Default: "claude",
				Tasks:   map[string]string{"chat": "local"},
			}},
			task: "chat",
			want: "local",
		},
		{
			name: "default applies when task not mapped",
			cfg: config.Config{Routing: config.RoutingConfig{
				Default: "local",
				Tasks:   map[string]string{"init": "claude"},
			}},
			task: "chat",
			want: "local",
		},
		{
			name: "empty task value falls through to default",
			cfg: config.Config{Routing: config.RoutingConfig{
				Default: "local",
				Tasks:   map[string]string{"chat": ""},
			}},
			task: "chat",
			want: "local",
		},
		{
			name: "claude is the ultimate fallback",
			cfg:  config.Config{},
			task: "chat",
			want: "claude",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveProviderName(tc.cfg, tc.task, tc.override)
			if got != tc.want {
				t.Errorf("resolveProviderName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSelectProvider_UnknownProvider(t *testing.T) {
	cfg := config.Config{Routing: config.RoutingConfig{Default: "gemini"}}
	_, err := selectProvider(cfg, "chat", "")
	if err == nil {
		t.Fatal("selectProvider() should error on unknown provider, got nil")
	}
}

func TestSelectProvider_ClaudeMissingAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg := config.Config{}
	_, err := selectProvider(cfg, "chat", "claude")
	if err == nil {
		t.Fatal("selectProvider() should error when ANTHROPIC_API_KEY is unset")
	}
}

func TestSelectProvider_LocalUsesDefaultBaseURL(t *testing.T) {
	t.Setenv("LOCAL_API_KEY", "lm-studio")
	cfg := config.Config{
		Providers: map[string]config.ProviderConfig{
			"local": {},
		},
	}
	p, err := selectProvider(cfg, "chat", "local")
	if err != nil {
		t.Fatalf("selectProvider() error = %v", err)
	}
	if p.Name() != "local" {
		t.Errorf("provider.Name() = %q, want local", p.Name())
	}
	if p.Model() != "local" {
		t.Errorf("default Model() = %q, want 'local'", p.Model())
	}
}

func TestBuildSystemPrompt_EmptyIndex(t *testing.T) {
	idx := skill.New()
	got := buildSystemPrompt(idx)
	if got != baseSystemPrompt {
		t.Errorf("expected base prompt unchanged for empty index\ngot:  %q\nwant: %q", got, baseSystemPrompt)
	}
}

func TestBuildSystemPrompt_WithSkills(t *testing.T) {
	idx := skill.New()
	idx.Add(skill.Entry{Name: "go-conventions", Description: "Go coding standards"})
	got := buildSystemPrompt(idx)
	if !strings.HasPrefix(got, baseSystemPrompt) {
		t.Error("expected base prompt as prefix")
	}
	if !strings.Contains(got, "## Available Skills") {
		t.Error("expected '## Available Skills' heading")
	}
	if !strings.Contains(got, "go-conventions") {
		t.Error("expected skill name in prompt")
	}
}

func TestAppendMemorySection_Empty(t *testing.T) {
	got := appendMemorySection("base prompt", nil)
	if got != "base prompt" {
		t.Errorf("expected unchanged prompt for nil obs, got %q", got)
	}
}

func TestAppendMemorySection_WithObs(t *testing.T) {
	obs := []memory.Observation{
		{Title: "auth", Content: "JWT tokens expire in 1h", UpdatedAt: time.Now()},
		{Title: "db", Content: "pool size 10", UpdatedAt: time.Now()},
	}
	got := appendMemorySection("base prompt", obs)
	if !strings.HasPrefix(got, "base prompt") {
		t.Error("expected base prompt as prefix")
	}
	if !strings.Contains(got, "## Recent Memory") {
		t.Error("expected '## Recent Memory' heading")
	}
	if !strings.Contains(got, "auth") {
		t.Error("expected topic 'auth' in memory section")
	}
	if !strings.Contains(got, "JWT tokens expire in 1h") {
		t.Error("expected content in memory section")
	}
}

func TestRunCmdRequiresTask(t *testing.T) {
	// run without --task must return an error
	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --task is missing, got nil")
	}
}

func TestBenchCmdRequiresFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"bench"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --baseline/--compare missing, got nil")
	}
}

func TestLoadPromptSuffix_TaskAndProvider(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".tu-agent", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "run-local.md"), []byte("  use grep not read  "), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadPromptSuffix(dir, "run", "local")
	if got != "use grep not read" {
		t.Errorf("loadPromptSuffix() = %q, want %q", got, "use grep not read")
	}
}

func TestLoadPromptSuffix_FallbackToProvider(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".tu-agent", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "local.md"), []byte("local generic hint"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadPromptSuffix(dir, "run", "local")
	if got != "local generic hint" {
		t.Errorf("loadPromptSuffix() = %q, want %q", got, "local generic hint")
	}
}

func TestLoadPromptSuffix_NeitherExists(t *testing.T) {
	dir := t.TempDir()
	got := loadPromptSuffix(dir, "run", "local")
	if got != "" {
		t.Errorf("loadPromptSuffix() = %q, want empty string", got)
	}
}

func TestLoadPromptSuffix_TaskFileBeatsProviderFile(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".tu-agent", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "run-local.md"), []byte("task specific"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "local.md"), []byte("provider generic"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadPromptSuffix(dir, "run", "local")
	if got != "task specific" {
		t.Errorf("loadPromptSuffix() = %q, want %q", got, "task specific")
	}
}
