---
name: verification-before-completion
description: Use when about to claim work is complete, fixed, passing, or working — before committing, reporting done, or moving to the next task. Requires running the project's verification commands fresh and reading their actual output; evidence before assertions, always. Keywords - done, complete, fixed, passing, works now, verify, before commit, tu-agent.
---

# tu-agent verification-before-completion

The words "done", "fixed", "passing", and "works" are claims about the world.
This skill is the gate between wanting to say them and having earned them.

## The iron law

**No completion claim without a fresh verification run whose output you have
actually read.** A claim made from memory of an earlier run, from "the change
was trivial", or from inference ("it compiles, so...") is an assertion
without evidence — the exact failure this skill exists to prevent.

## 1. Name the claim

Before verifying, state (to yourself) exactly what you are about to claim:
"the suite passes", "bug X is fixed", "feature Y handles the empty case".
The claim determines the verification — a vague claim gets a vague check.

## 2. Map claim → verification command

- **"Tests pass"** — the project's test command: `tdd.test_command` in
  `.tu-agent/config.yaml`, else the build tool's standard (`go test ./...`,
  `npm test`, `mvn test`, `pytest`). For a narrow claim, the narrowest
  package/module run that covers it (`get_context` lists tests-to-run);
  for "done" on a whole task, the full suite plus the project's lint/vet.
- **"Bug X is fixed"** — the original repro, re-run. A fix verified only by
  the suite (which missed the bug in the first place) is not verified.
- **"Feature Y works"** — behavior in the running system is a different
  altitude: hand off to the `tu-agent:verify-in-env` skill. Tests here,
  behavior there.

## 3. Run it fresh, read it fully

- Fresh: re-run after your **last** edit. Any edit — a comment, an import,
  a rename — invalidates every earlier green run.
- Read the real output, not just the exit code's vibe: count failures,
  notice skipped tests, notice "0 tests ran" (a green suite that ran
  nothing proves nothing), notice new warnings.

## 4. Report what actually happened

- Verbatim result, trimmed to the relevant lines: the pass/fail summary and
  any failure text. "Suite passes (27 packages, 0 failures)" — not "should
  be fine now".
- If it fails: **that is the report.** State the failure and the output.
  Never soften a failure into "mostly working", never claim done with a
  known red buried in the text, never blame flakiness without evidence
  (re-run it; a real flake shows a different failure, and is worth a
  `gotcha` note via `mem_save`).
- If you could not run the verification (missing tool, no test command
  configured): say NOT VERIFIED and why. An honest gap beats manufactured
  confidence.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "It passed 20 minutes ago" | You edited since. Stale green is not green. |
| "The change is trivial" | Trivial changes break builds daily. The run costs seconds. |
| "It compiles / typechecks" | Shape, not behavior. Run the tests. |
| "CI will catch it" | Then CI is doing your verification after your claim shipped. Backwards. |
| "The suite is slow" | Run the narrowest relevant test now, the full suite before "done". |
| "I'm confident in this one" | Confidence is the feeling; verification is the evidence. |
