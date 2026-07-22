# tu-agent documentation

tu-agent is a Claude Code **plugin** that gives your AI coding assistant a
durable, queryable model of your codebase — a dependency graph, a concept index,
and persistent project memory — so it stops re-reading files every session.

Everything deterministic (graph queries, clustering, memory, test-gap analysis)
runs locally in a bundled binary. Everything generative (concept definitions,
architecture synthesis, code, test bodies) runs in Claude Code on your existing
subscription. **No API key.**

---

## Read in this order

| # | Page | You'll learn |
|---|------|--------------|
| 1 | [Getting started](getting-started.md) | Install the plugin, prepare a repo, run your first graph query — end to end in ~5 minutes. |
| 2 | [Mental model](mental-model.md) | The three "organs" of project knowledge — graph, concept index, memory — and which question goes where. Read this once and everything else clicks. |
| 3 | [Skills reference](skills-reference.md) | Every `/tu-agent:*` command and skill, grouped by what you're doing, with a when-to-use for each. |
| 4 | [MCP tools](mcp-tools.md) | The `tu-agent-graph` tools the agent calls for you (`get_impact`, `get_context`, `get_flow`, …) — each with an example call and output. |
| 5 | [Dev-flow](dev-flow.md) | The `groundwork → design → tdd` chain, the dev-flow agents, and how project rules steer them. |
| 6 | [Memory](memory.md) | How persistent memory works: note types, the capture protocol, and team sync through committed chunks. |
| 7 | [Cookbook](cookbook.md) | End-to-end recipes: onboard a repo, "what breaks if I change X", add a feature with TDD, run a security review. |

New to tu-agent? Do [Getting started](getting-started.md), skim the
[Mental model](mental-model.md), then keep the [Cookbook](cookbook.md) open while
you work.

---

## The 30-second version

1. Open Claude Code in your repo and install the plugin (once):

   ```
   /plugin marketplace add carlosneir4/tu-agent
   /plugin install tu-agent@tools
   ```

2. Build the knowledge index (once per repo):

   ```
   /tu-agent:prepare
   ```

3. Work as usual. When you ask *"what breaks if I change `Service.Charge`?"* the
   agent queries the graph instead of re-reading your repo — cheaper, and more
   complete.

---

## What you get, at a glance

- **A dependency graph** of your code (`calls` / `implements` / `extends` /
  `imports` / `overrides` edges) in SQLite, rebuilt incrementally from a diff.
  Answers *what breaks if I change X, who calls this, what's the call flow.*
- **A concept index** — one thin card per concept (usually a package): a
  vocabulary → landmarks map. Answers *what does this part of the system mean.*
- **Persistent memory** — durable, topic-keyed facts with provenance, shared
  across the team through git. Answers *what did we decide / learn here.*
- **A dev-flow** — `groundwork`, `design`, and a strict multi-agent `tdd`
  pipeline with deterministic RED/GREEN gates.
- **Quality skills** — security review, systematic debugging, verification,
  refine, performance investigation — each an explicit, repeatable discipline.

> All examples in these docs use a fictional codebase — a Go HTTP service
> `github.com/acme/orders` and a Java service `com.acme.billing` — so they read
> cleanly for any project.
