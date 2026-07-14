---
name: prepare
description: Use when the user wants to set up Claude Code for a repository with tu-agent best practices — a CLAUDE.md knowledge block, a hardened settings.json, and .tu-agent data. Runs the deterministic binary setup, then (if needed) the learn pipeline. Keywords - init, setup, harden, best practices.
---

# tu-agent prepare (plugin orchestrator)

You set up a repository for Claude Code with tu-agent's best practices. The
binary does everything deterministic (a CLAUDE.md knowledge block, a hardened
`.claude/settings.json`, and seeded `.tu-agent` config); your one generative job
is running the learn pipeline when the concept store is empty (which includes
synthesizing the architecture overview).

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"` and use it for every binary call.

## Step 0: Preflight

Run: `"$TU" version`
- If it fails: STOP and show the shim's install instructions.
- Parse MAJOR.MINOR (a `-dev` suffix is fine). If older than 0.3, STOP and tell
  the user to upgrade (`go install -tags sqlite_fts5 github.com/carlosneir4/tu-agent/cmd/tu-agent@latest`).

## Step 1: Deterministic setup (binary)

Run: `"$TU" prepare --plugin` (pass the path argument the user gave, if any).
Always pass `--plugin`: the plugin's own hooks.json already supplies the
graph/memory hooks; this keeps settings.json from duplicating them.
This writes a CLAUDE.md knowledge block, a hardened `.claude/settings.json`
(deny-wins permissions, secret-guard + formatter hooks,
`enabledMcpjsonServers: ["tu-agent-graph"]`), and — **private by default** —
keeps tu-agent/Claude artifacts out of commits via `.git/info/exclude` (never
committed), and seeds `.tu-agent` config (detected language + test command). It
does **not** write dev-flow agents. If it errors, STOP and show the error.

Report which files were created vs skipped. If `settings.json` already existed,
note that the original was backed up to `.claude/settings.json.bak`.

`prepare` prints a line like `Detected language=… build-tool=… test-command="bun test"`
(informational — the test command is seeded into `.tu-agent/config.yaml`).

**Private is the default** (the safe choice for company/shared repos): the ignore
rules go to `.git/info/exclude` (local per clone, never committed) covering
`.claude/`, `CLAUDE.md`, `.mcp.json`, `.tu-agent/`, and `AGENTS.md` — so the
harness works locally and nothing, not even the ignore rules, reaches the repo
history. The one exception is `.tu-agent/memory/chunks/`, re-included so a team
can still commit shared memory.

Only if the user explicitly wants to **share** these artifacts with the team
(an OSS or knowledge-sharing repo), run `"$TU" prepare --public --plugin` — that
commits a tu-agent block to `.gitignore` instead. (The old `--private` flag is
now a deprecated no-op, since private is the default.)

If `prepare` reports that `CLAUDE.md` already existed (an already-initialized
repo), run `"$TU" prepare --update --plugin` (same path argument). This refreshes
the CLAUDE.md knowledge block in place — picking up the refreshed knowledge-block
protocol pointer from the current binary — without overwriting your own CLAUDE.md
prose. It does not run an LLM.

## Step 2: Ensure the concept store is populated

Call the `get_concept` MCP tool with no `name` to list concepts.
- If it lists one or more concepts, you are done — skip to Step 3.
- If it returns nothing (empty store), run the **learn** pipeline:
  1. Run `"$TU" learn --skip-llm` (pass the same path argument). It auto-detects
     concept roots from `package.json` `workspaces`; add `--concept-root <pkg>`
     (repeatable) only to override.
  2. List concepts again with `get_concept` (no name). For each whose
     description still contains the marker `files; landmarks:`, read it with
     `get_concept <name>`, compose ONE plain-English sentence of what it IS, and
     write it with the `set_concept_definition` MCP tool.
  3. Dispatch the `architecture-synthesizer` agent to write the architecture
     overview.

  (This is the same work as the `learn` skill; if the user prefers, they can run
  `/tu-agent:learn` separately first and then `/tu-agent:prepare` skips straight to
  Step 3.)

## Step 3: Dev-flow agents are not per-repo files

There is nothing to enrich. The tdd dev-flow does **not** materialize
`.claude/agents/*.md`. Instead, the flow composes each role at runtime from:

- an embedded generic shell (the role's base contract), plus
- the runtime language overlay, plus
- the user-owned project rules (`.tu-agent/rules.md` / `rules/<role>.md`), plus
- the project's conventions from the graph (`get_context`).

To customize a role, drop your own `.claude/agents/<role>.md` into the repo — it
wins over the embedded shell. Absent that file, the role resolves to the shell
automatically.

## Step 4: Make pre-existing custom agents graph-aware

If the repo already had its own custom sub-agents (any `.claude/agents/*.md` — a
framework or component expert the team wrote), those still lack the graph tools
and protocol. Make them graph-aware:

Run: `"$TU" prepare --augment-agents --plugin` (same path argument, if any).
This unions the graph MCP tools into each existing agent's `tools:` line and
appends a marker-delimited graph/memory protocol block to its body — additive
and idempotent, never touching the agent's custom content. It safely skips
agents whose `tools:` uses a non-inline (JSON-array or YAML block-list) form,
reporting them. Use `--exclude <name>[,<name>...]` to skip specific agents.

Skip this step if the repo has no custom agents.

## Step 5: Report

Summarize: files generated by `prepare` (and whether a `settings.json.bak` was
made), whether learn ran and how many definitions you filled, and which
pre-existing custom agents were augmented.

**Critical reminder:** CLAUDE.md and `.claude/settings.json` are read at SESSION
START. This session began before prepare wrote them, so the new permissions,
hooks, and knowledge protocol are NOT active yet. Tell the user, prominently, to
start a NEW session (or `/clear`) for the hardened settings to take effect.
