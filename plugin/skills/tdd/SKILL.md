---
name: tdd
description: Build a WHOLE feature end-to-end through the strict tu-agent TDD dev-flow — interrogation to a signed spec, Gherkin scenarios, RED/GREEN with a deterministic gate, a design judge, optional mutation hardening, whole-branch review, and a memory archive. Heavyweight; for a quick single-change anchor before coding, use groundwork instead. Keywords - tdd, dev-flow, feature, gherkin, strict TDD, craftsman, judge.
---

# tu-agent tdd dev-flow (plugin orchestrator)

You are the conductor of the tu-agent TDD dev-flow. The binary supplies the
deterministic gates, the role agents, and memory; you sequence the stages and
route on each stage's contract. Same on-disk artifacts as the CLI: each run's
artifacts live under a per-feature base dir `$BASE` — resolved once, either
fresh in Step 1 via `"$TU" tdd path`, or bound from `tdd status` in Step 0 when
resuming — so they are `$BASE/spec.md`, `$BASE/features/<name>.feature`,
`$BASE/progress/`.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"` and use it for every binary call.

## Step 0: Preflight

Run: `"$TU" version` — if it fails, STOP and show the shim's install instructions.

Run: `"$TU" tdd check` (informational, never fatal). Each dev-flow role resolves
to an embedded generic shell unless the repo overrides it with its own
`.claude/agents/<role>.md`, so a missing agent file is fine — the flow runs
either way. This flow never generates agents.

Run `"$TU" tdd status`. If its JSON has `"resumable": true`, show the user the
done/pending features and ask **resume or restart**. On **resume**, read the
`base` field from the same `tdd status` JSON you already parsed for
`resumable` and set `$BASE` to it — the resumed run's artifacts live there.
(The ticket is already encoded in `$BASE`; you do not need `$TICKET`/
`$FEATURE_DESC` on resume.) Before jumping into the feature loop, re-present
the REMAINING plan: read the pending features from `$BASE/plan.md` when it
exists (its Features & scenarios prose), else from the `tdd status` listing,
and ask the user one question — "still valid?" — so a stale plan is caught
before more work is sunk into it. Then skip Steps 1–3 and 3.5 and jump
straight to Step 5's feature loop at the first `pending` feature — **unless**
`tdd status` shows every feature already `pass` and `review: "pending"`, in
which case skip Step 5 entirely and resume directly at Step 6's post-loop
review. On **restart**, proceed normally (a fresh `tdd state begin` later
overwrites it; `$BASE` is instead resolved fresh in Step 1).

Read `.tu-agent/config.yaml` if present and note `tdd.mutation` (default off),
`tdd.archive` (default on), `tdd.auto_fix_review` (default off — gates Step 6's
fixer dispatch on human approval instead of auto-fixing), and
`tdd.test_command`.

## Stage dispatch model — read this carefully

Every non-interactive stage is run by dispatching the **`general-purpose`** agent
with the composed stage prompt as its instructions. Fetch the prompt with
`"$TU" tdd prompt <stage> --base "$BASE"` — the binary composes the role's
agent body (its knowledge — an embedded generic shell, or a repo-level
`.claude/agents/<role>.md` override), the runtime language overlay, project
rules, and the generic TDD overlay (the contract), substituting the same
`$BASE` this run resolved (fresh in Step 1,
or bound from `tdd status` on resume in Step 0). Prepend it to any runtime
specifics (feature name, prior gate/judge feedback). This depends on NO agent
being registered, so it works identically in a fresh session and right after
`tu-agent prepare` (which is exactly when named agents are not yet
dispatchable). Passing `--base "$BASE"` explicitly (rather than
`--ticket`/the feature description) is deterministic and avoids re-deriving
the slug — the plugin always has `$BASE` resolved by this point.

