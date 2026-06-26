---
name: learn
description: Use when the user wants to build or refresh the project's knowledge index (tu-agent learn pipeline) inside Claude Code. Generates the concept index deterministically with the tu-agent binary into the graph store, writes one-line concept definitions, then synthesizes the architecture skill. Keywords - learn, concept index, project knowledge, tu-agent.
---

# tu-agent learn (plugin orchestrator)

You orchestrate the tu-agent knowledge pipeline. The binary does everything
deterministic; your only generative jobs are the one-line concept definitions
and the architecture synthesis dispatch.

Concept cards live in the **graph store** (`graph.db`), not as `.claude/skills/`
files. You read them with the `get_concept` MCP tool and write definitions with
the `set_concept_definition` MCP tool (both provided by the bundled tu-agent-graph
MCP server). The only skill written as a file is `architecture`.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"` and use it for every binary
call below.

## Step 0: Preflight

Run: `"$TU" version`
- If the command fails: STOP. Show the user the shim's install instructions.
- Parse MAJOR.MINOR (a `-dev` suffix is fine). If older than 0.3: STOP and tell
  the user to upgrade (`go install -tags sqlite_fts5 github.com/tu/tu-agent/cmd/tu-agent@latest`).
- The binary must support `concepts set-definition` (concepts in the store). If
  `"$TU" concepts set-definition --help` errors, the binary predates this flow —
  tell the user to upgrade.

## Step 1: Generate the concept index (deterministic)

Run: `"$TU" learn --skip-llm <path>` (the path argument the user gave, if any).
The bare command **auto-detects concept roots** from the repo's `package.json`
`workspaces`, so on a JS/TS monorepo it indexes one concept per package across
the workspaces — no manual sizing. Pass `--concept-root <pkg>` (repeatable) ONLY
to override the auto-detected set (e.g. to restrict to one subtree, or to add a
non-workspace directory). Do not re-run to "size" the index.

This builds the graph, persists one thin card per concept into the graph store
with deterministic descriptions, and registers the knowledge block in CLAUDE.md.
It does NOT write per-concept files under `.claude/skills/`. If it errors, STOP
and show the error.

## Step 2: Write the definition lines (generative, in-session)

Call the `get_concept` MCP tool with no `name` to list every concept as
`- <name>: <description>`. A concept still needs a definition when its
description is the deterministic fallback — it contains the marker
`files; landmarks:` (shape: `<package> — N files; landmarks: ...`). Skip any
whose description is already a plain-English sentence (preserved from a prior run).

For each concept that needs one:
1. Call `get_concept` with that `name` to read the full card (its landmarks and
   traits are your evidence).
2. Compose ONE plain-English sentence saying what the concept IS (domain meaning,
   not implementation).
3. Call the `set_concept_definition` MCP tool with `name` and that sentence.

Cards are ≤1 KB; doing all of them in-session is cheap. Do not dispatch subagents
for this. (If the MCP tools are unavailable in your context, fall back to the CLI:
`"$TU" concepts set-definition <name> "<sentence>"` for the write; the binary
exposes the same operation.)

## Step 3: Synthesize architecture

Dispatch the `architecture-synthesizer` agent (no special prompt — it reads the
concepts from the store via `get_concept` and queries the graph for structure,
then writes `.claude/skills/architecture/SKILL.md`). If it reports FAILED, warn
the user but do not abort: the concept cards in the store are the primary artifact.

(There are no empty per-concept skill dirs to prune in this flow — concepts never
touch `.claude/skills/`.)

## Step 4: Report

Summarize: concepts in the store (count + names), how many definitions you filled,
whether architecture synthesis succeeded. The knowledge lives in `.tu-agent/graph.db`
(concept cards + graph) plus the `architecture` skill — there is no `.claude/skills/`
concept tree to commit. Note that older repos may still have stale per-concept
`.claude/skills/<name>/` dirs from before this flow; they are inert (nothing reads
them) and can be removed with `git`.

**Critical reminder:** the knowledge protocol lives in `CLAUDE.md`, which is
loaded at SESSION START. This session began before learn updated it, so it will
NOT use the graph/concepts for structural questions. Tell the user, prominently,
to start a NEW session (or `/clear`) before asking structural questions —
otherwise they get blind grep instead of graph-guided answers.
