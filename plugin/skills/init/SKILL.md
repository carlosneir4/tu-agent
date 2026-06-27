---
name: init
description: Use when the user wants to set up Claude Code for a repository with tu-agent best practices — dev-flow agents, CLAUDE.md, a hardened settings.json, and enriched agents. Runs the deterministic binary setup, then (if needed) the learn pipeline, then enriches the dev-flow agents. Keywords - init, setup, harden, dev-flow agents, best practices.
---

# tu-agent init (plugin orchestrator)

You set up a repository for Claude Code with tu-agent's best practices. The
binary does everything deterministic (skeleton dev-flow agents, CLAUDE.md, and a
hardened `.claude/settings.json`); your generative jobs are running the learn
pipeline when the concept store is empty and dispatching the per-role enrichment.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"` and use it for every binary call.

## Step 0: Preflight

Run: `"$TU" version`
- If it fails: STOP and show the shim's install instructions.
- Parse MAJOR.MINOR (a `-dev` suffix is fine). If older than 0.3, STOP and tell
  the user to upgrade (`go install -tags sqlite_fts5 github.com/tu/tu-agent/cmd/tu-agent@latest`).

## Step 1: Deterministic setup (binary)

Run: `"$TU" init` (pass the path argument the user gave, if any).
This writes the 5 skeleton dev-flow agents under `.claude/agents/`, a CLAUDE.md,
a hardened `.claude/settings.json` (deny-wins permissions, secret-guard +
formatter hooks, `enabledMcpjsonServers: ["tu-agent-graph"]`), and — **private by
default** — keeps tu-agent/Claude artifacts out of commits via `.git/info/exclude`
(never committed). If it errors, STOP and show the error.

Report which files were created vs skipped. If `settings.json` already existed,
note that the original was backed up to `.claude/settings.json.bak`.

`init` prints a line like `Detected language=… build-tool=… test-command="bun test"`.
**Note the quoted test command** — you pass it to the enricher in Step 3. If it is
empty (`test-command=""`), use `<your test command>` as the placeholder instead.

**Private is the default** (the safe choice for company/shared repos): the ignore
rules go to `.git/info/exclude` (local per clone, never committed) covering
`.claude/`, `CLAUDE.md`, `.mcp.json`, `.tu-agent/`, and `AGENTS.md` — so the
harness works locally and nothing, not even the ignore rules, reaches the repo
history. The one exception is `.tu-agent/memory/chunks/`, re-included so a team
can still commit shared memory.

Only if the user explicitly wants to **share** these artifacts with the team
(an OSS or knowledge-sharing repo), run `"$TU" init --public` — that commits a
tu-agent block to `.gitignore` instead. (The old `--private` flag is now a
deprecated no-op, since private is the default.)

If `init` reports that `CLAUDE.md` or the agents already existed (an
already-initialized repo), run `"$TU" init --update` (same path argument). This
refreshes the managed regions in place — the CLAUDE.md knowledge block and each
agent's `tools:` line — picking up new tools and the refreshed knowledge-block
protocol pointer from the current binary **without** overwriting the
project-specific enrichment or the agent body. It does not run an LLM.

## Step 2: Ensure the concept store is populated

Call the `get_concept` MCP tool with no `name` to list concepts.
- If it lists one or more concepts, skip to Step 3.
- If it returns nothing (empty store), run the **learn** pipeline first, because
  enrichment draws the agents' Project Context from the concept cards:
  1. Run `"$TU" learn --skip-llm` (pass the same path argument). It auto-detects
     concept roots from `package.json` `workspaces`; add `--concept-root <pkg>`
     (repeatable) only to override.
  2. List concepts again with `get_concept` (no name). For each whose
     description still contains the marker `files; landmarks:`, read it with
     `get_concept <name>`, compose ONE plain-English sentence of what it IS, and
     write it with the `set_concept_definition` MCP tool.
  3. Dispatch the `architecture-synthesizer` agent to write
     `.claude/skills/architecture/SKILL.md`.

  (This is the same work as the `learn` skill; if the user prefers, they can run
  `/tu-agent:learn` separately first and then `/tu-agent:init` skips straight to
  Step 3.)

