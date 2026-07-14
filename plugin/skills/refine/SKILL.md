---
name: refine
description: Use after implementation is green and before review/merge, to clean the changed code — reuse over duplication, right altitude, dead weight removed — through strictly behavior-preserving edits. Quality only; it does not hunt bugs (that is review's job), and a bug found here gets reported, not fixed. Keywords - refine, simplify, clean up, refactor the diff, reduce duplication, polish, dead code, tu-agent.
---

# tu-agent refine

A dedicated quality pass over the diff, run with the tests already green. Kept
separate from bug-hunting on purpose: a pass that mixes "find defects" with
"clean up" does both badly. Review judges; refine tidies.

## The iron law

**Behavior-preserving only, proven by the suite.** Green before the pass,
green after every edit, per `tu-agent:verification-before-completion`. Any
edit you cannot argue is behavior-preserving does not belong in this pass.
And if you spot a bug while simplifying: **report it, don't fix it here** —
it needs `tu-agent:systematic-debugging` and its own red test, not a drive-by
edit inside a cleanup.

## Scope: the diff, not the codebase

The unit of work is the branch/change diff (`git diff <default-branch>...HEAD`
— three dots, so git computes the merge-base — plus working tree). Simplifying pre-existing code you didn't touch is scope
creep that bloats the review — if you see a worthwhile cleanup outside the
diff, propose it as a follow-up instead.

## The pass — what to look for

Work through the changed hunks with these lenses, in order of payoff:

1. **Reuse over reinvention** — did the change add a helper, constant, or
   pattern the repo already has? `find_symbol`/`get_context` and a targeted
   grep answer this in seconds; the second implementation of anything is a
   bug factory. Prefer deleting yours and calling theirs.
2. **Altitude** — is the code at the abstraction level of its neighbors?
   Both directions are wrong: an interface/config/parameter serving exactly
   one caller (speculative generality — inline it), and a 40-line function
   doing three nameable things (extract them).
3. **Dead weight** — unused parameters, unreachable branches, flags nothing
   sets, imports nothing uses, commented-out code. Added-but-never-consumed
   surface is the most common diff residue.
4. **Control flow** — early returns over nesting, guard clauses over
   else-pyramids, one obvious path through each function.
5. **Comments that narrate instead of explain** — comments saying *what the
   next line does* or *why the change is correct* (review-talk) go; comments
   stating a constraint the code can't show stay.
6. **Names** — do new names follow the repo's conventions (check siblings,
   `mem_search("convention")`)? A good name deletes a comment.

## Apply one at a time

Each cleanup: smallest edit → suite green → next. Batched cleanups that end
red leave you bisecting your own polish. If a cleanup requires touching
unchanged code beyond trivial call-site updates, downgrade it to a proposal.

## Report

List what was simplified (one line each: lens + file), what was proposed as
follow-up, and any bug found (reported, untouched). Then re-run the full
verification once more — the pass ends green or it didn't happen.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "While I'm here, this old code needs cleanup too" | Outside the diff = follow-up proposal, not a stowaway edit. |
| "This tiny bug fix can ride along" | A fix without its red test, hidden in a cleanup commit, is how regressions are born. |
| "More abstraction makes it future-proof" | One caller = no abstraction. Speculative generality is debt, not investment. |
| "The tests pass, so my 5 batched edits are fine" | Which of the 5 would fail alone? You no longer know. |
| "The comment can stay, it's harmless" | Narration rots into lies the first time the code changes. |
