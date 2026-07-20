---
name: groundwork
description: Lightweight anchor BEFORE a single non-trivial change (one file, one edit, one flow) — grounds in the project graph and memory, asks the human only what code can't answer, confirms the approach, and captures new gotchas/decisions. For a whole feature built spec-to-merge under strict TDD, use tdd instead. Keywords - build, implement, create, add feature, new flow, start a task, before coding, groundwork.
---

# groundwork — anchor & confirm before building

The default posture for build/task work in a tu-agent-prepared repo. Stops you
from acting on assumptions and captures what you learn as you go. Anchoring is
deterministic (graph + memory, via MCP/CLI); the judgment calls go to the human.

## When this applies (and when to skip)

Apply to: creating a new file, implementing a feature or execution flow, writing
tests, refactors, or any non-trivial change in an area whose conventions you
have not already established this session.

Skip — just do it, no ceremony: one-line edits, renames, formatting, reverts,
reading, or answering a pure question.

If the user says "just do it" / "skip the questions", back off — and remember
that preference for the session.

## 1. Anchor first — silent, no questions yet

Before asking the human anything, gather what the repo already knows:

- `get_context(<file-or-symbol>)` — blast radius, the relevant concept(s),
  conventions, and tests to run. (Via CLI: `tu-agent graph context <x>`.)
- `mem_search <feature area>` — prior decisions, gotchas, and bug-patterns that
  carry the "why".
- Check for a project skill that already governs this area (a crystallized
  standard). If one exists, LOAD it and follow it — do not re-derive.

This step is cheap and must not be skipped: it is exactly the graph/memory that
gets ignored when you start from assumptions. If the anchor already answers the
approach, say so in one line and proceed — no interrogation needed.

## 2. Interrogate only the gaps — what only the human can answer

Ask the human ONLY what the code, graph, and memory cannot answer: intent,
scope, preferences, product decisions. Targeted questions, never a quiz. If the
anchor surfaced no gaps, skip this step.

Every question carries your reasoning and a recommendation — never a bare
"what should I do?".

### When the human is unsure
Switch from interrogator to advisor:
- Explain the tradeoffs, grounded in what the repo already does (from the
  anchor) — never invented.
- Propose a sensible default, state it explicitly, and proceed — marked
  revisable ("going with X because Y; we can change it").
- When the answer is genuinely unknowable up front, offer a spike ("let's try X
  small and see") instead of forcing a blind decision.
- Never freeze waiting; never silently pick.

## 3. Confirm the approach

State the plan + reasoning + recommendation, and wait for sign-off before
building. For a SUBSTANTIAL feature, do not improvise it here — hand off to the
`tdd` dev-flow (interrogation → signable Gherkin → strict TDD with a gate).
If the feature or subsystem is being designed FROM ZERO — no clear shape yet —
hand off to `/tu-agent:design` first; its architecture guild produces the
design doc that seeds `tdd`'s interrogation. Go straight to `tdd` only when the
shape is already clear.

## 4. Build incrementally

Build in steps the human can redirect. No big-bang change dumped at once.

## 5. Capture as you learn

The moment you hit a surprise, get corrected, or make a decision, `mem_save` an
atomic note right then:
- a trap → `type: gotcha`, one trap per note;
- a decision → `decision/<topic>` with Decision → Why → Alternatives → Scope;
- if the human was unsure and you went with a default, mark it tentative.

A captured answer becomes a note; next time, the anchor (step 1) finds it and
you stop re-asking. groundwork gets quieter as memory fills.

## Red flags — stop and re-anchor

| The excuse | Why it is wrong |
|---|---|
| "I already know how this area works" | Knowing the *language* is not knowing *this repo's* conventions. The anchor costs one query. |
| "The user is in a hurry, skip the anchor" | Skip the *questions*, never the anchor — it is silent and cheap; being wrong is not. |
| "The graph returned (none), so nothing depends on this" | Cross-check with a targeted grep before concluding — the graph can miss framework/DI edges. |
| "I'll save the gotcha when the task is done" | Later never comes. Save it the moment it bites (step 5). |
| "Memory says X but my approach differs slightly" | Deviating from a recalled decision silently reverses it. Say so explicitly and let the human decide. |
