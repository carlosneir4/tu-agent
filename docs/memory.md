# Memory: the durable *why* of your project

The graph and concept index are derived from code, so they can't hold the things
code doesn't say: *why* you retry three times, *which* alternative you rejected,
*what* trap bit someone last month. That's what memory is for — curated,
durable facts that survive across sessions and, through git, across your team.

---

## What a note looks like

Each note has a **topic key**, a **type**, a body, and provenance (author,
timestamp, revisions). The topic key is a path-like label:

```
decision/plugin-first
gotcha/stale-plugin-hooks
bug-pattern/cwd-relative-stray-stores
```

### The type taxonomy

Every note is exactly one type. This is a fixed set — filters depend on it:

| Type | For | Shape the body as |
|------|-----|-------------------|
| `decision` | A choice you made | Decision → Why → Alternatives rejected → Scope |
| `bug-pattern` | A recurring bug class | Symptom/trigger → Root cause → Fix → Prevention |
| `gotcha` | A single trap that bit someone | Symptom/trigger → Root cause → Fix → Prevention. **One trap per note.** |
| `architecture` | A structural fact worth keeping | — |
| `reference` | A pointer to an external resource (URL, ticket, dashboard) | — |
| `testing` | A testing convention or fact | — |

**Lead with the symptom.** A note is found by *situation* ("the plugin nudge is
missing"), not just by keyword. Writing the symptom first makes it retrievable
when a future person hits the same wall.

> **Gotchas are their own atomic notes.** Never bury a `GOTCHA:` section inside a
> `decision` or `architecture` note — it can't be retrieved as a gotcha. One trap,
> one note, `type: gotcha`. Recall all traps with
> `tu-agent memory search --type gotcha`.

---

## The capture protocol

The `CLAUDE.md` the plugin installs wires memory into the agent's habits:

**At the start of work:**

- `mem_recent` — recall recently saved notes (runs at session start).
- `mem_search <feature area>` — before non-trivial work, pull the prior decisions
  and bug-patterns for the area you're about to touch.

**When something durable happens:**

- `mem_save` with a `decision/...`, `bug-pattern/...`, or `gotcha/...` topic and
  a one-paragraph summary. Save the durable *why*, not session chatter.

**What not to save:** anything the repo already records — code structure, git
history, what a function does. If it's derivable, the graph already has it. Memory
is for what a human decided and the code can't show.

**Naming code in notes:** don't hand-list which files a note touches. Name the
**code symbols** (type/class names) in prose — `tu-agent memory relink` derives
the file links automatically, and they stay correct when files move.

---

## Team sync: memory travels through git

This is what makes memory a *team* asset, not a personal notebook.

- The `memory.db` database is **gitignored** — it's a local cache.
- The source of truth is **per-author chunks** under
  `.tu-agent/memory/chunks/*.jsonl.gz`, which **are committed**.
- Each author writes their own chunk (no merge conflicts on a shared DB).

The lifecycle is automatic via hooks:

1. **You write notes** during a session (`mem_save`).
2. **At session end** (`Stop` / `SessionEnd` hooks) your notes are **exported**
   to your chunk.
3. **You commit** the chunk with your normal git workflow.
4. **A teammate pulls**, and at their next session start the `SessionStart` hook
   **imports** every author's chunk into their local `memory.db`.

So a decision you record today shows up in your teammate's `mem_recent` tomorrow.

> **After a manual `git pull`** mid-session, run `tu-agent memory import` to
> absorb teammates' new notes without waiting for the next session start.

---

## Reconciling and consolidating

Over time a knowledge base needs curation. tu-agent has tools for it:

- **`mem_conflicts` / `mem_reconcile`** — surface notes that contradict each
  other and reconcile them (supersede the stale one).
- **`mem_clusters`** — group related notes so you can see a theme forming.
- **`mem_rescope` / `mem_delete`** — move a note between scopes, or soft-delete a
  note that turned out to be wrong.
- **`crystallize`** — when a cluster of notes about one task area gets dense,
  consolidate them into a reusable **project skill** — the "start here" standard
  for that area. See [`crystallize`](skills-reference.md#dev-flow-build-a-change).

---

## Recall staleness

Because notes can link to graph nodes, and code changes, a recalled note might
reference a symbol that no longer exists. tu-agent flags this: a recall that
points to a deleted node is annotated `⚠ stale`, cross-checked against the current
graph — so you know to trust the *lesson* but re-verify the *location*.

---

## Quick reference

```bash
tu-agent memory recent                    # what was saved recently
tu-agent memory search "payment retry"    # find notes by query
tu-agent memory search --type gotcha      # all traps
tu-agent memory export                    # write your chunk (also automatic)
tu-agent memory import                    # absorb teammates' chunks
tu-agent memory relink                    # re-derive file links from symbol names
```

The MCP equivalents (`mem_recent`, `mem_search`, `mem_save`, …) are what the
agent calls in-session — see [MCP tools](mcp-tools.md#memory-tools-mem_).

---

## Next

- Put it all together → [Cookbook](cookbook.md)
