---
name: architecture
description: Project architecture overview — domains, navigation, and change-impact map.
---
# Architecture Overview

## Purpose
tu-agent is a Go CLI coding harness that builds a persistent knowledge graph of a target codebase, clusters its files into semantic domains, and uses LLM providers (Anthropic Claude or any OpenAI-compatible local server) to synthesize per-domain concept cards and an architecture overview. The same binary then exposes that knowledge to an interactive agentic loop, generates tests against graph context guided by coverage and mutation analysis, persists decisions and bug-patterns in a shared memory store, and serves a Claude Code MCP plugin. End to end it goes: scan source -> extract a dependency graph (stored in SQLite) -> query/cluster it -> synthesize concepts and architecture -> run an agent loop or generate tests over that context, logging every model call to append-only JSONL telemetry.

## Domains
| Domain | What it does | Key files |
|--------|--------------|-----------|
| `cmd-tu-agent` | CLI entrypoint and cobra command tree; wires every subcommand (chat, run, init, learn, graph, mcp, stats, bench, test, scan, skill, map, memory) to the internal packages | `cmd/tu-agent/graph.go`, `cmd/tu-agent/test.go`, `cmd/tu-agent/chat.go`, `cmd/tu-agent/init.go`, `cmd/tu-agent/learn_graph.go`, `cmd/tu-agent/learn_synthesize.go`, `cmd/tu-agent/mcp.go`, `cmd/tu-agent/concepts.go`, `cmd/tu-agent/knowledge_block.go` |
| `internal-graph` | Shared graph data model (nodes, edges) and graph algorithms (SCC/cycles) used by every graph sub-package and consumer | `internal/graph/model.go`, `internal/graph/scc.go` |
| `internal-graph-extract` | Multi-language source parsing (Go, Java, Python, TypeScript) and dependency-edge resolution feeding the graph store | `internal/graph/extract/build.go`, `internal/graph/extract/extract.go`, `internal/graph/extract/typescript.go` |
| `internal-graph-query` | Graph queries: context, impact, traits, surprising connections, execution flows, bridge nodes | `internal/graph/query/query.go`, `internal/graph/query/traits.go`, `internal/graph/query/surprise.go` |
| `internal-graph-store` | SQLite-backed persistence of the dependency graph and concept cards (graph.db) | `internal/graph/store/store.go` |
| `internal-codegen` | Domain clustering, concept/skill generation, architecture synthesis, skill index, settings hardening, fingerprinting, CLAUDE.md knowledge block | `internal/codegen/scanner.go`, `internal/codegen/domainmap.go`, `internal/codegen/cluster.go`, `internal/codegen/concepts.go`, `internal/codegen/harden.go`, `internal/codegen/skillindex.go`, `internal/codegen/fingerprint.go` |
| `internal-memory` | Persistent memory store: decisions, bug-patterns, sessions, FTS5 search, team chunk export/import | `internal/memory/store.go` |
| `internal-testgen` | Graph-aware test generation across languages, calling a provider, with safe merge into conventional files | `internal/testgen/java.go`, `internal/testgen/python.go`, `internal/testgen/typescript.go` |
| `internal-coverage` | Parses coverage reports (Go coverprofile, JaCoCo, Cobertura, Istanbul) and feeds test-gap ranking | `internal/coverage/coverage.go`, `internal/coverage/generate.go`, `internal/coverage/load.go` |
| `internal-mutation` | Language-specific mutation operators (Go, Java, Python) for opt-in mutation testing | `internal/mutation/golang.go`, `internal/mutation/java.go`, `internal/mutation/python.go` |
| `internal-provider` | LLM provider abstraction and adapters for Anthropic Claude and OpenAI-compatible local servers | `internal/provider/local.go`, `internal/provider/claude.go`, `internal/provider/errors.go` |
| `internal-orchestrator` | The agentic loop driving a provider with the tool set toward a task | `internal/orchestrator/orchestrator.go` |
| `internal-tool` | Tool registry and tools: bash, read/write file, grep, list_dir, memory, load_skill, with path confinement | `internal/tool/tool.go`, `internal/tool/bash.go`, `internal/tool/memory.go` |
| `internal-subagent` | Sub-agent declarations, loader, dispatcher, and the dispatch_agent tool wrapper | `internal/subagent/subagent.go`, `internal/subagent/loader.go`, `internal/subagent/dispatch_agent.go` |
| `internal-skill` | Skill registry: scan, parse, and index SKILL.md files | `internal/skill/skill.go`, `internal/skill/scanner.go` |
| `internal-telemetry` | Per-call model observability (JSONL), schema, and cost estimation | `internal/telemetry/telemetry.go`, `internal/telemetry/cost.go` |
| `internal-stats` | Usage aggregation over telemetry | `internal/stats/stats.go` |
| `internal-bench` | A/B benchmark comparison over telemetry and stats | `internal/bench/bench.go` |
| `internal-config` | Config loader layering claude/ and tu-agent/ sources | `internal/config/loader.go`, `internal/config/config.go` |
| `plugin` | Claude Code plugin assets: agent templates and generative skills run via Claude Code/MCP | `plugin/agent-templates/validate.py` |