## Step 3: Enrich the dev-flow agents

**Always run this step — even on an already-initialized repo.** The enricher
OVERWRITES each agent, so re-running is how stale agents pick up template/format
updates from a newer plugin. Do NOT skip enrichment because the agents "already
look enriched": an agent written by an older plugin can be stale (old section
layout) yet still appear complete. Deterministic staleness signal — a current
dev-flow agent has a `## How to work` section, so any that lack it are stale:

```
grep -L "## How to work" .claude/agents/{architect,developer,qa,pr-reviewer,security-reviewer,analyst,scribe}.md 2>/dev/null
```

Re-enrich every role listed (and any whose Project Context is empty). When in
doubt, re-enrich all seven — it is idempotent and cheap.

The skeletons from Step 1 have an empty `## Project Context` and no graph tools.
Dispatch the `agent-enricher` agent once per role to fix that. Roles:
`architect`, `developer`, `qa`, `pr-reviewer`, `security-reviewer`, `analyst`,
`scribe`. You may dispatch them in parallel batches. Pass each enricher three
inputs: `role`, `project` (the project name), and `test_command` (the value you
noted in Step 1). Each enricher reads the curated graph-first skeleton plus the
concept cards via `get_concept`, then writes `.claude/agents/<role>.md` —
replacing the bare CLI skeleton, filling its Project Context from the cards, and
substituting `__PROJECT__`/`__TEST_COMMAND__`.

After all enrichers finish, **validate and auto-fix the output**:

```
python3 "${CLAUDE_PLUGIN_ROOT}/agent-templates/validate.py" --generated .claude/agents --fix
```

`--fix` deterministically strips stray wrapper tags (e.g. a leftover
`</content>`) — those need no re-dispatch. It then reports any file that still
has a real template token (`__PROJECT__`, `__TEST_COMMAND__`, an `ENRICH`
marker): re-dispatch ONLY those roles' enrichers and re-run until it prints `OK`.

## Step 4: Make pre-existing custom agents graph-aware

Step 3 only enriches tu-agent's own dev-flow roles. If the repo already had its
own custom sub-agents (any `.claude/agents/*.md` that is NOT one of the dev-flow
roles — e.g. a framework or component expert the team wrote), those still lack
the graph tools and protocol. Make them graph-aware:

Run: `"$TU" init --augment-agents` (same path argument, if any).
This unions the graph MCP tools into each existing agent's `tools:` line and
appends a marker-delimited graph/memory protocol block to its body — additive
and idempotent, never touching the agent's custom content. It safely skips
agents whose `tools:` uses a non-inline (JSON-array or YAML block-list) form,
reporting them. Use `--exclude <name>[,<name>...]` to skip specific agents.

It also makes the generalist dev-flow roles **defer** to those specialists: it
appends a marker-delimited `tu-agent:specialists` block to `developer.md` and
`qa.md` listing every non-dev-flow agent (name + description), so ad-hoc dispatch
routes domain work (e.g. a Next.js or GraphQL task) to the expert instead of the
generalist. The block is regenerated on each run, so re-run after adding or
removing a custom agent.

Skip this step if the repo has no custom agents beyond the dev-flow set.

## Step 5: Report

Summarize: files generated by `init` (and whether a `settings.json.bak` was
made), whether learn ran and how many definitions you filled, which dev-flow
agents were enriched, and which pre-existing custom agents were augmented.

**Critical reminder:** CLAUDE.md and `.claude/settings.json` are read at SESSION
START. This session began before init wrote them, so the new permissions,
hooks, and knowledge protocol are NOT active yet. Tell the user, prominently, to
start a NEW session (or `/clear`) for the hardened settings and enriched agents
to take effect.
