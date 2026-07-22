# Cookbook

End-to-end recipes that combine the pieces. Each one is a realistic task with the
skills and tools you'd actually use, on the fictional `github.com/acme/orders`
service. Keep this page open while you work.

- [1. Onboard a new repo](#1-onboard-a-new-repo)
- [2. "What breaks if I change X?"](#2-what-breaks-if-i-change-x)
- [3. Make a small change safely](#3-make-a-small-change-safely)
- [4. Build a whole feature with TDD](#4-build-a-whole-feature-with-tdd)
- [5. Understand an unfamiliar area](#5-understand-an-unfamiliar-area)
- [6. Add tests to risky untested code](#6-add-tests-to-risky-untested-code)
- [7. Review a branch before merge](#7-review-a-branch-before-merge)
- [8. Debug a failing test](#8-debug-a-failing-test)
- [9. Share what you learned with the team](#9-share-what-you-learned-with-the-team)

---

## 1. Onboard a new repo

**Goal:** get tu-agent working on a repo nobody has set up yet.

```
/tu-agent:prepare
```

Then confirm it built:

```
/tu-agent:status
→ Graph: 3,902 nodes (fresh) · Concepts: 41 cards · Architecture: present
```

Commit the generated `.claude/settings.json` and `CLAUDE.md` so the whole team
inherits the same hardened setup. Done once per repo.

---

## 2. "What breaks if I change X?"

**Goal:** the classic question, answered without re-reading the codebase.

Just ask:

> **You:** What breaks if I change the return type of `payment.Gateway.Authorize`?

The agent calls `get_impact` and answers with the real blast radius — the direct
dependents, the transitive count, and the tests to run. No guessing, no grep
marathon. See the example output in [MCP tools → get_impact](mcp-tools.md#get_impact--blast-radius).

If the graph returns `(none)` for a symbol you *know* is used, that's the
framework/DI blind spot — ask the agent to cross-check with a targeted search.

---

## 3. Make a small change safely

**Goal:** a one-file change without coding from assumptions.

```
/tu-agent:groundwork
```

The agent anchors in the graph and memory, then asks only what the code can't
tell it — e.g. *"Should `Refund` reuse the idempotency key from `Charge`, or mint
a new one?"* You answer, it confirms a short plan, you approve, it builds.

When it's done and you're about to call it finished:

```
/tu-agent:verification-before-completion
→ Runs the project's test command fresh, reads the actual output,
  reports: PASS ./orders/... (14 tests) — before claiming done.
```

---

## 4. Build a whole feature with TDD

**Goal:** a complete feature, spec to merge, under strict TDD.

```
/tu-agent:tdd
```

Walk through the stages (see [Dev-flow → tdd](dev-flow.md#tdd--a-whole-feature-spec-to-merge)):

1. **Interrogation** — you answer requirement questions up front.
2. **Sign the spec + Gherkin scenarios** — you approve `Given/When/Then` before
   any code exists.
3. **RED / GREEN** — the flow writes a failing test, then the implementation,
   each gated deterministically by the binary.
4. **Design judge** — reviews the design; can send it back citing a project rule.
5. **(Optional) mutation hardening** — proves the tests catch regressions.
6. **Whole-branch review** + **memory archive**.

Long build? It's resumable — `state.json` survives restarts, and you can have
multiple features in flight.

When the branch is done:

```
/tu-agent:finishing-a-development-branch
→ Preflight (verification, review, memory export), then offers:
  [merge] [open PR] [keep] [discard] — you choose.
```

---

## 5. Understand an unfamiliar area

**Goal:** learn how something works, without changing it.

```
/tu-agent:sensei how does the payment retry flow work?
```

`sensei` anchors in the graph (`get_flow` from the charge entry point) and memory
(the note explaining the retry decision), then teaches it at junior level — plain
prose, one idea per sentence, concrete `file:line` landmarks. Read-only. If it
uncovers an undocumented *why*, that gets fed back to memory.

---

## 6. Add tests to risky untested code

**Goal:** spend test effort where it matters most.

```
/tu-agent:test-gen
```

The graph ranks untested public functions by **fan-in × blast radius** (via
`test_gaps`) — the risk signal a grep-only agent can't compute. It picks the top
risk, generates a test at the conventional path, and verifies it passes before
handing it back. Your hand-written code is never touched; generated tests carry a
`_gen` marker.

Want a specific target instead? *"Generate tests for `InvoiceService`"* works too.

---

## 7. Review a branch before merge

**Goal:** catch correctness and security issues before they ship.

```
/tu-agent:security-review
```

It scopes deterministically to the branch diff plus the graph blast radius, runs
a threat-model-first sweep with per-language checklists, and — importantly —
**adversarially verifies every finding** before reporting it, dropping the ones
that don't survive. It's report-only; it never auto-fixes.

For a general quality pass on the diff (not bug-hunting), use `refine` first to
clean it up, then review.

When findings come back, process them with rigor:

```
/tu-agent:receiving-code-review
→ Verifies each finding against the code before implementing;
  pushes back with evidence on any that are wrong.
```

---

## 8. Debug a failing test

**Goal:** find the real root cause, not a plausible-looking patch.

```
/tu-agent:systematic-debugging
```

The discipline: reproduce first, gather evidence, form a hypothesis, **predict**
what you'd see if it were true, test the prediction, and confirm the root cause
adversarially — *before* any fix. No fix without a confirmed cause. This is what
stops the "change something, hope it works" loop.

---

## 9. Share what you learned with the team

**Goal:** make a decision you made today available to a teammate tomorrow.

You usually don't have to do anything — when you `mem_save` a decision, the
`Stop`/`SessionEnd` hook exports it to your chunk automatically. Then:

```bash
git add .tu-agent/memory/chunks/
git commit -m "chore(memory): capture idempotent-charge decision"
git push
```

Your teammate pulls, and at their next session start the `SessionStart` hook
imports your note. It shows up in their `mem_recent`. See
[Memory → Team sync](memory.md#team-sync-memory-travels-through-git).

End a heavy session with a retrospective to capture behavioral lessons too:

```
/tu-agent:retro
→ Classifies re-prompts and corrections from the session, saves the
  durable patterns as memory notes so next session starts smarter.
```

---

## The habit loop

The short version of all nine recipes:

1. **Anchor** before touching code — the agent queries the graph and recalls
   memory for you.
2. **Ask** only what the code can't answer.
3. **Build** with the right-sized skill (`groundwork` / `design` / `tdd`).
4. **Verify** with real evidence before claiming done.
5. **Capture** the *why* so the team inherits it.

---

## Back to

- [Documentation home](README.md)
- [Skills reference](skills-reference.md) · [MCP tools](mcp-tools.md) · [Memory](memory.md)