## How Domains Connect
The dependency flow is layered. `internal-graph` (the data model), `internal-provider`, `internal-telemetry`, `internal-config`, `internal-skill`, and `internal-memory` are leaf domains.

- The graph stack layers on the model: `internal-graph-extract`, `internal-graph-query`, and `internal-graph-store` all depend on `internal-graph`; `internal-graph-extract` also depends on `internal-graph-store` to persist what it parses, and `internal-graph-query` reads the graph for structural answers.
- `internal-tool` depends on `internal-provider`, `internal-telemetry`, `internal-skill` (load_skill tool), and `internal-memory` (memory tool).
- `internal-orchestrator` depends on `internal-provider`, `internal-telemetry`, and `internal-tool` to run the agent loop.
- `internal-subagent` depends on `internal-orchestrator`, `internal-provider`, `internal-telemetry`, `internal-tool`, and `internal-skill`.
- `internal-codegen` depends on `internal-graph`, `internal-orchestrator`, `internal-provider`, `internal-telemetry`, `internal-tool`, `internal-skill`, and `internal-memory` (clustering plus agent-driven synthesis over the tool set).
- `internal-testgen` depends on `internal-graph`, `internal-graph-query`, `internal-provider`, and `internal-telemetry`; `internal-coverage` and `internal-mutation` feed test generation through `cmd-tu-agent`, which orchestrates the `test` gaps/coverage/mutation runs.
- `internal-stats` depends on `internal-telemetry`; `internal-bench` depends on `internal-stats` and `internal-telemetry`.
- `cmd-tu-agent` is the integration layer and depends on every other domain: `internal-codegen`, `internal-config`, `internal-memory`, `internal-orchestrator`, `internal-provider`, `internal-skill`, `internal-subagent`, `internal-telemetry`, `internal-tool`, `internal-graph`, `internal-graph-extract`, `internal-graph-query`, `internal-graph-store`, `internal-testgen`, `internal-coverage`, `internal-mutation`, `internal-stats`, and `internal-bench`.

## Blast Radius
Changing `internal-graph` affects: `internal-graph-extract`, `internal-graph-query`, `internal-graph-store`, `internal-codegen`, `internal-testgen`, `cmd-tu-agent`.

Changing `internal-provider` affects: `internal-tool`, `internal-orchestrator`, `internal-subagent`, `internal-codegen`, `internal-testgen`, `cmd-tu-agent`.

Changing `internal-telemetry` affects: `internal-tool`, `internal-orchestrator`, `internal-subagent`, `internal-codegen`, `internal-testgen`, `internal-stats`, `internal-bench`, `cmd-tu-agent`.

Changing `internal-tool` affects: `internal-orchestrator`, `internal-subagent`, `internal-codegen`, `cmd-tu-agent`.

Changing `internal-orchestrator` affects: `internal-subagent`, `internal-codegen`, `cmd-tu-agent`.

Changing `internal-skill` affects: `internal-tool`, `internal-subagent`, `internal-codegen`, `internal-orchestrator`, `cmd-tu-agent`.

Changing `internal-memory` affects: `internal-tool`, `internal-subagent`, `internal-orchestrator`, `internal-codegen`, `cmd-tu-agent`.

Changing `internal-graph-store` affects: `internal-graph-extract`, `cmd-tu-agent`.

Changing `internal-graph-query` affects: `internal-testgen`, `cmd-tu-agent`.

Changing `internal-graph-extract` affects: `cmd-tu-agent`.

Changing `internal-codegen` affects: `cmd-tu-agent`.

Changing `internal-subagent` affects: `cmd-tu-agent`.

Changing `internal-testgen` affects: `cmd-tu-agent`.

Changing `internal-coverage` affects: `cmd-tu-agent`.

Changing `internal-mutation` affects: `cmd-tu-agent`.

Changing `internal-config` affects: `cmd-tu-agent`.

Changing `internal-stats` affects: `internal-bench`, `cmd-tu-agent`.

Changing `internal-bench` affects: `cmd-tu-agent`.

## Change-Impact Queries
For file- or symbol-level precision beyond the domain-level Blast Radius above,
query the graph (authoritative for structure — callers, dependents, tests):
- If you have the graph tools:  get_context(<file-or-symbol>) (also get_impact, find_symbol)
- Otherwise, via CLI:           tu-agent graph context <file-or-symbol>
                                tu-agent graph impact  <symbol>
get_context returns blast radius (dependents), the relevant concept card(s),
conventions, and tests to run — pointers, not source.
