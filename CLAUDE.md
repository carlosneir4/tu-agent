# tu-agent

# CLAUDE.md — Instructions for Claude Code working on this repo

This file is loaded automatically by Claude Code at session start. It defines how you should behave while working on the `tu-agent` codebase.

---

## 1. First thing every session

If the user starts a session with a request that depends on context you do not have, ask which area or milestone it relates to before coding.

---

## 2. What this project is

A Go CLI named `tu-agent` — a multi-provider, multi-agent coding harness with persistent memory. Phase 0 (the core harness) is complete; active work covers memory evolution, graph intelligence, and developer experience.

If a request seems to expand the scope, ask Carlos whether the scope is changing before investing in it.

---

## 3. Stack and conventions

### Language
- **Go 1.25+** (set by the `go.mod` directive).
- Prefer the standard library over third-party dependencies. Each new dependency must have a clear justification.
- Approved external dependencies (others require discussion):
  - `github.com/spf13/cobra` for CLI command structure
  - `github.com/spf13/viper` only if config gets complex (start with stdlib `encoding/yaml`)
  - `github.com/anthropics/anthropic-sdk-go` for Anthropic provider
  - Standard `net/http` for everything else, including the Qwen OpenAI-compatible client (do not pull `openai-go` unless necessary)

### Project layout
```
cmd/tu-agent/                      ← main entrypoint, cobra commands
internal/
├── config/                        ← config loader (claude/, tu-agent/ layering)
├── provider/                      ← provider abstraction + adapters
│   ├── provider.go                ← interface
│   ├── claude.go                  ← Anthropic adapter
│   └── qwen.go                    ← Qwen self-hosted adapter
├── skill/                         ← skill registry, index-first loader
├── subagent/                      ← sub-agent dispatcher
├── tool/                          ← tool registry (bash, read_file, etc.)
├── memory/                        ← Memory Lite (JSON-based)
├── telemetry/                     ← token/cost logging
└── orchestrator/                  ← main agent loop
docs/                              ← user-facing docs (some already exist)
scripts/                           ← demo scripts, install script
```

Public packages should be small and well-named. Avoid `internal/util` or `internal/common` — they become dumping grounds.

### Style
- Run `gofmt -w` and `go vet ./...` before any commit.
- Errors: wrap with `fmt.Errorf("doing X: %w", err)`. Never swallow errors silently.
- No panics in library code. `main` may panic on unrecoverable startup errors.
- Logging: use `log/slog` from stdlib, JSON output to stderr in production, text in dev (`--debug` flag).
- Tests: `*_test.go` co-located. Use table-driven tests. Mock external dependencies, do not hit live APIs in unit tests.

### Concurrency
- Concurrency is allowed where it is measured to matter (e.g., the learn worker pool keeps N model requests in flight). Keep it coarse-grained: worker pools and channels over shared-memory cleverness.
- Anything concurrent must pass `go test -race -tags sqlite_fts5 ./...` and keep a single, small critical section per shared structure.
- Default behavior stays sequential unless the user opts in (config or flag).

---

## 4. How to interact with me (Carlos)

### When to ask vs. when to decide

**Decide on your own:**
- Code style choices within Go idioms
- Internal naming (variables, helpers, package-internal types)
- Test structure and coverage approach
- Adding a small helper function or refactoring within one file

**Always ask first:**
- Adding a new external dependency
- Changing public API of any package (anything exported)
- Touching the core architecture (providers, skill registry, memory, orchestrator)
- Reversing a previously agreed technical decision
- Pulling in work previously scoped out as a non-goal
- Going more than ~200 lines of new code without showing me a plan first

### Tone
- Be direct. No filler, no excessive apologies, no "great question!" preambles.
- If I am wrong about something, push back with reasoning. I want correct outcomes more than I want validation.
- Spanish or English is fine. Match the language I am using in the session.

### When in doubt, stop
If a request feels ambiguous or like it might be expanding scope, stop and clarify. A 30-second clarification beats 2 hours of wrong work.

---

## 5. Working with the Claude Code file format

This is a compatibility layer. The harness reads:
- `~/.claude/agents/*.md` — Claude Code sub-agent definitions (frontmatter + prompt)
- `~/.claude/skills/*/SKILL.md` — Claude Code skill definitions
- `~/.claude/CLAUDE.md` — global Claude Code instructions

