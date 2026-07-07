---
name: status
description: Use when the user asks whether generated tu-agent skills are stale, or wants to check knowledge index health. Keywords - status, stale, skills, graph, tu-agent.
---

# tu-agent status (plugin)

Reports skill staleness and graph health. Read-only.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"`.

1. **Preflight:** run `"$TU" version`; require ≥ 0.3.
2. Run `"$TU" learn status` and show the output (fresh vs stale concept cards).
   If it errors with "no concepts found — run 'tu-agent learn <path>' first",
   tell the user to run /tu-agent:learn first.
3. Run `"$TU" graph status` and show the output (graph size, failed files,
   extractor version).
