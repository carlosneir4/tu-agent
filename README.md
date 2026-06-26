# tu-agent

A code-intelligence and multi-agent coding harness written in Go. It gives an AI
coding assistant a **durable, queryable model of your codebase** instead of making
it re-read files every session — and it runs either as a standalone CLI or as a
[Claude Code](https://claude.com/claude-code) plugin.

The deterministic core needs **no API key**: graph queries, the concept index,
memory, and test-gap analysis all run locally. Only the generative steps
(one-line concept definitions, architecture synthesis, test bodies) call a model —
and those can run through your existing Claude Code subscription via the plugin,
so you never need an Anthropic API key to get the full structural value.

---

## The idea: three organs of project knowledge

tu-agent replaces the old "generate prose docs per module" approach (prose rots)
with three complementary stores, each good at a different kind of question:

| Organ | What it is | Always accurate? | Answers |
|-------|-----------|------------------|---------|
| **Graph** | The live code structure — symbols and the `calls` / `implements` / `extends` / `imports` / `overrides` edges between them, in SQLite. Rebuilt incrementally from a SHA-256 diff. | Yes — derived from source | *What breaks if I change X? Who calls this? Where is it defined? What's the call flow? Which symbol is a chokepoint?* |
| **Concept index** | One thin **card** per concept (usually a package): a vocabulary → landmarks map. Near-static, ≤1 KB each, stored as `.claude/skills/<concept>/SKILL.md`. | Structurally yes; the one-line *definition* is generated | *What does this part of the system mean? What are its landmark types? Which concepts exist?* |
| **Memory** | Durable, topic-keyed facts with provenance and revisions, in SQLite. Survives across sessions; can be linked to graph nodes. | It's curated truth, not derived | *What did we decide / learn here? What's fragile? What relates to this node?* |

The split is deliberate: **structure questions go to the graph (exact, cheap,
no model), meaning questions go to the thin cards, and hard-won facts go to
memory.** An agent loads a compact index up front and pulls detail only when it
needs it, keeping context small.

```
            ┌─────────────┐   exact structure, zero model calls
   graph ──►│  graph.db   │   impact · context · flow · traits · bridges
            └─────────────┘
            ┌─────────────┐   thin meaning cards, vocabulary → landmarks
 concepts ─►│ SKILL.md ×N │   one card per concept (package)
            └─────────────┘
            ┌─────────────┐   durable facts, provenance, links to nodes
  memory ──►│  memory.db  │   save · search · link · sessions
            └─────────────┘
```

### Benchmark: cold vs. graph

Ablation on a ~3,300-file Java repo, 4 structure tasks (blast radius, chokepoint
ranking, execution flow, untested-risk), median of 2 reps. A graph-equipped agent
vs. a grep-only baseline:

| Per task | grep-only (cold) | with graph |
|----------|------------------|------------|
| Cost | $0.55 | **$0.39** (−30%) |
| Input tokens | 312k | **100k** (−68%) |
| Turns | 17 | **5.5** |
| Derived-metric task¹ | wrong ranking | **correct** |

¹ Risk-ranking untested code by fan-in × blast radius: the graph computes it;
the grep-only agent can't reconstruct it and gets the ranking wrong.

---

## Two ways to run it

### A. As a CLI (the binary)

Everything deterministic is a `tu-agent` subcommand you run in a terminal.
Generative subcommands call a configured provider (`claude` or a self-hosted
OpenAI-compatible `local` model).

### B. As a Claude Code plugin (recommended — no API key needed)

The plugin bundles the same binary plus an MCP server. Claude Code does the
generative work on your subscription; the binary does all the structural work.
You get:

- **Skills** you invoke in a session: `/tu-agent:learn`, `/tu-agent:synthesize`,
  `/tu-agent:status`, and a test-generation skill.
- **An MCP server** (`tu-agent-graph`) that exposes the graph/memory/test tools
  live, so the agent answers "what breaks if I change this?" by querying the
  graph instead of re-reading your repo.

The **dual-availability rule** holds across the project: every feature is both a
CLI subcommand and (where it's a query) an MCP tool, so both paths produce the
same output.

---

## Requirements

- **Go 1.22+**
- **`sqlite3`** and **`jq`** on your PATH (for the Java-readiness check and JSON
  workflows)
- An API key **only** for generative steps run via the CLI:
  - `ANTHROPIC_API_KEY` for the `claude` provider
  - `LOCAL_API_KEY` for a self-hosted OpenAI-compatible endpoint (any non-empty
    value for LM Studio)
- The plugin path needs **no API key** — generation runs on your Claude Code
  subscription.

---

## Installation

### Binary

```bash
go install -tags sqlite_fts5 github.com/tu/tu-agent/cmd/tu-agent@latest
```

This installs to `$(go env GOPATH)/bin` (typically `~/go/bin`). Put it on your PATH:

```bash
# ~/.zshenv runs for all zsh shells, including non-interactive ones (Claude Code)
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.zshenv
source ~/.zshenv
tu-agent version
```

> **The `sqlite_fts5` build tag** compiles SQLite's FTS5 module into the binary so
> ranked `memory search` works. Binaries built without it still run — search
> degrades to substring matching with a logged warning.

**Local build without installing:**

```bash
go build -tags sqlite_fts5 -o bin/tu-agent ./cmd/tu-agent
# run as ./bin/tu-agent <command>, or: make build
```

### Claude Code plugin

```bash
claude --plugin-dir /path/to/tu-agent/plugin
```

The plugin ships a `.mcp.json`, so the `tu-agent-graph` MCP server registers
automatically. It expects the binary at `~/.tu-agent/bin/tu-agent` (install it
there with `go install` as above, or point the shim at your build).

---

## Quick start (deterministic, no API key)

From the root of any Go / Java / Python / TypeScript repo:

```bash
# 1. Build the concept index + graph + knowledge block — zero model calls
tu-agent learn --skip-llm

# 2. Ask structural questions (exact, instant)
tu-agent graph context  com.acme.shop.orders.OrderService   # blast radius + skills + tests
tu-agent graph impact   com.acme.shop.orders.BaseService.describe
tu-agent graph flow     com.acme.shop.orders.OrderService.total
tu-agent graph bridges  --top 10                            # architectural chokepoints
tu-agent concepts                                           # the concept cards

# 3. Record and recall durable facts
tu-agent memory save --topic architecture/auth --content "sessions are signed, not encrypted"
tu-agent memory search auth

# 4. Track work across restarts
tu-agent session start        # prints last session's summary
tu-agent session end --summary "wired up session injection"
```

To validate the whole no-API-key path on a fresh Java repo, run the readiness
gauntlet (see [docs/java-quickstart.md](docs/java-quickstart.md)):

```bash
scripts/java_ready_check.sh          # PASS/exit 0 when the pipeline is healthy
```

---

## Command reference

### Knowledge: build and query

| Command | What it does |
|---------|--------------|
| `learn [path]` | Build the concept index: graph build → one card per concept under `.claude/skills/` → register a knowledge-pointer block in `CLAUDE.md`. `--skip-llm` keeps it 100% deterministic; otherwise it also writes a one-line definition per card and synthesizes an architecture overview. |
| `concepts [path]` | Print the concept index cards (deterministic, no model calls). |
| `map [path]` | Print the domain map — how files cluster into domains, exactly as `learn` derives them. `--json` includes per-domain dependency context for external orchestrators. |

`learn` flags: `--skip-llm` (no model calls), `--concept-root <pkg>` (the package
whose direct subpackages define concepts), `--cluster leiden` (fallback clustering
when no concept root is set), `--depth`, `--min-files`, `--min-standalone-files`,
`--max-files-per-domain`, `--patterns`, `--provider claude|local`.

### Graph: exact structure (no model calls)

```bash
tu-agent graph build [path]     # build or incrementally refresh (alias: update)
tu-agent graph status           # size, failed files, extractor version
tu-agent graph context <sym>    # blast radius + relevant skills + conventions + tests
tu-agent graph impact  <sym>    # who is affected if you change <sym>
tu-agent graph find    <sym>    # where a symbol is defined
tu-agent graph flow    <sym>    # execution call tree with boundary + dispatch annotations
tu-agent graph traits  <type>   # shared interfaces, where logic lives, blast radius
tu-agent graph bridges          # architectural chokepoints (betweenness over the call graph)
```

- `impact` surfaces a **surprising cross-domain dependencies** section
  (`--surprising-only`, `--surprise-threshold`, `--domain-depth`, `--min-domain-edges`).
- `bridges` ranks chokepoints by approximate betweenness (`--top`, `--samples`, `--json`).

### Memory: durable facts

```bash
tu-agent memory save --topic architecture/auth --content "..."   # upsert by topic key (bumps revision)
tu-agent memory search <query>                                   # ranked (FTS5) or substring fallback
tu-agent memory list                                             # all observations, with IDs
tu-agent memory link  --from <obs-id> --to <node-id> --type documents
tu-agent memory links --of <id>                                  # relations for an id
```

Relation types: `related | supersedes | documents | conflicts_with`. Linking an
observation to a graph node makes it surface in `graph impact` as **"Related
knowledge."**

### Sessions: continuity across restarts

```bash
tu-agent session start          # opens a session, prints the previous summary
tu-agent session end            # composes a deterministic summary from this session's observations
tu-agent session end --summary "explicit text"
tu-agent session list
```

At most one session is active per project; `start` auto-closes any open one. The
previous summary is injected into the next `chat` session ahead of recent memory.

### Tests: coverage analysis and generation

```bash
tu-agent test gaps              # rank untested public functions by risk (fan-in × blast radius)
tu-agent test gaps --json --top 20 --domain <skill>
tu-agent test gen --symbol <sym>    # generate a verified unit test (Go/Java/Python/TS)
```

`test gen` is hybrid: the binary supplies graph context + a scoped run command,
the provider writes the body, and the result is verified (`--max-repair`,
`--dry-run`, `--force`, `--discard-failing`).

### Agent loops

```bash
tu-agent chat                   # interactive agent in the current repo (/exit or Ctrl+D)
tu-agent chat --provider local  # use the self-hosted model
tu-agent run --task "..."       # single non-interactive task (for scripted benchmarks)
```

The chat agent gets bash/read/write/grep/find/load_skill/memory tools, the
sub-agent dispatcher, the concept index, recent memory, and the previous session
summary.

### Setup, inventory, MCP, cost

```bash
tu-agent init [path]            # generate 5 dev-flow agents + CLAUDE.md (--lang, --no-llm, --force)
tu-agent setup                  # interactive ~/.tu-agent/config.yaml (once per machine)
tu-agent scan                   # read-only inventory of skills/agents/config/plugins (--json)
tu-agent skill prune            # remove empty skill dirs left by an interrupted run
tu-agent mcp                    # run the stdio MCP server (--list to print the tools)
tu-agent stats                  # token usage and cost from telemetry.jsonl
tu-agent bench --baseline a.jsonl --compare b.jsonl   # compare two telemetry runs
tu-agent version
```

---

## Using it from Claude Code

Once the plugin is installed, in a Claude Code session opened on your repo:

**Skills (you invoke them):**

| Skill | What it does |
|-------|--------------|
| `/tu-agent:learn [path]` | Full pipeline: graph build → concept index (deterministic) → one-line definitions (in-session, no API key) → architecture synthesis → `CLAUDE.md` registration |
| `/tu-agent:synthesize` | Regenerate the architecture overview from existing concept cards |
| `/tu-agent:status` | Progress, card staleness, and graph health |

**MCP tools (the agent calls them automatically):** `get_impact`, `get_context`,
`find_symbol`, `get_flow`, `get_traits`, `get_concept`, `get_bridges`,
`mem_save`, `mem_search`, `mem_recent`, `mem_relate`, `mem_related`,
`mem_session_start|end|list`, `test_gaps`, `test_scaffold`. Run
`tu-agent mcp --list` to see the live set.

These read `./.tu-agent/graph.db` and `./.tu-agent/memory.db`. Build the graph
first with `/tu-agent:learn` (or `tu-agent graph build`); the MCP server also
rebuilds on hash drift at session start.

---

## Configuration

tu-agent merges config from three layers (later wins):

| Layer | Path | Purpose |
|-------|------|---------|
| Global Claude Code | `~/.claude/` | Read-only compatibility layer |
| User config | `~/.tu-agent/config.yaml` | Personal provider defaults (`tu-agent setup`) |
| Project config | `./.tu-agent/config.yaml` | Per-repo routing and model overrides |

**Example `.tu-agent/config.yaml`:**

```yaml
routing:
  default: claude          # used when no task-specific route matches
  tasks:
    chat: claude
  sub_agents:
    codebase-explorer: local   # route exploration to the cheaper local model

providers:
  claude:
    model: claude-sonnet-4-6   # empty → provider default
  local:
    base_url: http://localhost:1234   # required; your OpenAI-compatible endpoint
    model: ""                  # empty → server uses the loaded model
    context_size: 16384        # must match the n_ctx loaded by the server
    max_output_tokens: 2048    # 0 = let the server decide
    request_timeout_seconds: 600
```

> **`context_size` must match the model loaded in your local server.** Too low and
> tu-agent underuses the context; too high and you get `n_keep > n_ctx` HTTP 400s.
> Reload the model after changing `n_ctx`.

API keys are never stored in config — set them as environment variables:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export LOCAL_API_KEY="any-value"     # any non-empty value for LM Studio
```

---

## How knowledge is stored on disk

```
.tu-agent/
├── graph.db                 ← the code knowledge graph (derived; safe to delete + rebuild)
├── memory.db                ← durable observations + relations + sessions (NOT derived — never delete)
└── telemetry.jsonl          ← one row per model call (gitignored)
.claude/skills/<concept>/SKILL.md   ← one thin concept card per concept
CLAUDE.md                    ← includes a tu-agent:knowledge pointer block
```

**Concept cards** are `SKILL.md` files with YAML frontmatter (`name`,
`description`, landmarks/traits in the body). At session start the agent receives
a compact index (`- name: description` per card); full content loads on demand —
keeping the initial prompt small. Commit `.claude/skills/` to share the captured
knowledge with your team.

Skills are discovered from four layers (later shadows earlier):
`~/.claude/skills/` < `~/.tu-agent/skills/` < `./.claude/skills/` <
`./.tu-agent/skills/`.

---

## Telemetry and cost

Every model call is logged to `.tu-agent/telemetry.jsonl` (gitignored).
`tu-agent stats` summarizes token usage, cost, and latency by provider;
`tu-agent bench --baseline a.jsonl --compare b.jsonl` compares two runs to measure
routing savings. Deterministic commands (`--skip-llm`, all graph/memory queries)
log **zero** model-call rows — which is exactly how the readiness check proves the
no-API-key path stays free.

---

## Development

```bash
make build      # → bin/tu-agent (with the sqlite_fts5 tag)
make test       # go test -race -tags sqlite_fts5 ./...
make lint       # golangci-lint (build tag set)
make vet        # go vet -tags sqlite_fts5 ./...
make fmt        # gofmt -w
```

Coverage target: **≥70% on `internal/` packages.** All commands take the
`sqlite_fts5` build tag.

---

## Project layout

```
cmd/tu-agent/          ← main entrypoint, cobra commands, MCP server
internal/
├── config/            ← 3-layer config loader
├── provider/          ← provider interface + claude and local adapters
├── graph/             ← code knowledge graph: build, store, query (impact/context/flow/traits/bridges/surprise)
├── memory/            ← durable SQLite store: observations, relations, sessions, FTS5 search
├── skill/             ← concept-card index + SKILL.md frontmatter scanner
├── subagent/          ← sub-agent dispatcher + codebase-explorer
├── tool/              ← bash, read/write_file, grep, find, load_skill, memory tools
├── testgen/           ← test-gap ranking + verified test generation
├── codegen/           ← scanner/sampler/prompt builders (used by init)
├── orchestrator/      ← agent loop (tool execution, conversation history)
├── stats/             ← telemetry summarization
├── bench/             ← telemetry comparison
└── telemetry/         ← JSONL token/latency logger
plugin/                ← Claude Code plugin: skills, agents, hooks, MCP, binary shim
docs/                  ← quickstarts, format extensions, superpowers specs + plans
scripts/               ← java_ready_check.sh, fixtures, parity/demo scripts
```