Stages run this way: `architect`, `craftsman`, `judge`, `scribe`, `test-writer`,
`implementer`. `test-writer` and `implementer` are not flow stages the binary
executes on their own — they exist only so `"$TU" tdd prompt <name> --base "$BASE"` can hand you
their composed overlay (RED-phase "tests only, no production" / GREEN-phase
"minimal production, never touch tests"). Together they form the RED->GREEN
"sandwich" that Step 5's inner loop runs per sub-feature; the bare `craftsman`
stage is used only by Step 4's trivial path.

The **analyst** (Step 1) and the **human gate** (Step 3) are interactive — a
dispatched agent cannot ask the user and wait — so YOU, the conductor, run them
yourself in this conversation (the analyst using
`"$TU" tdd prompt analyst --base "$BASE"` for its style and project
knowledge). Never dispatch them.

"**run the <stage> stage**" means: dispatch `general-purpose` with
`"$TU" tdd prompt <stage> --base "$BASE"` as its instructions, plus the task
described.

## Step 1: Analyst — you conduct this yourself (do NOT dispatch)

Before anything else, resolve this run's base dir and reuse it for the rest of
the run:

- Ensure you have a feature description `$FEATURE_DESC` (if the user gave none
  when invoking the skill, ask what they want to build — this is the first
  analyst question anyway).
- Ask the user for an optional ticket id `$TICKET` (e.g. `JIRA-1234`), or
  "none" (empty).
- Resolve the run's base dir once:
  ```
  BASE="$( "$TU" tdd path ${TICKET:+--ticket "$TICKET"} "$FEATURE_DESC" )"
  ```

If `$BASE/spec.md` already exists from a prior run, show it to the user and ask
whether to reuse it (skip to Step 2) or start the interrogation over. Otherwise:

YOU act as the analyst, because a dispatched sub-agent cannot talk to the user.
Fetch `"$TU" tdd prompt analyst --base "$BASE"` for
the interrogation style and project knowledge, then interview
the user directly, **one question at a time**: what they want to build, the
contract (inputs/outputs/behavior), edge cases, and the reasons behind decisions.
Keep going until the spec is complete; then write it to
`$BASE/spec.md`. Do not invent answers — the user is the source of truth.

**Design exploration:** with the requirements complete and before writing the
spec, if more than one viable approach exists — the signal is that the choice
changes which files or packages get touched — propose 2-3 approaches with their
trade-offs and a recommendation, and let the user pick. Record the decision (the
chosen approach, why, and the rejected alternatives) in an always-present
`## Design` section of `$BASE/spec.md`; when only one reasonable approach exists
that section is a single line and you ask no extra question.

**Design doc seeding:** if the user provides a design doc or superpowers plan file,
seed the spec from it instead of interrogating from zero (mirroring the CLI `--design <path>` flag) — fetch the analyst prompt
and prepend the file path and instruction: "The user provided a design document at
<path>. Read it, seed the spec from it, then confirm by exception — ask only about
gaps, ambiguities, or contradictions." The analyst reads the file and writes the spec
directly if the document is complete, or asks clarifying questions if gaps exist.

Only once the spec is complete, continue to Step 2.

## Step 2: Architect

Run the architect stage (dispatch `general-purpose` with `"$TU" tdd prompt architect --base "$BASE"`). It reads the spec, writes one
`$BASE/features/<name>.feature` per feature with `@s1..@sn` tagged
scenarios, and returns a contract with a `features` array of
`{name, scenarios}` objects (one entry for standard complexity, several for
complex), plus a `complexity` of `trivial`, `standard`, or `complex`.

The architect consults existing test coverage (via `tu-agent test gaps` / graph
`tested_by`) before writing scenarios so it avoids proposing regression scenarios
for behavior already covered.

(The binary also accepts the legacy `handoff`+`scenarios` contract form for
backward compatibility, normalized into a single feature.)

## Step 3: Human gate (with design loop-back)

