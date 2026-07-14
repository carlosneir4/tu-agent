---
name: status
description: Use when the user asks whether generated tu-agent skills are stale, or wants to check knowledge index health. Keywords - status, stale, skills, graph, tu-agent.
---

# tu-agent status (plugin)

Reports skill staleness and graph health. Read-only.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"`.

1. **Preflight:** run `"$TU" version`; require ≥ 0.3.
2. Run `"$TU" status` and show the output. It composes two sections:
   - **Graph** — graph size, failed files, extractor version.
   - **Knowledge (learn)** — fresh vs stale concept cards. If this half reports
     "no concepts found — run 'tu-agent learn <path>' first", tell the user to
     run /tu-agent:learn first.
   The command is read-only and always exits 0; a failing half is reported
   inline while the other half still renders. (The underlying `"$TU" graph status`
   and `"$TU" learn status` remain available if you need one section alone.)
