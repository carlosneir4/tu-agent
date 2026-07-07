package config

// Config is the merged tu-agent configuration for a session.
// Layer precedence (later wins): ~/.tu-agent/config.yaml < ./.tu-agent/config.yaml
type Config struct {
	Routing   RoutingConfig             `yaml:"routing"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Learn     LearnConfig               `yaml:"learn"`
	Tdd       TddConfig                 `yaml:"tdd"`
}

// RoutingConfig controls which provider handles each task or sub-agent.
type RoutingConfig struct {
	Default   string            `yaml:"default"`
	SubAgents map[string]string `yaml:"sub_agents"`
	Tasks     map[string]string `yaml:"tasks"`
	// Disabled hard-blocks every provider call regardless of environment keys.
	// Set it in the PROJECT config (./.tu-agent/config.yaml, committed) to
	// guarantee a repo never talks to an external model through the binary;
	// deterministic commands are unaffected.
	Disabled bool `yaml:"disabled"`
}

// ProviderConfig holds endpoint configuration for a named provider.
// API keys are never stored here — read from environment variables at call time.
type ProviderConfig struct {
	BaseURL               string  `yaml:"base_url"`
	Model                 string  `yaml:"model"`                   // optional; provider default used if empty
	ContextSize           int     `yaml:"context_size"`            // token context window; 0 means unlimited/API default
	MaxOutputTokens       int     `yaml:"max_output_tokens"`       // max tokens to generate; 0 = let provider decide
	RequestTimeoutSeconds int     `yaml:"request_timeout_seconds"` // per-HTTP-call timeout; 0 = adapter default (120s)
	MaxPromptTokens       int     `yaml:"max_prompt_tokens"`       // cap on prompt tokens per request; 0 = use full context window
	Temperature           float64 `yaml:"temperature"`             // sampling temperature; 0 = unset (server default). Cannot request exactly 0.0; use 0.01 for near-greedy.
	MaxConcurrent         int     `yaml:"max_concurrent"`          // max model requests in flight during learn; 0 = 1 (sequential)
}

// LearnConfig tunes the learn pipeline's domain clustering guardrails.
type LearnConfig struct {
	MinStandaloneFiles int      `yaml:"min_standalone_files"` // packages with >= this many non-test files never merge into a consumer domain
	MaxFilesPerDomain  int      `yaml:"max_files_per_domain"` // domains above this split into sub-domains under a parent skill
	ConceptRoot        string   `yaml:"concept_root"`         // single root (kept for back-compat)
	ConceptRoots       []string `yaml:"concept_roots"`        // multiple roots; unioned with ConceptRoot
}

// TddConfig tunes the tdd dev-flow.
type TddConfig struct {
	// TestCommand is the shell command the deterministic gate runs to check the
	// suite. Empty means: auto-detect a Go repo (`go test ./...`), else error.
	TestCommand string `yaml:"test_command"`
	// Mutation opts into the mutation gate (FASE 2c). Default false.
	Mutation bool `yaml:"mutation"`
	// MutationThreshold is the kill-ratio floor (0..1); 0 means use the default (0.7).
	MutationThreshold float64 `yaml:"mutation_threshold"`
	// Archive enables the scribe stage (FASE 3) on standard-path success.
	// Pointer so absent => on; set `archive: false` to opt out.
	Archive *bool `yaml:"archive"`
	// Strict runs the sandwich one @s at a time (test->red->impl->green->next)
	// instead of batching all of a sub-feature's tests. Default false.
	Strict bool `yaml:"strict"`
}

func defaultConfig() Config {
	return Config{
		Routing: RoutingConfig{
			Default:   "claude",
			SubAgents: make(map[string]string),
			Tasks:     make(map[string]string),
		},
		Providers: make(map[string]ProviderConfig),
		Learn: LearnConfig{
			MinStandaloneFiles: 4,
			MaxFilesPerDomain:  40,
		},
	}
}