When `$BASE/plan.md` exists, BEFORE presenting it, run the spec-judge stage
(dispatch `general-purpose` with `"$TU" tdd prompt spec-judge --base "$BASE"`)
— the pre-code scope skeptic that checks the plan and its scenarios trace to
spec.md's Goal and do not violate its Non-goals. It returns a 2-4 line verdict
as plain text (not a JSON contract) — show that verdict TOGETHER WITH
`plan.md`'s prose. State explicitly that the human decides: the spec-judge
verdict only informs, it never auto-blocks approval — even a critical
finding is the user's call. On a design loop-back round (see "describe a
change" below), re-run the spec-judge on the revised plan and show its
updated verdict alongside the delta.

Present the design for approval. When `$BASE/plan.md` exists, show ITS PROSE —
never raw Gherkin — the reader is a human deciding whether to sign off, not a
test runner. Fall back to the current feature/scenario listing (iterate the
`features` array) only when `plan.md` does not exist. Ask the user to
**approve**, **abort**, or **describe what to change**. Then:

- approve →
  1. run `"$TU" tdd state approve-design --base "$BASE" --rounds <n>` where
     `<n>` is the number of design rounds consumed so far (1 if approved on
     the first showing) — this stamps the `## Sign-off` section into
     `plan.md` and writes the durable approval token later steps depend on.
  2. `mem_save` a `decision/<slug>-design` note recording the chosen approach
     and the rejected alternatives AT PLAN TIME — do not defer this to the
     post-loop scribe, the design reasoning is freshest now.
  3. only then continue to Step 3.5.
- abort → STOP.
- describe a change → re-dispatch the architect (`general-purpose` with
  `"$TU" tdd prompt architect --base "$BASE"`) with the user's feedback prepended ("The user
  rejected the previous design: <feedback>. Revise accordingly."), re-run the
  spec-judge on the revised plan, then present only the DELTA of the revised
  design — what changed and why — plus the updated spec-judge verdict, not a
  reprint of the whole plan.md/feature listing, and ask again. Allow up to
  **3 design rounds**; if still not approved, STOP.

If the architect returned `complexity: trivial`, there are no features to
list — show the architect's one-line summary of the intended change (its
`handoff`) and ask the user to **approve or abort** before Step 4. Never
start the trivial path without that approval. (The trivial path has no
plan.md and never calls `approve-design` — it never has a design to approve.)

## Step 3.5: Branch and ticket

After the user approves the scenarios, before any TDD. The ticket `$TICKET`
was already captured in Step 1 — do not ask for it again:

1. Propose a feature branch name that weaves the already-known `$TICKET` and a slug of the
   feature name (e.g. `feat/<ticket>-<slug>`; drop the ticket segment if none). Show it and
   let the user **confirm or edit** it — do not assume a fixed convention.
2. Create and check out the branch: `git checkout -b <name>` (this carries the already-written
   spec and feature files into the new branch). If the user is already on a suitable feature
   branch, ask whether to use it as-is instead.
3. If a ticket was given, it is already reflected in `$BASE` (the artifact dir resolved in
   Step 1) — additionally record it in the `$BASE/spec.md` header and prefix the commit
   messages you suggest with it.

(Run-state recording happens in Step 5, not here, so the trivial path never
leaves a tracked run open.)

## Step 4: Trivial path

If `complexity` is `trivial`: run the craftsman stage (dispatch `general-purpose`
with `"$TU" tdd prompt craftsman --base "$BASE"`) to make the change keeping
existing tests green, then run
```
"$TU" tdd verify
```
If the JSON has `"ok": true`, report done; otherwise show the failure and stop.
Do NOT call `tdd gate` on the trivial path — there is no feature file to gate
against. The trivial path writes no run state.

## Step 5: Standard loop — outer feature loop + inner retry budget

