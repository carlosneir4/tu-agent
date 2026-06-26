---
name: tdd
description: Use when the user wants to build a feature end-to-end with the tu-agent TDD dev-flow inside Claude Code — interrogation to a signed spec, Gherkin scenarios, strict TDD with a deterministic gate, a design judge, optional mutation hardening, and a memory archive. Keywords - tdd, dev-flow, feature, gherkin, strict TDD, craftsman, judge.
---

# tu-agent tdd dev-flow (plugin orchestrator)

You are the conductor of the tu-agent TDD dev-flow. The binary supplies the
deterministic gates, the role agents, and memory; you sequence the stages and
route on each stage's contract. Same on-disk artifacts as the CLI:
`.tu-agent/tdd/spec.md`, `.tu-agent/tdd/features/<name>.feature`,
`.tu-agent/tdd/progress/`.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"` and use it for every binary call.

## Step 0: Preflight — verify the dev-flow agents exist

Run: `"$TU" version` — if it fails, STOP and show the shim's install instructions.

Run: `"$TU" tdd check`. If it exits non-zero, the project's dev-flow agents are
not provisioned — tell the user to run `/tu-agent:init` (or `tu-agent init`) and
STOP. This flow never generates agents; init owns that.

Run `"$TU" tdd status`. If its JSON has `"resumable": true`, show the user the
done/pending features and ask **resume or restart**. On **resume**, skip Steps 1–3
and 3.5 and jump straight to Step 5's feature loop at the first `pending` feature.
On **restart**, proceed normally (a fresh `tdd state begin` later overwrites it).

Read `.tu-agent/config.yaml` if present and note `tdd.mutation` (default off),
`tdd.archive` (default on), and `tdd.test_command`.

## Stage dispatch model — read this carefully

Every non-interactive stage is run by dispatching the **`general-purpose`** agent
with the composed stage prompt as its instructions. Fetch the prompt with
`"$TU" tdd prompt <stage>` — the binary composes the project's enriched agent body
(its knowledge) plus the generic TDD overlay (the contract). Prepend it to any
runtime specifics (feature name, prior gate/judge feedback). This depends on NO
agent being registered, so it works identically in a fresh session and right after
`tu-agent init` (which is exactly when named agents are not yet dispatchable).

Stages run this way: `architect`, `craftsman`, `judge`, `scribe`.

The **analyst** (Step 1) and the **human gate** (Step 3) are interactive — a
dispatched agent cannot ask the user and wait — so YOU, the conductor, run them
yourself in this conversation (the analyst using `"$TU" tdd prompt analyst` for its
style and project knowledge). Never dispatch them.

"**run the <stage> stage**" means: dispatch `general-purpose` with
`"$TU" tdd prompt <stage>` as its instructions, plus the task described.

## Step 1: Analyst — you conduct this yourself (do NOT dispatch)

If `.tu-agent/tdd/spec.md` already exists from a prior run, show it to the user and ask
whether to reuse it (skip to Step 2) or start the interrogation over. Otherwise:

YOU act as the analyst, because a dispatched sub-agent cannot talk to the user.
Fetch `"$TU" tdd prompt analyst` for the interrogation style and project knowledge, then interview
the user directly, **one question at a time**: what they want to build, the
contract (inputs/outputs/behavior), edge cases, and the reasons behind decisions.
If the user gave no feature description when invoking the skill, open by asking
what they want to build. Keep going until the spec is complete; then write it to
`.tu-agent/tdd/spec.md`. Do not invent answers — the user is the source of truth.
Only once the spec is complete, continue to Step 2.

## Step 2: Architect

Run the architect stage (dispatch `general-purpose` with `"$TU" tdd prompt architect`). It reads the spec, writes one
`.tu-agent/tdd/features/<name>.feature` per feature with `@s1..@sn` tagged
scenarios, and returns a contract with a `features` array of
`{name, scenarios}` objects (one entry for standard complexity, several for
complex), plus a `complexity` of `trivial`, `standard`, or `complex`.

(The binary also accepts the legacy `handoff`+`scenarios` contract form for
backward compatibility, normalized into a single feature.)

## Step 3: Human gate (with design loop-back)

Show the user **all** features and their scenarios (iterate the `features` array).
Ask them to **approve**, **abort**, or **describe what to change**. Then:

