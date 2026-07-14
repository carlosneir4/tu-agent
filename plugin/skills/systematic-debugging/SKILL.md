---
name: systematic-debugging
description: Use when encountering any bug, test failure, error, or unexpected behavior, BEFORE proposing or applying a fix — reproduce first, gather evidence, test hypotheses by prediction, and confirm the root cause adversarially; no fix without a confirmed cause. Keywords - bug, debug, test failure, error, broken, doesn't work, unexpected behavior, root cause, tu-agent.
---

# tu-agent systematic-debugging

Debugging is hypothesis testing, not fix roulette. The expensive failure mode
is not "the bug took a while" — it is shipping a fix for the wrong cause,
which hides the symptom and leaves the bug.

## The iron law

**No fix until the root cause is confirmed by a prediction that came true.**
"I changed X and the symptom went away" is not confirmation — it is one
experiment with an uncontrolled variable.

## 0. Recall

`mem_search` the symptom and the area (`mem_search("<error text>")`,
`--type gotcha`, `--type bug-pattern`) — this project's known traps are the
cheapest hypotheses, already ranked by having been true before.

## 1. Reproduce first

- Get a deterministic repro, then shrink it: the narrowest command/test that
  still fails. A bug you cannot trigger on demand cannot be confirmed fixed.
- Read the FULL error text, slowly. The answer is in the message more often
  than pride admits: the line number, the actual vs expected value, the
  type name that is subtly wrong.
- Cannot reproduce? Then the current task is *instrumenting until you can*
  — not fixing blind. If it truly cannot be reproduced, say so and stop;
  a speculative fix is a new bug with extra steps.

## 2. Gather evidence before hypothesizing

- **Newest code first**: `git log`/`git diff` over the recently changed
  files — most bugs live in the last diff, not in the framework.
- **Structure**: `get_context` on the failing symbol for callers and
  dependents — where do the values come from? (Cheaper than guessing.)
- **Reality over model**: log/print the actual runtime values at the
  failure boundary. The bug is precisely where your mental model and the
  runtime disagree, and only evidence locates the divergence.

## 3. Hypothesize — plural

Write down 2-3 candidate explanations ranked by the evidence so far. The
first idea is a candidate, not the answer. If you have only one hypothesis,
you have not looked at enough evidence yet.

## 4. Test by prediction, one variable at a time

- Turn the top hypothesis into a falsifiable prediction: "if H is true,
  then adding a log at X shows Y" / "then reverting commit C makes the test
  pass" / "then this minimal input also fails".
- Run ONE experiment per change: targeted logging, `git bisect`, halving
  the input, isolating a dependency. Two simultaneous changes = zero
  information.
- Prediction wrong → hypothesis dead. Next one. That is progress, not
  failure.

## 5. Confirm adversarially

Before declaring the root cause, run the refutation pass from
`${CLAUDE_PLUGIN_ROOT}/references/adversarial-verification.md` on your own
claim: trace the mechanism end-to-end (`file:line` per hop), hunt the
alternative explanation, and check the cause actually accounts for **every**
observed symptom — a cause that explains 2 of 3 symptoms is a second bug or
the wrong cause.

## 6. Fix the cause, prove it, capture it

- Minimal fix at the root — not a guard at the symptom. If you find
  yourself adding a nil/null check, first answer *why* the value was nil.
- Write the regression test that fails on the old code and passes on the
  fix — that failing-then-green test IS the proof the cause was real.
- Verify per `tu-agent:verification-before-completion`: repro passes, suite
  green.
- If the trap generalizes, `mem_save` it — `type: gotcha` (one trap per
  note) or `bug-pattern/<name>` with Symptom → Root cause → Fix →
  Prevention, so next time step 0 finds it.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "Let me just try changing X" | Shotgun debugging. Each untested change scrambles the evidence. |
| "It's probably a bug in the framework/library" | It is your code until a minimal repro outside your code proves otherwise. |
| "Added a null check, symptom gone, done" | You silenced the symptom. Why was it null? |
| "I changed 3 things and it works now" | Which one fixed it? Do the other 2 harm? You don't know. |
| "The test is just flaky" | Re-run it. A dismissal without evidence is a bug you scheduled for later. |
| "No time to reproduce, I'll fix from the stack trace" | The trace shows where it died, not why. Cheap now, expensive twice. |
