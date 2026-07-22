# tu-agent

A code-intelligence and multi-agent coding harness for
[Claude Code](https://claude.com/claude-code). It gives your AI coding assistant
a **durable, queryable model of your codebase** instead of making it re-read
files every session.

Install it as a Claude Code plugin and it runs on your existing subscription —
**no API key needed**. The deterministic core (graph queries, the concept index,
memory, and test-gap analysis) runs locally in a bundled binary; only the
generative steps (one-line concept definitions, architecture synthesis, test
bodies) call a model, and those run in Claude Code on your subscription. A
standalone CLI is also available for scripts and CI.

---

## The idea: three organs of project knowledge

tu-agent replaces the old "generate prose docs per module" approach (prose rots)
with three complementary stores, each good at a different kind of question:

| Organ | What it is | Always accurate? | Answers |
|-------|-----------|------------------|---------|
| **Graph** | The live code structure — symbols and the `calls` / `implements` / `extends` / `imports` / `overrides` edges between them, in SQLite. Rebuilt incrementally from a SHA-256 diff. | Yes — derived from source | *What breaks if I change X? Who calls this? Where is it defined? What's the call flow? Which symbol is a chokepoint?* |
| **Concept index** | One thin **card** per concept (usually a package): a vocabulary → landmarks map. Near-static, ≤1 KB each, stored in the graph store (`graph.db`, the `concepts` table) and read via `get_concept`. | Structurally yes; the one-line *definition* is generated | *What does this part of the system mean? What are its landmark types? Which concepts exist?* |
| **Memory** | Durable, topic-keyed facts with provenance and revisions, in SQLite. Survives across sessions; can be linked to graph nodes. | It's curated truth, not derived | *What did we decide / learn here? What's fragile? What relates to this node?* |

The split is deliberate: **structure questions go to the graph (exact, cheap,
no model), meaning questions go to the thin cards, and hard-won facts go to
memory.** An agent loads a compact index up front and pulls detail only when it
needs it, keeping context small.

```
  graph &   ┌─────────────┐   structure + concept index, zero model calls
 concepts ─►│  graph.db   │   impact · context · flow · traits · bridges · get_concept
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

## Install (Claude Code plugin)

From a Claude Code session:

```
/plugin marketplace add carlosneir4/tu-agent
/plugin install tu-agent@tools
```

That's the whole setup. On first use the bundled shim downloads the matching
binary (`tu-agent-<os>-<arch>`) from the latest GitHub Release, verifies its
checksum, and caches it at `~/.tu-agent/bin/` — **no API key, no Go toolchain, no
manual build**. It then keeps that binary up to date automatically: a throttled,
best-effort check (the published `SHA256SUMS` is the freshness signal, so there's
no pinned version) refreshes it when a newer release ships. Opt out with
`TU_AGENT_NO_AUTO_UPDATE=1`; tune the cadence with `TU_AGENT_UPDATE_INTERVAL`
(seconds, default 86400). The `tu-agent-graph` MCP server and the `/tu-agent:*`
skills register automatically.

Prefer not to auto-download? Build from source (`make build`) and place the
binary at `~/.tu-agent/bin/tu-agent`, or point the shim at your own fork via
`TU_AGENT_RELEASE_REPO`. See [SECURITY.md](SECURITY.md) for the trust model.

---

## Use it (from Claude Code)

In a session opened on your repo:

1. **Build the knowledge index** — run `/tu-agent:prepare` to set up the repo
   (a `CLAUDE.md` knowledge block and a hardened `settings.json`) and build the
   index, or `/tu-agent:learn` for just the index. Generation runs in-session on
   your subscription — no API key.
2. **Ask structural questions** — the agent calls the `tu-agent-graph` MCP tools
   automatically: *what breaks if I change this? who calls it? what's the flow?*
   — querying the graph instead of re-reading your repo.

**Skills you invoke:**

| Skill | What it does |
|-------|--------------|
| `/tu-agent:prepare` | Set up a repo: `CLAUDE.md` knowledge block, hardened `settings.json`, seeded `.tu-agent/` config; builds the index if empty |
| `/tu-agent:learn` | Build the graph + per-domain concept index + architecture overview |
| `/tu-agent:synthesize` | Regenerate the architecture overview from the concept index in the graph store |
| `/tu-agent:status` | Progress, card staleness, and graph health |
| `/tu-agent:groundwork` | Anchor-before-build protocol: graph + memory recall, gap questions, confirmed approach |
| `/tu-agent:design` | Architecture guild for a new feature/subsystem from zero: proportionality-gated lens critique, human picks what enters the design |
| `/tu-agent:tdd` | End-to-end TDD dev-flow (interrogation → spec → deterministic RED/GREEN gates → review) |
| `/tu-agent:test-gen` | Generate a verified unit test for a function, or for the riskiest untested code |
| `/tu-agent:crystallize` | Consolidate a dense cluster of memory notes into a reusable skill |

**MCP tools the agent calls automatically:** `get_impact`, `get_context`,
`find_symbol`, `get_flow`, `get_traits`, `get_concept`, `get_bridges`, the
`mem_*` memory tools, and `test_gaps` / `test_scaffold`. They read
`./.tu-agent/graph/graph.db` and `./.tu-agent/memory/memory.db`; the server
rebuilds on hash drift at session start.

---

## Documentation

Full guide for your team, in [`docs/`](docs/) — start with the
[documentation home](docs/README.md):

| Page | You'll learn |
|------|--------------|
| [Getting started](docs/getting-started.md) | Install, prepare a repo, first query — end to end. |
| [Mental model](docs/mental-model.md) | The three organs — graph, concept index, memory — and which question goes where. |
| [Skills reference](docs/skills-reference.md) | Every `/tu-agent:*` command and skill, grouped, with a when-to-use. |
| [MCP tools](docs/mcp-tools.md) | The `tu-agent-graph` tools the agent calls for you, each with an example. |
| [Dev-flow](docs/dev-flow.md) | The `groundwork → design → tdd` chain, the agents, and project rules. |
| [Memory](docs/memory.md) | Note types, the capture protocol, and team sync through committed chunks. |
| [Cookbook](docs/cookbook.md) | End-to-end recipes for common tasks. |

---

## Standalone CLI (optional)

Everything deterministic is also a `tu-agent` subcommand, handy for scripts and
CI — no API key, no model calls. Get the binary (download the release asset for
your platform, or `make build` from a clone), put it on your PATH, then:

```bash
tu-agent learn --skip-llm           # build graph + concept index, zero model calls
tu-agent graph context <symbol>     # blast radius + relevant concept + tests
tu-agent graph impact|flow|bridges  # structure queries
tu-agent memory save|search         # durable, topic-keyed facts
tu-agent test gaps                  # rank untested code by risk (fan-in × blast radius)
```

| Group | Commands |
|-------|----------|
| Knowledge | `learn`, `concepts`, `map` |
| Graph | `graph build \| status \| context \| impact \| find \| flow \| traits \| bridges \| cycles \| architecture` |
| Memory | `memory save \| search \| list \| link \| links \| relink \| rescope \| delete \| crystallize \| materialize` |
| Team memory | `memory export \| import \| pending \| chunks` — share curated notes through git-committed chunks; `pending` is the human pre-commit review gate |
| TDD dev-flow | `tdd run \| status \| state \| gate \| verify \| prompt \| path \| check` |
| Sessions | `session start \| end \| list` |
| Tests | `test gaps \| gen \| mutation` |
| Setup / misc | `prepare` (alias `init`), `scan`, `skill`, `mcp`, `version` |

Run `tu-agent <command> --help` for flags. Full knowledge generation — the
one-line concept definitions and the architecture overview — runs in the plugin
inside Claude Code, not the CLI. `sqlite3` and `jq` on PATH are used by the
Java-readiness check (`scripts/java_ready_check.sh`). The `sqlite_fts5` build tag
compiles SQLite's FTS5 module so ranked `memory search` works; without it, search
degrades to substring matching.

---

## How knowledge is stored on disk

```
.tu-agent/
├── graph/graph.db          ← code graph + concept index + architecture overview (derived; safe to delete + rebuild)
├── memory/memory.db        ← durable observations + relations + sessions (NOT derived — never delete; gitignored)
└── share/memory/chunks/    ← per-author exported note chunks (committed — this is how teams share memory)
CLAUDE.md                   ← includes a tu-agent:knowledge pointer block
```

**Team memory** flows through git: `memory export` writes your curated notes to a
per-author chunk under `.tu-agent/share/memory/chunks/` (and tells you on stderr
when new or updated notes landed there), you review what's about to be shared with
`memory pending`, commit the chunk, and teammates absorb it with
`memory import` (the plugin runs export/import automatically at session
boundaries). `memory.db` itself is never shared — the committed chunks are the
transport.

**Concept cards** are rows in `graph.db` (the `concepts` table), read with
`get_concept` and rebuilt by `learn` — they are not files. The **architecture
overview** is likewise stored in `graph.db` (metadata), read with
`get_architecture` — also not a file. Because `graph.db` is derived (and
gitignored), teammates rebuild the graph, concept index, and overview by running
`/tu-agent:learn` rather than pulling files.

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
├── graph/             ← code knowledge graph + concept index: build, store, query (impact/context/flow/traits/bridges/surprise)
├── memory/            ← durable SQLite store: observations, relations, sessions, FTS5 search
├── autolink/          ← link memory observations to graph nodes
├── skill/             ← skill registry + SKILL.md frontmatter scanner (architecture overview, user skills)
├── subagent/          ← sub-agent dispatcher + codebase-explorer
├── tool/              ← bash, read/write_file, grep, find, load_skill, memory tools
├── testgen/           ← test-gap ranking + verified test generation
├── coverage/          ← coverage parsing (Go, JaCoCo, coverage.py, Istanbul)
├── mutation/          ← mutation-testing engines (go-mutesting, PIT, mutmut, Stryker)
├── tdd/               ← TDD dev-flow state machine
├── codegen/           ← scanner/sampler/prompt builders (used by init)
├── orchestrator/      ← agent loop (tool execution, conversation history)
├── stats/             ← telemetry summarization
├── bench/             ← telemetry comparison
└── telemetry/         ← JSONL token/latency logger
plugin/                ← Claude Code plugin: skills, agents, hooks, MCP, binary shim
scripts/               ← java_ready_check.sh, fixtures, parity/demo scripts
```
