# MCP tools: `tu-agent-graph`

Installing the plugin registers a stdio MCP server called **`tu-agent-graph`**.
Claude Code starts it automatically; it runs the bundled binary against the repo
you opened Claude Code in. These tools are how the agent reads the graph and
memory.

**You almost never call these by name.** The agent calls them for you because
`CLAUDE.md` tells it to consult the graph before editing and to recall memory
before non-trivial work. This page exists so you understand what it's doing — and
so you can ask for a specific query when you want one.

The tools read `./.tu-agent/graph.db` and `./.tu-agent/memory.db`. The server
refreshes the graph from the working tree each time it starts, so results track
your current code. If a graph is missing, build it with `/tu-agent:learn`.

> The examples below use a fictional Go service `github.com/acme/orders`.
> Output is illustrative — shapes are real, values are made up.

---

## Graph query tools

### `get_context` — everything relevant to touching a target

The one you'll see most. Given a file or symbol, it returns the blast radius, the
relevant concept(s), conventions, and which tests to run — pointers, not source.

```
get_context("orders.Service.Charge")
→ Concept: orders — order lifecycle and payment capture
  Blast radius: 6 dependents (Refund, Reconciler.Settle, 2 handlers, 2 tests)
  Conventions: errors wrapped with %w; table-driven tests in *_test.go
  Memory: decision/idempotent-charge — "Charge dedupes on idempotency key…"
  Tests to run: ./orders/...
```

### `get_impact` — blast radius

*What breaks if I change this symbol?* The dependents, transitively, with a count
and a truncation flag on large fan-out.

```
get_impact("payment.Gateway.Authorize")
→ Direct dependents (4): Service.Charge, Service.Refund, Reconciler.Settle, TestCharge_Declined
  Blast radius: 11 symbols across 3 packages
```

### `find_symbol` — where is it defined?

```
find_symbol("InvoiceService")
→ com.acme.billing.InvoiceService — billing/InvoiceService.java:23 (class)
  3 methods: issue(), void(), reconcile()
```

### `get_flow` — execution call tree

The call tree from an entry symbol: boundary crossings, interface-dispatch
candidates, with depth and fan-out control. Great for "how does this flow work".

```
get_flow("handlers.CreateOrder")
→ handlers.CreateOrder
    → orders.Service.Charge
        → payment.Gateway.Authorize   [interface: Gateway → StripeGateway, MockGateway]
        → ledger.Record
    → orders.Repository.Save            [boundary: DB]
```

### `get_traits` — interfaces and implementers

Two views: `as_type` (shared interfaces + logic sites for a concrete type) and
`as_interface` (implementers + blast radius for an interface). JSON.

```
get_traits("payment.Gateway", as_interface)
→ implementers: StripeGateway, MockGateway, RetryingGateway
  blast radius if the interface changes: 8 symbols
```

### `get_bridges` — chokepoint / bridge nodes

Symbols that connect otherwise-separate parts of the graph — the risky, high-leverage
spots. Useful before a refactor.

### `get_cycles` — dependency cycles

Reports import/call cycles worth breaking.

### `get_architecture` — the architecture overview

The synthesized, top-level overview stored in the graph (not a file). *"Give me
the architecture"* routes here.

### `get_concept` — a concept card, or list all

```
get_concept("billing")
→ billing — issuing and reconciling invoices
  Landmarks: InvoiceService, Reconciler, LedgerEntry
  Vocabulary: invoice, settle, ledger, dunning, void
```

---

## Test-intelligence tools

### `test_gaps` — rank untested code by risk

Ranks untested public functions by **fan-in × blast radius** — the risk metric a
grep-only agent can't reconstruct. This is what `test-gen` uses to pick targets.

```
test_gaps()
→ 1. payment.Gateway.Authorize   fan-in 7, blast 11 — untested
  2. billing.Reconciler.Settle    fan-in 4, blast 9  — untested
  3. orders.Service.Refund        fan-in 3, blast 5  — partial
```

### `test_scaffold` — the deterministic half of test-gen

Given a target, returns the context, the conventional test-file path, and the
scoped run command. Claude Code then writes the test body.

### `test_mutation` — mutation testing

Runs mutation testing (go-mutesting / PIT / mutmut / Stryker, detect-or-degrade)
to check whether your tests actually catch regressions, not just cover lines.

---

## Memory tools (`mem_*`)

The agent calls these to recall the *why* and to save new decisions. See
[Memory](memory.md) for the full model.

| Tool | What it does |
|------|--------------|
| `mem_recent` | Recall recently saved notes (called at session start). |
| `mem_search` | Find notes relevant to a feature area (called before non-trivial work). |
| `mem_save` | Save a decision, bug-pattern, gotcha, etc. with a topic key and type. |
| `mem_related` / `mem_relate` | Find or create links between notes and graph nodes. |
| `mem_export` / `mem_import` | Sync memory to/from committable chunks (team sharing). |
| `mem_session_start` / `mem_session_end` / `mem_session_list` | Group a session's notes. |
| `mem_clusters` / `mem_conflicts` / `mem_reconcile` | Cluster related notes, surface contradictions, reconcile them. |
| `mem_rescope` / `mem_delete` | Move a note between scopes, or soft-delete it. |
| `crystallize_save` | Persist a crystallized skill from a note cluster. |

There are also `advise`, `set_architecture`, and `set_concept_definition` tools
used by the setup and dev-flow skills — you won't call these directly.

---

## Seeing the tools yourself

The MCP tools may be **deferred** in a session — present but not in the active
tool list until loaded. That's normal. If you're scripting against the binary
instead, every graph query has a CLI equivalent:

```bash
tu-agent graph context orders.Service.Charge
tu-agent graph impact payment.Gateway.Authorize
tu-agent graph flow handlers.CreateOrder
tu-agent graph find InvoiceService
```

---

## Next

- How the dev-flow uses these → [Dev-flow](dev-flow.md)
- Recipes that combine them → [Cookbook](cookbook.md)
