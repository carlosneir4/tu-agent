# Dev-flow: how tu-agent builds a change

tu-agent doesn't just answer questions — it has an opinionated way to *make
changes*. Three skills form a chain, chosen by the size of what you're building.
This page explains the chain, the agents that run inside `tdd`, and how you steer
them with project rules.

```
groundwork          design                 tdd
(single change)  →  (new feature/subsystem  →  (full implementation,
                     from zero)                spec-to-merge, strict TDD)
```

You don't have to use all three. Most changes start (and often end) at
`groundwork`.

---

## `groundwork` — the default before coding

For a single non-trivial change — one file, one edit, one flow. It's lightweight
but it stops the two most common failure modes: coding from assumptions, and
missing a dependency the graph knew about.

The procedure:

1. **Anchor** — `get_context` on the target and `mem_search` on the area, plus
   load any project skill for that area. Now the agent knows the blast radius,
   the conventions, and the past decisions.
2. **Ask only the gaps** — the questions the *code can't answer* (a product
   choice, an ambiguous requirement), not questions the graph already resolved.
3. **Confirm the approach** — a short plan you approve before code.
4. **Build incrementally.**
5. **Capture** new gotchas/decisions as atomic memory notes.

Skip it only for one-line edits, renames, formatting, reverts, reading, or pure
questions.

---

## `design` — a new feature or subsystem, from zero

When there's no spec and no code yet — greenfield, architecture, brainstorm —
`design` runs a **proportionality-gated architecture guild**: a panel of
production "lenses" (reliability, security, operability, cost, …) critiques your
from-zero design. It **converses; it never decides for you.** You pick what
enters the design. The output feeds `tdd`.

"Proportionality-gated" means the depth of the critique scales with the size of
the thing — a small feature gets a light touch, a new subsystem gets the full
panel.

---

## `tdd` — a whole feature, spec to merge

The heavyweight. Use it to build an entire feature end-to-end under strict
test-driven development. It's a **deterministic state machine in the binary**
(not an LLM improvising the process) that dispatches generative work to agents at
each stage:

1. **Interrogation** → a complete spec, front-loaded so questions come before
   design, not during coding.
2. **Signed spec + Gherkin scenarios** → you approve human-readable
   `Given/When/Then` scenarios before any code.
3. **RED** → a failing test, gated deterministically (the binary checks the test
   actually fails for the right reason).
4. **GREEN** → the implementation, gated deterministically (the binary checks the
   test now passes).
5. **Design judge** → an agent reviews the design; can send it back to revise,
   citing the exact project rule or criterion it violates.
6. **Optional mutation hardening** → mutation testing to prove the tests catch
   regressions.
7. **Whole-branch review** → a review pass over the entire branch diff.
8. **Memory archive** → the decisions made are saved to memory.

The gates are deterministic on purpose: "the test is red/green" is a fact the
binary checks, not a judgment a model makes. That's what makes the flow
trustworthy.

`tdd` supports **resume** (durable state in `state.json`) and **multiple
features** in flight, so a long build survives restarts.

---

## The dev-flow agents

`tdd` composes several roles. They are **not** materialized as
`.claude/agents/*.md` files you maintain — each role is composed at runtime from
four layers:

```
embedded generic shell   (the role's base contract: tools, output format, done-definition)
  + runtime language overlay   (Go vs Java specifics)
  + your project rules         (.tu-agent/rules.md)
  + the graph's conventions    (get_context)
```

The roles:

| Role | Does |
|------|------|
| **analyst** | Interrogates requirements into a complete spec before any design or code. |
| **architect** | Strategic design, pattern evaluation, ADR authoring. |
| **developer** | Implements features and fixes bugs. |
| **pr-reviewer** | Code review: correctness, security surface, style, test coverage. |
| **qa** | Test strategy, coverage analysis, test generation. |
| **scribe** | Records what changed and why to durable memory. |
| **security-reviewer** | Security review: OWASP Top 10, secrets, injection, auth, deps. |
| **architecture-synthesizer** | Synthesizes the architecture overview from the concept index. |

**To customize a role**, drop your own `.claude/agents/<role>.md` into the repo —
it wins over the embedded shell. There's nothing to "enrich" or regenerate.

---

## Project rules — steering the flow

Two optional, **user-owned** files let you impose repo-specific rules on the
dev-flow. **No tu-agent command ever writes them**, so your edits survive every
`prepare` and update.

- **`.tu-agent/rules.md`** — rules every role must follow (conventions the
  generated agent bodies don't already encode).
- **`.tu-agent/rules/<role>.md`** — rules for one role only (roles: `analyst`,
  `architect`, `developer`, `pr-reviewer`, `scribe`).

When present, the rules are spliced into each stage prompt between the agent body
and the stage overlay (`body + rules + overlay`), so every dispatched stage sees
them. **The judge enforces them:** a change that violates a project rule is
grounds to send it back to revise, citing the exact rule.

Example `.tu-agent/rules.md`:

```markdown
# Project rules for the dev-flow

- All exported errors are wrapped: fmt.Errorf("pkg.Method: %w", err).
- No new third-party dependency without a comment justifying it.
- Handlers validate input before touching the datastore.
- Tests are table-driven; no live network calls in unit tests.
```

---

## Related quality skills

The dev-flow leans on the standalone quality skills — you can also run them on
their own:

- [`refine`](skills-reference.md#quality--verification) — clean the diff before review
- [`security-review`](skills-reference.md#quality--verification) — verified security pass
- [`verification-before-completion`](skills-reference.md#quality--verification) — the "done" gate
- [`receiving-code-review`](skills-reference.md#quality--verification) — process the judge's feedback
- [`finishing-a-development-branch`](skills-reference.md#quality--verification) — integrate when done

---

## Next

- How decisions get remembered → [Memory](memory.md)
- Full walk-throughs → [Cookbook](cookbook.md)
