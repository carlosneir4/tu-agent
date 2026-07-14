---
name: finishing-a-development-branch
description: Use when implementation work on a branch is complete and verified, and it is time to integrate — after a tdd run finishes or any feature/fix work wraps up. Checks verification, review, and memory hygiene, then presents integration options (merge, PR, keep, discard) for the user to choose; never merges, pushes, or deletes on its own. Keywords - finish branch, merge, open PR, wrap up, done with feature, integrate, close out, tu-agent.
---

# tu-agent finishing-a-development-branch

The gap between "the code works" and "the work is finished": integration is a
set of decisions the user owns, executed over a branch whose quality and
memory hygiene you can check first. This picks up exactly where the `tdd`
dev-flow ends (features pass, decision archived) — and applies equally to
work done without it.

## The iron law

**Integration actions are the user's call.** Never merge, push, delete a
branch, or open a PR without the user having chosen that option this session.
Your job is to make the choice easy: preflight done, options laid out,
consequences stated.

## 1. Preflight — earn the right to integrate

- **Working tree**: `git status` — every change staged-or-committed
  deliberately. Stray files (editor droppings, generated artifacts, unrelated
  edits) get flagged, not swept into the merge.
- **Verification**: fresh full-suite run per
  `tu-agent:verification-before-completion`. A branch integrates on today's
  green, not last Tuesday's.
- **Review**: has the branch been reviewed (tdd post-loop review, pr-reviewer,
  `tu-agent:security-review` for security-relevant diffs)? If not, offer it
  now — reviewing after merge is archaeology.
- **Commits**: history tells the story? Follow the project's commit
  convention (check `CLAUDE.md`/recent `git log` for the style, e.g.
  Conventional Commits).

## 2. Memory hygiene — don't let the branch's lessons die

- Were this branch's decisions/gotchas captured? (A tdd run's scribe did it;
  ad-hoc work often didn't.) If not: `mem_save` the durable "why" now —
  `decision/<topic>`, one `gotcha` per trap.
- If the project shares memory via git chunks (`.tu-agent/memory/chunks/`),
  run `tu-agent memory export` and include the updated chunk in the branch —
  otherwise the team merges your code but not your reasons.

## 3. Present the options — then wait

Lay out the choices with one line of consequence each, and let the user pick:

1. **Merge into <default-branch> locally** — for solo/local flows; state
   whether it fast-forwards, and ask whether to delete the merged branch
   afterwards.
2. **Push and open a PR** — for reviewed team flows; draft title/description
   from the branch's commits.
3. **Keep the branch as-is** — work pauses here; note in one line what
   remains.
4. **Discard** — destructive; require explicit confirmation naming the
   branch, and remind about anything unmerged (including the memory chunk).

Execute only what was chosen, exactly as scoped. If pre-authorized in config
or by the user earlier in the session, say what you are doing anyway.

## 4. After integrating

- Switch back to the default branch and sync (`git pull`), then
  `tu-agent memory import` to absorb teammates' chunks.
- Delete the merged branch only if the user chose that.
- If the work changed structure significantly and the graph looks stale,
  `tu-agent learn` refreshes it.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "Tests are green, I'll just merge" | Green earns the *option* to merge. The user picks the option. |
| "I'll commit everything in the tree, it's all mine" | Audit `git status` first — stray files ride along silently forever. |
| "Memory export can wait for another day" | The chunk merges with the branch or the team gets code without reasons. |
| "The branch is stale, safe to delete" | Verify merged-ness (`git branch --merged`) and ask; deleted work is gone. |
| "The PR description can just say 'fixes'" | The description is the reviewer's threat model. Summarize what changed and why. |
