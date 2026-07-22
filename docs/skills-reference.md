# Skills reference

Everything you can run is a **skill** — invoke it by typing `/tu-agent:<name>` in
Claude Code, or just describe what you want and the agent picks the matching
skill. This page lists every skill, grouped by what you're doing, with a
one-line *when to use it*.

A skill is a packaged discipline: instead of the agent improvising, it follows an
explicit, repeatable procedure. That's why the results are consistent across
people and sessions.

Jump to: [Setup & knowledge](#setup--knowledge) ·
[Dev-flow](#dev-flow-build-a-change) · [Quality & verification](#quality--verification) ·
[Learning & memory](#learning--memory)

---

## Setup & knowledge

These build and maintain the knowledge index. Run `prepare` once per repo; the
rest keep it healthy.

| Skill | When to use it |
|-------|----------------|
| **`prepare`** | First time on a repo. End-to-end setup: `CLAUDE.md` knowledge block, hardened `settings.json`, seeded `.tu-agent/`, then the `learn` pipeline if the index is empty. |
| **`learn`** | (Re)build the knowledge index — graph + per-concept cards + one-line definitions + architecture overview. Run after big structural changes. |
| **`synthesize`** | Regenerate just the architecture overview from the existing concept index, without re-running the whole `learn` pipeline. |
| **`status`** | Check whether the index is stale and see graph health (node count, last build, card staleness). |

**Example — onboard a repo:**

```
/tu-agent:prepare
```

Then verify:

```
/tu-agent:status
→ Graph: 3,902 nodes, built 2 minutes ago (fresh)
  Concepts: 41 cards, all defined
  Architecture overview: present
```

---

## Dev-flow: build a change

Pick the entry point by **size of the change**. They form a chain:
`groundwork` (single change) → `design` (new feature/subsystem from zero) →
`tdd` (full implementation). See [Dev-flow](dev-flow.md) for the deep dive.

| Skill | When to use it |
|-------|----------------|
| **`groundwork`** | A single non-trivial change (one file, one edit, one flow). Anchors in the graph + memory, asks only what the code can't answer, confirms the approach, then builds. **The default before coding.** |
| **`design`** | A new feature or subsystem designed from zero — architecture, greenfield, brainstorm — before any spec or code. A panel of production "lenses" critiques your design; **you** pick what enters it. |
| **`tdd`** | Build a *whole* feature end-to-end under strict TDD: interrogation → signed spec → Gherkin scenarios → deterministic RED/GREEN gates → design judge → optional mutation hardening → whole-branch review → memory archive. Heavyweight; for a quick change use `groundwork`. |
| **`test-gen`** | Generate unit tests — for one function, a whole class, a flow, a package, or the riskiest untested code. Graph picks the risk ranking; Claude Code writes the test body. |
| **`crystallize`** | Consolidate a dense cluster of related memory notes into a reusable **project skill** — the "start here" standard for a task area. |

**Example — a small change (the common case):**

```
/tu-agent:groundwork
→ (anchors in graph + memory, asks: "Should Refund reuse the idempotency
   key from Charge, or mint a new one?")
```

**Example — generate tests for the riskiest untested code:**

```
/tu-agent:test-gen
→ Ranks untested public functions by fan-in × blast radius.
  Top risk: payment.Gateway.Authorize (fan-in 7, blast radius 11) — untested.
  Generates orders/payment_gateway_test.go with a verified passing test.
```

---

## Quality & verification

These are explicit disciplines for the parts of engineering that are easy to do
sloppily. Reach for them by name; several are also enforced automatically by the
`tdd` flow.

| Skill | When to use it |
|-------|----------------|
| **`security-review`** | Review changes, a branch, or an area for vulnerabilities. Deterministic scoping (diff + graph blast radius), threat-model-first sweep with per-language checklists, and **adversarial verification of every finding** before it's reported. Report-only — never auto-fixes. |
| **`systematic-debugging`** | Any bug, test failure, or unexpected behavior — **before** proposing a fix. Reproduce first, gather evidence, test hypotheses by prediction, confirm the root cause adversarially. No fix without a confirmed cause. |
| **`verification-before-completion`** | About to say "done / fixed / passing"? Run the project's verification fresh, read the actual output, report verbatim. Evidence before assertions. |
| **`verify-in-env`** | Prove a change works in the *running* system — launch the app, drive the affected flow end-to-end, report observed behavior. Learns the launch recipe once, saves it to memory. |
| **`refine`** | After code is green, before review/merge: a behavior-preserving cleanup of the diff — reuse over duplication, right altitude, dead weight removed. Quality only; a bug found here is reported, not fixed. |
| **`performance-investigation`** | Something is slow or heavy — **before** changing code. Requires a baseline, a profile of the dominant cost, one change at a time, and a re-measurement proving the delta. Refuses speculative optimization. |
| **`receiving-code-review`** | Processing review feedback (human, agent, or judge). Verify each finding against the code before implementing; push back with evidence on wrong ones instead of performative agreement. |
| **`finishing-a-development-branch`** | Work on a branch is done and verified. Preflight (verification, review, memory export), then presents merge / PR / keep / discard — **you** choose. Never integrates on its own. |

**Example — review before merge:**

```
/tu-agent:security-review
→ Scopes to the branch diff + graph blast radius.
  Finding (verified): user-supplied `orderID` reaches an fmt.Sprintf SQL
  string in billing/query.go:44 — SQL injection (CWE-89).
  2 candidate findings refuted during verification and dropped.
```

---

## Learning & memory

| Skill | When to use it |
|-------|----------------|
| **`sensei`** | You want something **explained, not changed** — a concept, a file, an execution flow, a past decision, "how does X work". Taught at junior level, anchored in the graph and memory, with concrete `file:line` examples from this repo. Read-only. |
| **`retro`** | End of a work session. Reflect on it — classify re-prompts, corrections, guardrail violations — and capture the durable behavioral patterns as memory notes so the next session starts smarter. |
| **`crystallize`** | (Also listed under dev-flow.) Turn a cluster of memory notes into a reusable skill. |

**Example — understand an unfamiliar area:**

```
/tu-agent:sensei how does the payment retry flow work?
→ Anchors in the graph (get_flow from the charge entry point) and memory
  (the note explaining the 3-retry decision), then explains it in plain
  prose with the exact file:line landmarks.
```

---

## The 4 slash commands vs. the full skill set

The plugin advertises **four headline commands** — `prepare`, `learn`,
`synthesize`, `status` — because those are the setup lifecycle. But **every skill
above is invokable** the same way (`/tu-agent:<name>`), and the agent will also
reach for the right one on its own when your request matches. You rarely need to
memorize names; describing the task is enough.

---

## Next

- The graph queries behind these skills → [MCP tools](mcp-tools.md)
- How the dev-flow chain and its agents work → [Dev-flow](dev-flow.md)
