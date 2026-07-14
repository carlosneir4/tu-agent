---
name: security-review
description: Use when the user wants a security review or audit of changes, a branch, or an area of the codebase — deterministic scoping (diff + graph blast radius), a threat-model-first sweep with per-language checklists, and adversarial verification of every finding before it is reported. Report-only, never auto-fixes. Keywords - security review, security audit, vulnerabilities, OWASP, CWE, injection, secrets, auth review, tu-agent.
---

# tu-agent security review

An adversarially verified security review. Scoping is deterministic (git +
graph); judgment happens here. The output is a report the human acts on —
**this skill never applies fixes**, no matter how obvious one looks. Findings
are reported with evidence and wait for human approval.

## 0. Scope deterministically

Establish exactly what is under review before reading any code:

- Default scope: the current branch's diff — `git diff --name-only
  $(git merge-base HEAD <default-branch>)..HEAD` (default branch is usually
  `main`; check `git branch -r`). If the user named an area, package, or PR
  instead, use that.
- For each changed file, `get_context` gives blast radius: callers and
  dependents that can make a local weakness reachable. A changed helper with
  40 dependents reviews differently than a leaf.
- Recall first: `mem_recent(5)`, `mem_search("review-finding")`, and
  `mem_search("security <area>")` — prior findings and decisions carry
  context the diff does not (e.g. "auth is enforced at the gateway").

Resolve the language checklist: read `tdd.language` from
`.tu-agent/config.yaml`; if unset, detect from build files (`go.mod` → go,
`pom.xml`/`build.gradle` → java, `pyproject.toml` → python, `package.json` →
typescript). Load `${CLAUDE_PLUGIN_ROOT}/skills/security-review/references/<language>.md`
if it exists (`javascript` uses the typescript checklist). For polyglot scopes, load one checklist per language present in
the changed set.

## 1. Threat-model before grepping

Write down, in 3-6 lines, before any pattern search:

- **Entry points** in scope: HTTP handlers, CLI args, file/queue consumers,
  webhooks — anything an outsider or a less-trusted component can drive.
- **Trust boundaries** the changed code crosses: user input → query, request
  → filesystem path, config → shell command, service A → service B.
- **Assets**: what an attacker would want here (credentials, PII, code
  execution, another user's data).

Every check in step 2 is then anchored: "can input from entry point E reach
sink S?". A sweep without a threat model is grep with extra steps.

## 2. Systematic sweep

Check each category against the scope, using the language checklist for the
concrete patterns. Do not skip a category because it "obviously doesn't
apply" — write one line saying why it doesn't.

1. **Injection** — SQL/NoSQL, shell/command, template, header/log injection.
2. **Secrets** — hardcoded credentials, tokens, keys; secrets in logs, error
   messages, or client-delivered code.
3. **AuthN/AuthZ** — unauthenticated entry points, missing per-object
   authorization (IDOR), privilege checks done client-side or after the
   action.
4. **Unsafe deserialization & parsing** — native deserializers on untrusted
   data, XXE, archive extraction (zip-slip), schema-less parsing into
   executable structures.
5. **Filesystem & network reach** — path traversal, SSRF via user-supplied
   URLs, permissive TLS settings.
6. **Crypto & randomness** — home-rolled crypto, weak hashes for passwords,
   non-cryptographic randomness for tokens/session IDs.
7. **Dependencies** — suspicious or abandoned packages added by the diff;
   known-vulnerable versions in the manifest.
8. **Data exposure** — PII or sensitive values in logs, metrics, or overly
   detailed error responses.

Collect *candidates* here, cheaply — a candidate is a lead, not yet a
finding.

## 3. Adversarial verification — the gate

Read `${CLAUDE_PLUGIN_ROOT}/references/adversarial-verification.md` and run
its refutation pass on **every** candidate: state the claim precisely, trace
entry-point → sink with `file:line` per hop, hunt the guard that kills it,
walk a concrete exploit. Only CONFIRMED and (clearly labeled) PLAUSIBLE
findings survive into the report. REFUTED candidates are dropped, not listed.

If the scope is large (more than ~15 changed files), dispatch the sweep: one
read-only agent per area or category (use the project's `security-reviewer`
agent if present, else a read-only general-purpose agent), passing each the
scope, the threat model from step 1, the relevant language checklist, and the
refutation-pass instructions verbatim. Verification standards do not relax
because an agent did the sweep — re-verify anything an agent reports as
CONFIRMED that lacks a complete trace.

## 4. Severity by exploitability

Severity comes from the verified trace, not the pattern:

- **HIGH** — exploitable now by an unprivileged actor; block merge.
- **MEDIUM** — exploitable with plausible preconditions (authenticated user,
  specific config); fix before next release.
- **LOW** — hardening; defense-in-depth with no traced path.

A textbook-scary pattern with no reachable path is LOW or dropped. A boring
pattern with a complete trace from the internet is HIGH.

## 5. Report and stop

```
## Risk: HIGH | MEDIUM | LOW | NONE

### Findings (adversarially verified)
- [CWE-nnn] path/to/file:line — claim — trace summary (entry → sink) —
  CONFIRMED | PLAUSIBLE (unverified hop: ...) — remediation

### Refuted during review (one line each, only if the user asked "check X")

### Informational
- path/to/file:line — observation (no action required)
```

- Every HIGH/MEDIUM cites CWE, `file:line`, and the traced path.
- **Do not fix anything.** Present the report and wait for the human's
  decision — strong evidence for a disputed finding is a failing
  test/repro against the current code, not more prose.
- If a recurring vulnerability pattern surfaced (same mistake in 2+ places),
  `mem_save` it: topic `bug-pattern/<pattern>`, one paragraph, so the next
  review recalls it.