- approve → continue to Step 3.5.
- abort → STOP.
- describe a change → re-dispatch the architect (`general-purpose` with
  `"$TU" tdd prompt architect`) with the user's feedback prepended ("The user
  rejected the previous design: <feedback>. Revise accordingly."), then show the
  revised features and ask again. Allow up to **3 design rounds**; if still not
  approved, STOP.

## Step 3.5: Branch and ticket

After the user approves the scenarios, before any TDD:

1. Ask the user for an optional ticket id (e.g. `JIRA-1234`), or "none".
2. Propose a feature branch name that weaves the ticket and a slug of the feature name
   (e.g. `feat/<ticket>-<slug>`; drop the ticket segment if none). Show it and let the user
   **confirm or edit** it — do not assume a fixed convention.
3. Create and check out the branch: `git checkout -b <name>` (this carries the already-written
   spec and feature files into the new branch). If the user is already on a suitable feature
   branch, ask whether to use it as-is instead.
4. If a ticket was given, record it in the `.tu-agent/tdd/spec.md` header and prefix the commit
   messages you suggest with it.

(Run-state recording happens in Step 5, not here, so the trivial path never
leaves a tracked run open.)

## Step 4: Trivial path

If `complexity` is `trivial`: run the craftsman stage (dispatch `general-purpose`
with `"$TU" tdd prompt craftsman`) to make the change keeping
existing tests green, then run
`"$TU" tdd gate --feature <name> --covered <scenarios>`. If the JSON has
`"ok": true`, report done. Otherwise show the feedback and stop. The trivial path
writes no run state.

## Step 5: Standard loop — outer feature loop + inner retry budget

**Record the run first (fresh runs only).** If you are NOT resuming, run
`"$TU" tdd state begin` with one `--feature <slug>` per approved feature (from the
architect's `features` array), plus `--branch <name>` and `--task "<short
description>"`. On resume the state already exists — skip this.

Then obtain the pending features from `"$TU" tdd status` (the `pending` list). For
each pending feature in order, run the **inner standard loop** below (retry
budget 3). After the feature reaches a terminal state, call
`"$TU" tdd state mark <slug> pass` on success or
`"$TU" tdd state mark <slug> blocked` on failure. Stop the entire run on the
first blocked feature — remaining features stay `pending` and are resumable in
the next session.

**Inner standard loop for one feature (retry budget 3):**

Repeat up to 3 times:

1. Run the craftsman stage (dispatch `general-purpose` with
   `"$TU" tdd prompt craftsman`; pass any prior gate/judge feedback). It implements
   by strict TDD and returns a contract listing the `@s` tags it covered.
2. Run `"$TU" tdd gate --feature <name> --covered <the craftsman's scenarios>`.
   - Non-zero exit / error: the gate could not run — show the error and stop.
   - `"ok": false`: feed the `feedback` back to the craftsman and loop (consume one budget).
   - `"ok": true`: continue.
3. Run the judge stage (dispatch `general-purpose` with `"$TU" tdd prompt judge`). On its verdict:
   - `revise`: feed the feedback back to the craftsman and loop (consume one budget).
   - `fail`: call `"$TU" tdd state mark <slug> blocked` and stop the entire run.
   - `pass`: continue to step 4.
4. If `tdd.mutation` is enabled, run `"$TU" test mutation <primary source file or package>`.
   If the score is below `tdd.mutation_threshold` (default 0.7), feed the surviving
   mutants back to the craftsman and loop (consume one budget). Otherwise continue.
5. The feature passed. If `tdd.archive` is enabled (default on), run the scribe
   stage (dispatch `general-purpose` with `"$TU" tdd prompt scribe`)
   to `mem_save` a `decision/<feature>` note. Call `"$TU" tdd state mark <slug> pass`.

If the budget is exhausted for a feature, call
`"$TU" tdd state mark <slug> blocked` and stop the entire run with the last
feedback.

## Notes

- The gate (`tdd gate`) and mutation (`test mutation`) are deterministic and run
  in the binary — trust their output rather than re-judging tests yourself.
- Keep the contract JSON shape the agents emit; route on `status` and (for the
  architect) `complexity`, exactly as the CLI conductor does.