The harness extends with:
- `~/.tu-agent/sub-agents/*.md` — tu-agent sub-agents (same format)
- `~/.tu-agent/skills/*/SKILL.md` — tu-agent skills
- `~/.tu-agent/config.yaml` — tu-agent config
- `./.tu-agent/*` — project-local overrides

**Parser rule: the Claude Code format is the source of truth for the schema.** When in doubt about a field, mirror what Claude Code does. Document any tu-agent-specific extensions in `docs/format-extensions.md`.

---

## 6. Testing requirements

For Phase 0:
- Unit tests for: config loader, skill registry indexing, provider adapters (mocked HTTP), tool registry, memory operations.
- Integration tests for: end-to-end `tu-agent chat` flow with a mock provider, `tu-agent prepare` on a small fixture repo.
- Manual demo tests for: the key buy-in claims. These are scripted but executed manually with real APIs.

Coverage target: **70%+ on `internal/` packages, no requirement on `cmd/`.**

Do not add E2E tests against live Anthropic or Qwen APIs in the test suite — those are demo scripts in `scripts/`.

---

## 7. Telemetry from day one

Every model call must log to `telemetry.jsonl`. This is not optional, even for early development. The whole point of Phase 0 is producing numeric evidence of token savings — if we cannot measure it, we cannot prove it.

Schema is in `internal/telemetry/schema.go`. Add fields conservatively.

---

## 8. What success looks like at end of Phase 0

By week 8 the repo has:
1. A working `tu-agent` binary that builds with `go build ./cmd/tu-agent` on Linux/macOS.
2. The 4 commands documented in PRD: `init`, `chat`, `stats`, plus `version` and `help`.
3. The 4 buy-in claims have data backing them.
4. A `docs/demo.md` walking a stakeholder through reproducing the demo.
5. Clean architecture for Phase 1 to extend (memory daemon, more sub-agents).

If at any point in Phase 0 we cannot trace what we are working on back to one of those five outcomes, we are off track.

---

## 9. Generic examples only

`tu-agent` is a **generic** coding harness. All documentation, tests, and demo scripts must use generic examples — no project-specific code, class names, or domain concepts.

When a demo or test needs a concrete example, use a representative but fictional codebase (e.g., a Go HTTP service, a Java REST API with `com.acme.*` packages) that any developer can follow. Real codebases that tu-agent is validated against are never the subject of this repo, and their names, symbols, or domain concepts must not leak into tu-agent's own docs or tests.

---

## 10. CLI + plugin dual availability rule

**Every feature implemented from Phase 1 onward must be available both as a `tu-agent` CLI command and through the Claude Code plugin.** Do not implement a feature on only one side.

The split follows the existing architecture:

| Component | Owns |
|---|---|
| `tu-agent` binary | Everything deterministic: graph queries, memory store, parsing, progress tracking, domain clustering |
| Claude Code plugin | Everything generative: skill synthesis, docstring/wiki generation, test body generation, architecture overview |

**Implementation pattern per feature type:**

- **Purely deterministic** (e.g., `graph flows`, `memory search`, `session list`): add a CLI subcommand **and** a new MCP tool in `cmd/tu-agent/mcp.go`. The plugin inherits it automatically via the bundled MCP server.
- **Purely generative** (e.g., architecture synthesis): add a CLI subcommand that calls the configured provider **and** a plugin skill under `plugin/skills/` that runs the same logic via Claude Code.
- **Hybrid** (e.g., `docs inline`, `docs wiki`, test generation): the CLI subcommand calls the binary for graph context then the provider for generation. The plugin skill delegates graph work to the binary via MCP, then uses Claude Code for generation. Same output format from both paths.

**Checklist before marking a Phase 1+ task done:**
- [ ] `tu-agent <command>` works from the terminal
- [ ] If the feature has a query/analysis component: MCP tool added and listed in `tu-agent mcp --list`
- [ ] If the feature has a generative component: plugin skill added under `plugin/skills/`
- [ ] Both paths produce the same output format (so they are comparable and testable)

