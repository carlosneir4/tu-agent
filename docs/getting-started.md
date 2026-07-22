# Getting started

This page takes you from zero to a working knowledge index in about five
minutes. You need [Claude Code](https://claude.com/claude-code) and a git repo.

---

## 1. Install the plugin

From a Claude Code session opened in any repo:

```
/plugin marketplace add carlosneir4/tu-agent
/plugin install tu-agent@tools
```

That's the whole setup. On first use the bundled shim downloads the matching
binary (`tu-agent-<os>-<arch>`) from the latest GitHub Release, verifies its
checksum, and caches it at `~/.tu-agent/bin/`. **No API key, no Go toolchain, no
manual build.**

It then keeps that binary up to date automatically — a throttled, best-effort
check refreshes it when a newer release ships.

- Opt out of auto-update: `TU_AGENT_NO_AUTO_UPDATE=1`
- Change the cadence: `TU_AGENT_UPDATE_INTERVAL` (seconds, default `86400` = 1 day)

Prefer not to auto-download? Build from source and place the binary at
`~/.tu-agent/bin/tu-agent`, or point the shim at your own fork with
`TU_AGENT_RELEASE_REPO`.

Installing the plugin also registers the `tu-agent-graph` MCP server and the
`/tu-agent:*` skills automatically.

---

## 2. Prepare the repo

Open Claude Code in the repo you want to work on, then run:

```
/tu-agent:prepare
```

This does two things:

1. **Deterministic setup** (the binary): writes a `CLAUDE.md` knowledge block, a
   hardened `.claude/settings.json` (deny-wins permissions, a secret-guard hook,
   a formatter hook, an MCP allowlist), a private-by-default `.git/info/exclude`
   block, and seeds `.tu-agent/` config.
2. **Knowledge index** (if the concept store is empty): runs the `learn`
   pipeline — builds the dependency graph, clusters it into concepts, writes a
   one-line definition per concept, and synthesizes an architecture overview.

On a large repo the graph build and clustering take a little while; the one-line
definitions and architecture overview run in-session on your subscription.

> **Just the index, no repo setup?** Run `/tu-agent:learn` instead.
> **Want to check what got built?** Run `/tu-agent:status`.

Commit the generated `.claude/settings.json` and `CLAUDE.md` with your team.

---

## 3. Ask a structural question

Now work as you normally would. The difference: when a question is about
structure, dependencies, or impact, the agent queries the graph instead of
re-reading files. Try:

> **You:** What breaks if I change the signature of `Service.Charge`?

Behind the scenes the agent calls `get_impact` (or `get_context`) and answers
with the actual blast radius — the callers, the tests to run, the relevant
concept — not a guess reconstructed by grepping.

Other questions that now hit the graph:

- *Who calls `payment.Gateway.Authorize`?* → `get_impact` / `find_symbol`
- *Where is `InvoiceService` defined?* → `find_symbol`
- *What's the call flow from the HTTP handler `POST /orders`?* → `get_flow`
- *What does the `billing` package mean?* → `get_concept`
- *Give me the architecture overview* → `get_architecture`

You don't call these tools by name — the agent does, because `CLAUDE.md` tells it
to consult the graph before editing. See [MCP tools](mcp-tools.md) for what each
one returns.

---

## 4. Keep it fresh (automatic)

You don't rebuild the index by hand. The plugin's hooks keep the graph and
memory current as you work:

- **After every `Write`/`Edit`** the graph updates incrementally from the diff.
- **At session start** the graph refreshes, teammates' memory is imported, and a
  short "graph ready (N nodes)" nudge is printed.
- **At session end** your new memory notes are exported to a committable chunk.

If a graph query ever contradicts what the code plainly shows, the index may be
stale — run `/tu-agent:learn` to rebuild, then re-query.

---

## Where things live

| Path | What it is | Commit it? |
|------|-----------|-----------|
| `.tu-agent/graph/graph.db` | The dependency graph + concept index (SQLite) | No (rebuildable) |
| `.tu-agent/memory/memory.db` | Local memory database | No (gitignored) |
| `.tu-agent/share/memory/chunks/*.jsonl.gz` | Per-author memory chunks (team sync) | **Yes** |
| `.tu-agent/rules.md` | Your repo-specific dev-flow rules | **Yes** |
| `.claude/settings.json` | Hardened permissions + hooks | **Yes** |
| `CLAUDE.md` | Knowledge block + protocol (managed section) + your own rules | **Yes** |

---

## Next

- Understand *why* it's split into three stores → [Mental model](mental-model.md)
- See everything you can run → [Skills reference](skills-reference.md)
