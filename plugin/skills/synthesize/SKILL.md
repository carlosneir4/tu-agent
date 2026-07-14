---
name: synthesize
description: Use when the user wants to (re)generate the architecture overview skill from the existing concept index, without re-running the full learn pipeline. Keywords - synthesize, architecture, overview, blast radius, tu-agent.
---

# tu-agent synthesize (plugin)

Regenerates the architecture overview (stored in the graph store, read via
`get_architecture`) and the CLAUDE.md knowledge block from the concept cards
already in the graph store.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"`.

1. **Preflight:** run `"$TU" version`; require ≥ 0.3 (see install
   instructions in the shim error if missing).
2. **Check inputs:** the graph store must hold at least one concept — call the
   `get_concept` MCP tool (no `name`) and confirm it lists one or more. If none,
   STOP: "no concepts found — run /tu-agent:learn first".
3. Run `"$TU" graph build` to refresh the graph (structure lives in `graph.db`;
   the synthesizer queries it via `get_context`/`get_impact` or
   `tu-agent graph context|impact`).
4. Dispatch the `architecture-synthesizer` agent.
5. Relay its one-line result to the user.