## Stack
- Go 1.25+ (go.mod directive; the `modelcontextprotocol/go-sdk` dependency sets the floor)
- Linter: golangci-lint

## Build & Commands
```bash
go build -tags sqlite_fts5 ./...
go test -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
golangci-lint run --build-tags sqlite_fts5 ./...
```

The `sqlite_fts5` tag compiles SQLite's FTS5 module into `mattn/go-sqlite3` for ranked memory search. Binaries built without it still work — `memory search` degrades to substring matching with a logged warning.

## Coding Conventions
- Always wrap errors with context: `fmt.Errorf("serviceName.MethodName: %w", err)`
- Never use `panic()` in production code
- Define interfaces in the **consumer** package, not the producer
- Use `envconfig` struct tags for configuration: `env:"VAR_NAME"`
- Context is always the first parameter: `ctx context.Context`
- Pre-allocate slices when size is known: `make([]T, 0, n)`

## Workflow Rules
- Run `go vet ./...` and `golangci-lint run` before every commit
- Unit tests are required for every new handler and service
- NEVER hardcode secrets — use environment variables
- Commits must follow Conventional Commits: feat/fix/refactor/chore/docs/test
- Keep PRs focused — one feature or fix per branch

## Key Directories
- `cmd/`        → entrypoints (one main.go per service)
- `internal/`   → private application logic
- `pkg/`        → reusable packages (safe to import externally)
- `api/`        → protobuf definitions / OpenAPI specs
- `docs/`       → architecture decisions and Superpowers plans

<!-- tu-agent:knowledge -->
> Auto-generated by `tu-agent learn` — rewritten on each run. Put your own
> project instructions OUTSIDE the tu-agent:knowledge markers so they survive.

This repo has tu-agent knowledge: an `architecture` skill in .claude/skills/,
concept cards in the graph store (via get_concept), and a dependency graph,
queryable via the graph tools (get_context, get_impact, find_symbol, get_concept)
or the `tu-agent graph` CLI.

## PROTOCOL — follow before answering structural questions or editing
1. Orient: skim the `architecture` skill; get a concept's meaning with get_concept
   (or get_context), which read from the graph — concepts are not loaded as skills.
2. When the task is about impact, dependencies, callers, or "what breaks if I
   change X" — OR before you edit any file — query the graph FIRST:
     - If you have the graph tools:  get_context(<file-or-symbol>)
       (also: get_impact, find_symbol)
     - Otherwise, via CLI:           tu-agent graph context <file-or-symbol>
   get_context returns blast radius (dependents), the relevant concept(s),
   conventions, and tests to run — pointers, not source.
3. The graph is authoritative for structure (callers, dependents, tests), but it
   can miss framework/DI/inherited-from-compiled relationships. If it returns
   "(none)" where you expect dependents, cross-check with a targeted search
   before concluding.
4. Only then read specific files if you still need detail.

Do not skip step 2 for impact/dependency questions or before edits — the graph
is cheaper and more complete than re-deriving structure by reading files.

## MEMORY — recall at start, capture decisions
- At session start call `mem_recent`; before non-trivial work call
  `mem_search <feature area>`. Prior decisions and bug-patterns carry the "why"
  the code does not.
- When you make a decision, hit a bug-pattern worth remembering, or finish
  non-trivial work, call `mem_save` with a `decision/...` or `bug-pattern/...`
  topic and a one-paragraph summary. Save the durable "why", not session chatter.
- Save a gotcha/trap as its OWN atomic note with type `gotcha` (`mem_save` with
  `type: gotcha`), one trap per note — never as a buried `GOTCHA:`/`OJO:` section
  inside a `decision`/`architecture` note, or it cannot be retrieved as a gotcha.
  Recall all traps with `memory search --type gotcha`.
- After `git pull`, run `tu-agent memory import` to absorb teammates' memory
  (chunks under `.tu-agent/memory/chunks/` are shared via git).

## VERIFY before claiming done
Before reporting work complete, fixed, or passing, run the project's test
command and report the actual result. Evidence before assertions.

## If the graph looks wrong
If a graph query contradicts what the code plainly shows, the graph may be
stale — run `tu-agent learn` to rebuild, then re-query.
<!-- /tu-agent:knowledge -->
