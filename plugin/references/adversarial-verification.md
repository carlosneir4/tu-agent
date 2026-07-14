# Adversarial verification — refute your own findings

Shared discipline for any skill or agent that produces findings: security
issues, bug diagnoses, review feedback, root-cause claims. The step most
reviews skip is the one that makes them trustworthy.

## The iron law

**A finding you have not tried to kill is a guess.** Nothing is reported until
it has survived a genuine refutation attempt. One verified finding is worth
ten plausible ones — noise buries the real signal and burns the reader's
trust.

## The refutation pass — per finding

1. **State the claim precisely.** Not "possible SQL injection" but "input X
   entering at `handler.go:42` reaches the query built at `store.go:88`
   without sanitization, so a crafted value does Z". If you cannot state it
   this precisely yet, you are not done investigating.
2. **Trace the path.** Walk from an entry point to the sink, citing
   `file:line` for every hop. Use the graph (`get_context`, `find_symbol`) to
   find callers instead of assuming them. A sink nobody reaches with
   attacker-controlled data is not a finding.
3. **Hunt the guard.** Actively search for the thing that kills your finding:
   validation upstream, a middleware, a schema check, an authz gate, a
   framework default. Grep the callers and the call chain — not just the file
   the sink lives in. Finding the guard is a success, not a failure: drop the
   finding.
4. **Walk a concrete exploit or repro.** What exact input, request, or
   sequence triggers it? Follow it mentally step by step. Where the
   walk-through breaks down, your finding breaks down.
5. **Verdict.** Every finding gets exactly one:
   - **CONFIRMED** — complete path traced, no guard found, concrete repro
     walk-through holds. Report it.
   - **PLAUSIBLE** — you could not complete the trace; say exactly which hop
     is unverified and why. Report it clearly labeled, below CONFIRMED items.
   - **REFUTED** — a guard exists or the path is unreachable. Drop it
     silently; do not pad the report with survivors' corpses.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "It pattern-matches a known CWE / bug shape, so it counts" | Pattern match is a *lead*, not a finding. Trace it. |
| "Better to over-report and let the human decide" | Ten unverified findings hide the one real HIGH. Curation is the job. |
| "I can't run the code, so I can't verify" | You can read and trace every hop. That is verification. |
| "Someone downstream will double-check it" | You are the check. Nobody re-verifies a reviewer. |
| "I already spent a lot of effort on this finding" | Sunk cost. A refuted finding dropped late is still a win. |

## Beyond security

The same pass applies to a debugging root-cause claim ("the bug is in X" —
trace it, hunt the alternative explanation, repro it) and to review feedback
you receive ("the reviewer says X is broken" — verify before implementing the
fix; performative agreement ships wrong fixes).
