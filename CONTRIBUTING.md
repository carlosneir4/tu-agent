# Contributing to tu-agent

Thanks for your interest. Small, focused contributions are the easiest to
review and land.

## Build and test

Go 1.25+ is required. All commands take the `sqlite_fts5` build tag (it
compiles SQLite's FTS5 module for ranked memory search):

```bash
make build      # → bin/tu-agent
make test       # go test -race -tags sqlite_fts5 ./...
make lint       # golangci-lint (build tag set)
make vet        # go vet -tags sqlite_fts5 ./...
make fmt        # gofmt -w
```

Before every commit: `make fmt`, `make vet`, and `make lint` must be clean
(note: golangci-lint's default set does **not** run gofmt — run it
separately).

## Ground rules

- **Conventional Commits** — `feat:`, `fix:`, `refactor:`, `chore:`,
  `docs:`, `test:`.
- **One feature or fix per branch/PR.** The PR template in `.github/`
  applies.
- **Tests required** for new behavior; table-driven where it fits, mocked
  externals, never live APIs in the unit suite. Coverage target is ≥70%
  on `internal/` packages.
- **Standard library first.** A new external dependency needs a clear
  justification in the PR description.
- Errors are wrapped with context (`fmt.Errorf("doing X: %w", err)`); no
  `panic()` in library code.
- Docs, tests, and demo fixtures use **generic examples only** — no code
  or names from real private codebases.

## Architecture notes

The project is plugin-first: the `tu-agent` binary owns everything
deterministic (graph, memory, gates, parsing) and the Claude Code plugin
owns everything generative (skills under `plugin/skills/`). New features
must not add CLI paths that call a model provider directly — see
`CLAUDE.md` §10 for the full rule and the frozen legacy surface.

## Security issues

Do not open public issues for vulnerabilities — see [SECURITY.md](SECURITY.md).