**Record the run first (fresh runs only).** If you are NOT resuming, run
`"$TU" tdd state begin --base "$BASE"` with one `--feature <slug>` per approved
feature (from the architect's `features` array), plus `--branch <name>`,
`--task "<short description>"`, and `--complexity <complexity from the
architect contract>` (`standard` or `complex` — Step 5 is never reached on the
trivial path). Standard/complex begins are refused by the binary unless Step 3
already ran `approve-design` for this base, so the flag also documents which
design was signed off on. On resume the state already exists — skip this.

Then obtain the pending features from `"$TU" tdd status --base "$BASE"` (the
features whose `status` is `"pending"` in the `features` array). For
each pending feature in order, run the **inner standard loop** below (retry
budget 3). After the feature reaches a terminal state, call
`"$TU" tdd state mark <slug> pass --base "$BASE"` on success or
`"$TU" tdd state mark <slug> blocked --base "$BASE"` on failure. Stop the entire
run on the first blocked feature — remaining features stay `pending` and are
resumable in the next session.

**Inner standard loop for one feature (retry budget 3):** each pass through the
loop is a full RED->GREEN sandwich — the test-writer and implementer are always
re-dispatched together; there is no partial resume mid-sandwich.

**Refactor features skip the sandwich.** If a feature's entry in the architect
contract has `"kind": "refactor"`, do not run RED→GREEN: dispatch
`general-purpose` with `"$TU" tdd prompt refactor --base "$BASE"` plus the
feature name, then run `"$TU" tdd verify` — the whole suite must stay green
(`"ok": true`; otherwise feed the failure back and retry within the same
budget of 3). Then run the judge (step 6) and on `pass` mark the feature done
as usual.

**Language support:** the RED→GREEN gate currently supports Java (JUnit XML)
and Go projects only — other languages lack a result-artifact parser and
`isTestPath` recognition, so their test files would be mis-partitioned. For Go
the gate additionally proves each new test file red by running exactly its
Test functions scoped to its package.

Repeat up to 3 times:

**RED phase — failing tests only, no production:**

1. Run the test-writer stage (dispatch `general-purpose` with
   `"$TU" tdd prompt test-writer --base "$BASE"`; pass any prior gate/judge feedback plus the
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
   Then run: `"$TU" tdd gate --feature <name> --expect red --new-tests "$NEW_TESTS" --base "$BASE"`.
   - `"ok": false` with feedback `"tests green without production: ..."` naming
     every new test file — the scenario already passed against existing code.
     This is a **regression catch**, not a defect: note it for the archive and
     skip straight to step 6 (judge) for this pass, with no GREEN phase.
   - `"ok": false` otherwise (e.g. `"suite is green — no failing test drove the
     change"`) — the new tests did not go red; feed the feedback to the
     test-writer and loop (consume one budget).
   - `"ok": true` — the new tests are confirmed red; continue to the GREEN
     phase. The gate just wrote the durable RED baseline to
     `$BASE/progress/red-baseline.json` (the content hashes of the new test
     files). You do not need to remember anything — the GREEN gate verifies
     it.

**GREEN phase — minimal production, tests are frozen:**

4. Run the implementer stage (dispatch `general-purpose` with
   `"$TU" tdd prompt implementer --base "$BASE"`; pass any prior gate/judge feedback). It must
   not modify, add, or delete any test file.
5. Recompute the changed-or-new file set the same untracked-aware way
   (`{ git diff --name-only; git ls-files --others --exclude-standard; } | sort -u`).
   Read the test-file list from `$BASE/progress/red-baseline.json`; if any
   TEST file in the new set is not listed there, the implementer **added** a
   test — feed it back ("you added a test; tests are frozen once red") and
   loop (consume one budget) without calling the gate. Modified tests are
   caught by the gate itself: it re-hashes the baseline files and fails with
   `test files mutated since RED: ...` — treat that feedback exactly like any
   other `"ok": false` (feed back to the implementer, consume one budget).
   Otherwise run
   `"$TU" tdd gate --feature <name> --covered <the implementer's scenarios> --base "$BASE"`.
   - Non-zero exit / error: the gate could not run — show the error and stop.
   - `"ok": false`: feed the `feedback` back to the implementer and loop (consume one budget).
   - `"ok": true`: continue. If the gate JSON carries a `warning` about a
     missing baseline, surface it to the user but continue.
6. Run the judge stage (dispatch `general-purpose` with `"$TU" tdd prompt judge --base "$BASE"`,
   plus the instruction to read
   `${CLAUDE_PLUGIN_ROOT}/references/adversarial-verification.md` and run its
   refutation pass on every finding before the verdict). On its verdict:
   - `revise`: feed the feedback to the test-writer and restart the sandwich
     from step 1 (consume one budget).
   - `fail`: call `"$TU" tdd state mark <slug> blocked --base "$BASE"` and stop the entire run.
   - `pass`: continue to step 7.
7. **DEFERRED — do not run `"$TU" test mutation` here even if `tdd.mutation`
   is enabled.** Mutation hardening is pending re-enablement for the sandwich
   flow, matching the CLI conductor.
8. The feature passed. If `tdd.archive` is enabled (default on), run the scribe
   stage (dispatch `general-purpose` with `"$TU" tdd prompt scribe --base "$BASE"`)
   to `mem_save` a `decision/<feature>` note. Call
   `"$TU" tdd state mark <slug> pass --base "$BASE"`.

If the budget is exhausted for a feature, call
`"$TU" tdd state mark <slug> blocked --base "$BASE"` and stop the entire run
with the last feedback.

## Step 6: Post-loop review

Runs **once**, after all features pass — never on the trivial path (Step 4)
and never when any feature is `blocked` (the run already stopped in that
case). Mirrors the CLI conductor's `internal/tdd/review.go`.

1. Set `"$TU" tdd state review pending --base "$BASE"` **before** dispatching
   anything, so an interrupted session resumes straight back into this step
   instead of re-running the completed feature loop.
2. Compute the branch scope yourself, matching `internal/tdd/reviewscope.go`:
   resolve the default branch by checking, in order, `origin/HEAD`, then
   `main`, then `master`; run `git merge-base <default-branch> HEAD` to get
   the merge-base, then `git diff --name-only <merge-base> HEAD` for the
   changed files on the branch since it — the committed branch diff since
   merge-base. If no default branch or merge-base resolves, or that diff is
   empty, skip with a **visible warning** to the user, run
   `"$TU" tdd state review skipped --base "$BASE"`, and stop this step — the
   run still ends `pass` on its feature gates.
3. Fetch the stage prompt with `"$TU" tdd prompt review --base "$BASE"` and
   dispatch `general-purpose` with it plus the branch scope (merge-base and
   changed files), same as any other stage — plus the instruction to read
   `${CLAUDE_PLUGIN_ROOT}/references/adversarial-verification.md` and run its
   refutation pass on each finding before reporting it.
4. Route the verdict's findings, branching FIRST on `tdd.auto_fix_review`
   (read in Step 0):
   - **`tdd.auto_fix_review` off (default):** STOP here — do not dispatch the
     review-fixer yet. Present the findings to the user grouped by severity
     (critical/important/minor) with `file:line` for each, at a **human**
     gate, and ask which findings to fix. On **approval**, dispatch the
     review-fixer (`"$TU" tdd prompt review-fixer --base "$BASE"`) for the
     **approved subset** of findings only — never the ones the user did not
     approve — then continue with the same test-immutability enforcement and
     re-review loop described below for that subset. On **reject** or
     **defer**, do not dispatch the fixer at all: the findings stay recorded
     as-is in `$BASE/progress/review.md` (untouched), and step 7 records the
     outcome as `pass-with-pending` (`"$TU" tdd state review pass --base
     "$BASE" --findings <code>`, same terminal shape as the budget-exhausted
     path below; see step 7 for the `<code>` grammar).
   - **`tdd.auto_fix_review` on:** skip the human gate — auto-fix the same as
     today, per the routing below.
   - `critical` or `important` findings: dispatch the review-fixer
     (`"$TU" tdd prompt review-fixer --base "$BASE"`) with the findings.
     Outer-round budget: **2**. After each fixer dispatch, FIRST enforce test
     immutability deterministically — do not trust the prompt. The review-fixer
     may not modify, add, or delete any test file, so compute the test files it
     touched (its edits are uncommitted, so the working-tree diff against the
     committed branch is exactly what it changed):
     ```
     TOUCHED_TESTS="$( git diff --name-only | grep -E '(_test\.go$|/src/test/|^src/test/)' )"
     ```
     If `TOUCHED_TESTS` is non-empty, treat the attempt as failed: re-feed the
     fixer within the **same** round, naming those files ("you modified test
     files, which is forbidden — revert them and fix production only"), and do
     NOT run `"$TU" tdd verify` or accept it as green — mirrors the touched-tests
     path in `runFixerRound` (`internal/tdd/review.go`). Only when no test file
     was touched, run `"$TU" tdd verify` — if `"ok": true`, re-dispatch the
     review stage; if not `true` (red), re-feed the fixer with the failing
     output within the **same** round rather than consuming another round. Both
     re-feed cases (touched tests and red suite) share one inner loop bounded to
     **2** re-feeds (mirrors `reviewRefeedBudget` in `internal/tdd/review.go`).
     If a round exhausts its re-feed budget, the conductor gives up on that
     round without re-review — re-reviewing a diff that was never confirmed
     green would be meaningless — and surfaces a warning (the test suite was
     left red by the review-fixer, or the forbidden test edits it never reverted
     remain in the working tree); the review outcome is still recorded (see
     step 7) so the run does not hang.
   - `minor` findings are **report-only** — never dispatched to the fixer.
5. If critical/important findings persist after the 2-round budget, the run
   still ends `pass`, with the pending findings reported to the user.
6. The dispatched review stage writes its findings to
   `$BASE/progress/review.md`, not the conductor.
7. Record the outcome with `"$TU" tdd state review pass --base "$BASE"
   --findings <code>` (no blocking findings, the budget exhausted with
   pending findings reported, or a fixer round gave up with the suite left
   red) — `<code>` is `clean` when the final verdict carried no findings at
   all, else `critical:N,important:N,minor:N` from its counts, appending
   `,pending` when blocking (critical/important) findings were left unfixed
   (flag off and rejected/deferred, or the round budget exhausted) — or
   `"$TU" tdd state review skipped --base "$BASE"` (step 2 above; a skipped
   review has no findings, so omit `--findings`).

## Notes

- The gate (`tdd gate`) and mutation (`test mutation`) are deterministic and run
  in the binary — trust their output rather than re-judging tests yourself.
- Keep the contract JSON shape the agents emit; route on `status` and (for the
  architect) `complexity`, exactly as the CLI conductor does.
- `tdd gate --expect red` only checks whether the named files went red — it does
  not check who wrote what. The RED-phase "wrote production" order violation is
  guarded by you computing the untracked-aware changed-file set (step 2). Test
  mutation after RED is no longer something you track by hand: the gate owns
  the durable baseline (`$BASE/progress/red-baseline.json`, written and
  re-hashed by `cmd/tu-agent/tdd_gate.go`) and fails with `test files mutated
  since RED: ...` if anything changed — you only additionally catch **added**
  tests (step 5), which the baseline can't see. This is a different mechanism
  from the CLI's fully-automated conductor, `internal/tdd.RunSandwich`, which
  never calls `tdd gate` and instead diffs a private git-index snapshot
  (`internal/tdd/worktree.go`) — the two paths solve the same problem with
  different plumbing.
