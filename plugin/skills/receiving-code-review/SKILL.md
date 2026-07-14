---
name: receiving-code-review
description: Use when processing code-review feedback on your work — from a human, a reviewer agent, or a judge stage — before implementing any suggested change. Requires verifying each finding against the code first; push back with evidence on incorrect findings instead of performative agreement. Keywords - review feedback, reviewer said, address comments, fix findings, code review response, tu-agent.
---

# tu-agent receiving-code-review

Review feedback is a set of claims about your code — not a work order. The
goal is a correct outcome, which means every finding gets verified before it
gets implemented, disputed, or dismissed. Blind compliance ships wrong fixes;
blind dismissal ships the original bug.

## The iron law

**Never implement a fix you have not verified is fixing something real, and
never dismiss a finding you have not verified is wrong.** Agreement is earned
by evidence in both directions. "Great catch, fixed!" without verification is
performative — and worse than silence when the catch was wrong.

## 1. Parse into discrete findings

Split the review into individual claims, each with: the assertion (what is
wrong), the evidence given (file:line? scenario? none?), and the suggested
remedy (which may be absent or may differ from the right one).

## 2. Verify each finding against the code

Run the refutation pass from
`${CLAUDE_PLUGIN_ROOT}/references/adversarial-verification.md` on the
*reviewer's* claim, exactly as you would on your own: trace the scenario they
describe, hunt the guard they may have missed, walk their failure case
concretely (`get_context` for callers they may not have seen). Classify:

- **CORRECT** — the scenario holds. (Most findings land here; verifying is
  usually fast.)
- **CORRECT, WRONG REMEDY** — real problem, but the suggested fix is
  misplaced or breaks something else the reviewer didn't see.
- **INCORRECT** — a guard exists, the path is unreachable, or the claimed
  behavior isn't what the code does.
- **UNCLEAR** — you cannot tell what scenario the reviewer means.

## 3. Respond by class — with evidence

- **CORRECT** → fix it, narrowest change that resolves the finding; say
  what changed and where.
- **CORRECT, WRONG REMEDY** → fix the real problem your way and explain why
  the suggested remedy was not taken. Silently substituting your fix while
  implying you followed theirs erodes trust.
- **INCORRECT** → push back, respectfully and concretely: the `file:line`
  of the guard they missed, the trace that shows the path unreachable. The
  strongest evidence in a dispute is executable — a test that passes
  against current code where they predicted failure (or, for findings you
  raise, a test that fails against the old code). Prose ties go to the
  reviewer; evidence settles it.
- **UNCLEAR** → one targeted question naming what you need ("which entry
  point reaches this with a nil user?"). Not a defense, a question.

When the reviewer is the human: report your verification and evidence, then
**wait for their call** on disputed findings — pushback is your duty,
unilateral dismissal is not your right.

## 4. Apply as a batch, then re-verify

- Check fixes against each other: two individually correct fixes can
  conflict, and a fix for finding 3 can invalidate your response to
  finding 1.
- After applying, re-run verification per
  `tu-agent:verification-before-completion` — a review round that ends with
  an unverified "all addressed" restarts the loop with interest.
- If a finding revealed a recurring trap, `mem_save` it (`type: gotcha` or
  `bug-pattern/...`) so the next review doesn't re-teach it.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "The reviewer is senior / an expert, just do it" | Seniority raises the prior, not to 1.0. Verify; experts misread diffs too. |
| "Easier to comply than argue" | You'll own the wrong fix, not the reviewer. Verifying is usually cheaper than the rework. |
| "They clearly misread — skip it" | Dismissal needs the same evidence bar as compliance. Trace it first. |
| "I'll apply everything and see if tests pass" | Tests passing doesn't validate fixes for claims that were wrong to begin with. |
| "Defending my code looks bad" | Correctness beats comfort. Evidence-backed pushback is the job description. |
