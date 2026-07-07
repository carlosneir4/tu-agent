package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tu/tu-agent/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDefaultLoader(t *testing.T) {
	l, err := config.DefaultLoader()
	if err != nil {
		t.Fatalf("DefaultLoader() error = %v", err)
	}
	if l == nil {
		t.Fatal("DefaultLoader() returned nil")
	}
}

func TestLoader_Load(t *testing.T) {
	tests := []struct {
		name     string
		userYAML string
		projYAML string
		want     config.Config
		wantErr  bool
	}{
		{
			name: "no config files returns defaults",
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{},
			},
		},
		{
			name: "user config overrides default routing",
			userYAML: `
routing:
  default: local-self-hosted
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "local-self-hosted",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{},
			},
		},
		{
			name: "project config overrides user config",
			userYAML: `
routing:
  default: local-self-hosted
`,
			projYAML: `
routing:
  default: claude
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{},
			},
		},
		{
			name: "sub_agents merged key-by-key, project wins on conflict",
			userYAML: `
routing:
  sub_agents:
    codebase-explorer: local-self-hosted
    another-agent: claude
`,
			projYAML: `
routing:
  sub_agents:
    codebase-explorer: claude
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default: "claude",
					SubAgents: map[string]string{
						"codebase-explorer": "claude",
						"another-agent":     "claude",
					},
					Tasks: map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{},
			},
		},
		{
			name: "provider base_url set from project config is stripped (SSRF prevention)",
			projYAML: `
providers:
  local-self-hosted:
    base_url: http://192.168.1.10:1234/v1
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local-self-hosted": {BaseURL: ""},
				},
			},
		},
		{
			name:     "invalid yaml returns error",
			userYAML: "routing: [invalid",
			wantErr:  true,
		},
		{
			name: "tasks merged key-by-key, project wins on conflict",
			userYAML: `
routing:
  tasks:
    init: local-self-hosted
    chat: local-self-hosted
`,
			projYAML: `
routing:
  tasks:
    init: claude
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks: map[string]string{
						"init": "claude",
						"chat": "local-self-hosted",
					},
				},
				Providers: map[string]config.ProviderConfig{},
			},
		},
		{
			name: "providers merged additively across layers; project base_url stripped",
			userYAML: `
providers:
  provider-a:
    base_url: http://host-a:1234/v1
`,
			projYAML: `
providers:
  provider-b:
    base_url: http://host-b:5678/v1
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"provider-a": {BaseURL: "http://host-a:1234/v1"},
					"provider-b": {BaseURL: ""},
				},
			},
		},
		{
			name: "provider model set from config",
			projYAML: `
providers:
  claude:
    model: claude-sonnet-4-6
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"claude": {Model: "claude-sonnet-4-6"},
				},
			},
		},
		{
			name: "project config cannot override base_url set by user config",
			userYAML: `
providers:
  local:
    base_url: "http://localhost:1234"
    model: "qwen3"
`,
			projYAML: `
providers:
  local:
    base_url: "https://evil.example.com"
    model: "override-model"
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {BaseURL: "http://localhost:1234", Model: "override-model"},
				},
			},
		},
		{
			name: "project config cannot set base_url when user config has none",
			projYAML: `
providers:
  local:
    base_url: "https://evil.example.com"
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {BaseURL: ""},
				},
			},
		},
		{
			name: "user config context_size and max_output_tokens are preserved",
			userYAML: `
providers:
  local:
    base_url: "http://localhost:1234"
    model: "qwen3"
    context_size: 8192
    max_output_tokens: 1024
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {
						BaseURL:         "http://localhost:1234",
						Model:           "qwen3",
						ContextSize:     8192,
						MaxOutputTokens: 1024,
					},
				},
			},
		},
		{
			name: "project config can override context_size and max_output_tokens",
			userYAML: `
providers:
  local:
    base_url: "http://localhost:1234"
    context_size: 8192
    max_output_tokens: 1024
`,
			projYAML: `
providers:
  local:
    context_size: 4096
    max_output_tokens: 512
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {
						BaseURL:         "http://localhost:1234",
						ContextSize:     4096,
						MaxOutputTokens: 512,
					},
				},
			},
		},
		{
			name: "project config can override temperature",
			userYAML: `
providers:
  local:
    base_url: "http://localhost:1234"
    temperature: 0.7
`,
			projYAML: `
providers:
  local:
    temperature: 0.2
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {
						BaseURL:     "http://localhost:1234",
						Temperature: 0.2,
					},
				},
			},
		},
		{
			name: "user config max_prompt_tokens is parsed",
			userYAML: `
providers:
  local:
    base_url: http://localhost:1234
    context_size: 32768
    max_prompt_tokens: 16384
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {
						BaseURL:         "http://localhost:1234",
						ContextSize:     32768,
						MaxPromptTokens: 16384,
					},
				},
			},
		},
		{
			name: "project config can override max_prompt_tokens",
			userYAML: `
providers:
  local:
    base_url: http://localhost:1234
    context_size: 32768
    max_prompt_tokens: 16384
`,
			projYAML: `
providers:
  local:
    max_prompt_tokens: 8192
`,
			want: config.Config{
				Routing: config.RoutingConfig{
					Default:   "claude",
					SubAgents: map[string]string{},
					Tasks:     map[string]string{},
				},
				Providers: map[string]config.ProviderConfig{
					"local": {
						BaseURL:         "http://localhost:1234",
						ContextSize:     32768,
						MaxPromptTokens: 8192,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			userDir := filepath.Join(base, "tu-agent")
			projDir := filepath.Join(base, "project")

			if tt.userYAML != "" {
				writeFile(t, userDir, "config.yaml", tt.userYAML)
			}
			if tt.projYAML != "" {
				writeFile(t, projDir, "config.yaml", tt.projYAML)
			}

			l := config.NewLoader(
				filepath.Join(base, "claude"),
				userDir,
				projDir,
			)
			got, err := l.Load()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got.Routing.Default != tt.want.Routing.Default {
				t.Errorf("Routing.Default = %q, want %q", got.Routing.Default, tt.want.Routing.Default)
			}
			for k, want := range tt.want.Routing.SubAgents {
				if got := got.Routing.SubAgents[k]; got != want {
					t.Errorf("SubAgents[%q] = %q, want %q", k, got, want)
				}
			}
			for k := range got.Routing.SubAgents {
				if _, ok := tt.want.Routing.SubAgents[k]; !ok {
					t.Errorf("unexpected SubAgents key %q", k)
				}
			}
			for k, want := range tt.want.Providers {
				if got := got.Providers[k]; got != want {
					t.Errorf("Providers[%q] = %+v, want %+v", k, got, want)
				}
			}
		})
	}
}

func TestLoad_MergesMaxConcurrent(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	writeFile(t, userDir, "config.yaml", `
providers:
  local:
    base_url: http://localhost:1234/v1
    max_concurrent: 4
`)
	// Project layer without max_concurrent must not zero the user value.
	writeFile(t, projDir, "config.yaml", `
providers:
  local:
    model: override-model
`)

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Providers["local"].MaxConcurrent; got != 4 {
		t.Errorf("MaxConcurrent = %d, want 4", got)
	}
	if got := cfg.Providers["local"].Model; got != "override-model" {
		t.Errorf("Model = %q, want override-model (merge must still work)", got)
	}
}

func TestLoad_RoutingDisabled_ProjectLayer(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	// User layer doesn't set it; project layer (committed, repo-enforced) does.
	writeFile(t, userDir, "config.yaml", `
routing:
  default: claude
`)
	writeFile(t, projDir, "config.yaml", `
routing:
  disabled: true
`)

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Routing.Disabled {
		t.Fatal("Routing.Disabled = false, want true (project layer must set the kill-switch)")
	}
}

func TestLoad_RoutingDisabled_NotSetByDefault(t *testing.T) {
	base := t.TempDir()
	userDir := filepath.Join(base, "tu-agent")
	projDir := filepath.Join(base, "project")

	cfg, err := config.NewLoader(filepath.Join(base, "claude"), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Routing.Disabled {
		t.Fatal("Routing.Disabled = true by default, want false")
	}
}

func TestLoadLearnConfig(t *testing.T) {
	tests := []struct {
		name           string
		userYAML       string
		projectYAML    string
		wantStandalone int
		wantMaxFiles   int
	}{
		{
			name:           "defaults when no file sets learn",
			wantStandalone: 4,
			wantMaxFiles:   40,
		},
		{
			name:           "project layer overrides",
			projectYAML:    "learn:\n  min_standalone_files: 2\n  max_files_per_domain: 80\n",
			wantStandalone: 2,
			wantMaxFiles:   80,
		},
		{
			name:           "user layer sets, project partial-overrides",
			userYAML:       "learn:\n  min_standalone_files: 6\n  max_files_per_domain: 50\n",
			projectYAML:    "learn:\n  max_files_per_domain: 30\n",
			wantStandalone: 6,
			wantMaxFiles:   30,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userDir, projDir := t.TempDir(), t.TempDir()
			if tt.userYAML != "" {
				if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(tt.userYAML), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.projectYAML != "" {
				if err := os.WriteFile(filepath.Join(projDir, "config.yaml"), []byte(tt.projectYAML), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := config.NewLoader(t.TempDir(), userDir, projDir).Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Learn.MinStandaloneFiles != tt.wantStandalone {
				t.Errorf("MinStandaloneFiles = %d, want %d", cfg.Learn.MinStandaloneFiles, tt.wantStandalone)
			}
			if cfg.Learn.MaxFilesPerDomain != tt.wantMaxFiles {
				t.Errorf("MaxFilesPerDomain = %d, want %d", cfg.Learn.MaxFilesPerDomain, tt.wantMaxFiles)
			}
		})
	}
}

func TestLoadLearnConceptRoot(t *testing.T) {
	userDir, projDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "config.yaml"),
		[]byte("learn:\n  concept_root: com.acme.shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewLoader(t.TempDir(), userDir, projDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Learn.ConceptRoot != "com.acme.shop" {
		t.Errorf("ConceptRoot = %q, want com.acme.shop", cfg.Learn.ConceptRoot)
	}

	// absent key defaults to ""
	cfg2, err := config.NewLoader(t.TempDir(), t.TempDir(), t.TempDir()).Load()
	if err != nil {
		t.Fatalf("Load (empty): %v", err)
	}
	if cfg2.Learn.ConceptRoot != "" {
		t.Errorf("ConceptRoot (absent) = %q, want empty", cfg2.Learn.ConceptRoot)
	}
}
