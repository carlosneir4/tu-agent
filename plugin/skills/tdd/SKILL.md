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
not provisioned — tell the user to run `/tu-agent:prepare` (or `tu-agent prepare`) and
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
`tu-agent prepare` (which is exactly when named agents are not yet dispatchable).

Stages run this way: `architect`, `craftsman`, `judge`, `scribe`, `test-writer`,
`implementer`. `test-writer` and `implementer` are not flow stages the binary
executes on their own — they exist only so `"$TU" tdd prompt <name>` can hand you
their composed overlay (RED-phase "tests only, no production" / GREEN-phase
"minimal production, never touch tests"). Together they form the RED->GREEN
"sandwich" that Step 5's inner loop runs per sub-feature; the bare `craftsman`
stage is used only by Step 4's trivial path.

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

**Inner standard loop for one feature (retry budget 3):** each pass through the
loop is a full RED->GREEN sandwich — the test-writer and implementer are always
re-dispatched together; there is no partial resume mid-sandwich.

**Language support:** the RED→GREEN gate currently supports Java (JUnit XML)
and Go projects only — other languages lack a result-artifact parser and
`isTestPath` recognition, so their test files would be mis-partitioned.

Repeat up to 3 times:

**RED phase — failing tests only, no production:**

1. Run the test-writer stage (dispatch `general-purpose` with
   `"$TU" tdd prompt test-writer`; pass any prior gate/judge feedback plus the
   feature's `@s` scenarios). It must write ONLY test files.
2. Compute the changed-or-new file set — **tracked and untracked**. A plain
   `git diff --name-only` misses brand-new files (the normal RED-phase output:
   a newly-created test file), which would silently empty out `--new-tests`
   and make the gate below vacuously pass. Discover both:
   ```
   CHANGED="$( { git diff --name-only; git ls-files --others --exclude-standard; } | sort -u )"
   ```
   Order-violation check: if any path in `$CHANGED` is not a test file (not
   `*_test.go` and not under a `src/test/` path segment), that's an order
   violation the gate below cannot see — feed that back to the test-writer
   directly ("you wrote production code during the RED phase; tests only")
   and loop (consume one budget) without calling the gate.
3. Otherwise build the `--new-tests` argument from the TEST files in
   `$CHANGED`. The gate flag splits on commas, but `git`'s output is
   newline-separated, so comma-join it explicitly:
   ```
   NEW_TESTS="$( printf '%s\n' "$CHANGED" | grep -E '(_test\.go$|/src/test/|^src/test/)' | paste -sd, - )"
   ```
   Then run: `"$TU" tdd gate --feature <name> --expect red --new-tests "$NEW_TESTS"`.
   - `"ok": false` with feedback `"tests green without production: ..."` naming
     every new test file — the scenario already passed against existing code.
     This is a **regression catch**, not a defect: note it for the archive and
     skip straight to step 6 (judge) for this pass, with no GREEN phase.
   - `"ok": false` otherwise (e.g. `"suite is green — no failing test drove the
     change"`) — the new tests did not go red; feed the feedback to the
     test-writer and loop (consume one budget).
   - `"ok": true` — the new tests are confirmed red; continue to the GREEN
     phase. Before dispatching the implementer, **remember this state as the
     post-RED baseline**: keep `$CHANGED` (the full path set) and, for every
     TEST file in it, its content hash (`git hash-object <file>`). Nothing is
     committed or stashed between RED and GREEN, so this remembered baseline —
     not a fresh diff — is what the GREEN guard compares against.

**GREEN phase — minimal production, tests are frozen:**

4. Run the implementer stage (dispatch `general-purpose` with
   `"$TU" tdd prompt implementer`; pass any prior gate/judge feedback). It must
   not modify, add, or delete any test file.
5. Recompute the changed-or-new file set the same untracked-aware way
   (`{ git diff --name-only; git ls-files --others --exclude-standard; } | sort -u`).
   A plain re-diff after GREEN shows the *cumulative* RED+GREEN changes — the
   RED test files always reappear in it — so presence alone can't tell you
   whether the implementer touched them. Compare against the **remembered
   post-RED baseline** instead: for every TEST file in the new set, if it
   wasn't in the baseline's test-file list, or its `git hash-object` no longer
   matches the hash recorded at the end of RED, that's a violation — the
   implementer added or modified a test. Feed it back to the implementer ("you
   modified a test; tests are frozen once red") and loop (consume one budget)
   without calling the gate. Otherwise run
   `"$TU" tdd gate --feature <name> --covered <the implementer's scenarios>`.
   - Non-zero exit / error: the gate could not run — show the error and stop.
   - `"ok": false`: feed the `feedback` back to the implementer and loop (consume one budget).
   - `"ok": true`: continue.
6. Run the judge stage (dispatch `general-purpose` with `"$TU" tdd prompt judge`). On its verdict:
   - `revise`: feed the feedback to the test-writer and restart the sandwich
     from step 1 (consume one budget).
   - `fail`: call `"$TU" tdd state mark <slug> blocked` and stop the entire run.
   - `pass`: continue to step 7.
7. **DEFERRED — not currently run.** Mutation hardening (`tdd.mutation`) is
   deliberately skipped on this two-phase RED→GREEN sandwich path, matching the
   CLI conductor (`internal/tdd/flow.go`), which only runs the mutation gate on
   the non-sandwich loop. Do not run `"$TU" test mutation` here even if
   `tdd.mutation` is enabled — this step is pending re-enablement for the
   sandwich flow. (Historical description, not currently exercised: if enabled,
   run `"$TU" test mutation <primary source file or package>`; if the score is
   below `tdd.mutation_threshold` (default 0.7), feed the surviving mutants back
   to the implementer and loop, consuming one budget.)
8. The feature passed. If `tdd.archive` is enabled (default on), run the scribe
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
- `tdd gate --expect red` only checks whether the named files went red — it does
  not check who wrote what. The RED-phase "wrote production" and GREEN-phase
  "modified a test" order violations are guarded by you computing the
  untracked-aware changed-file set and, for GREEN, hashing test files against
  the remembered post-RED baseline (steps 2 and 5) — the same guard
  `internal/tdd.RunSandwich` applies in the CLI conductor by staging into a
  private temp index (`git add -A`) before diffing.
