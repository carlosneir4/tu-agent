---
name: verify-in-env
description: Use after implementing a change to prove it works in the running system — launch the project's app or environment, drive the affected flow end-to-end, and report observed behavior with evidence. Discovers how to launch and exercise the project once, then persists the recipe to project memory for every later run. Keywords - verify, smoke test, run the app, try it out, does it work, end-to-end, manual test, tu-agent.
---

# tu-agent verify-in-env

Prove a change works by observing the running system, not by inferring it
from green tests. Unit tests passing, typecheck clean, and build succeeding
are all *necessary* — none of them is *evidence that the feature works*.

## The iron law

**A change is verified when you have driven the affected flow in the running
system and observed the expected behavior.** If you cannot observe it, you
cannot claim it — say what you could not verify and why, instead of
substituting weaker evidence.

## 1. Recall the recipe

This skill discovers each project once. Before exploring:

- `mem_search("verify-recipe")` (also `mem_search("run the app")`).
- If a recipe note exists: follow it and skip to step 3. If following it
  fails (commands drifted), fix the recipe as you go and update the note in
  step 5.

## 2. Discover (first run in a repo)

Work out, from the repo itself, and write down as you go:

- **Project type** — CLI, HTTP server/API, worker/consumer, TUI, web
  frontend, library. `get_context` on the main entry point and the
  architecture overview (`get_architecture`) answer this fast.
- **How to build and launch** — in priority order: existing scripts
  (`Makefile`, `package.json` scripts, `docker-compose.yml`, `Procfile`),
  the README's run section, then the entry point itself. Prefer the
  project's own blessed command over improvising one.
- **What it needs** — env vars, config files, ports, local services
  (database, queue). If a required credential or service is unavailable,
  **stop and say so** — never fake the evidence or silently downgrade to
  "tests pass".
- **How to drive a flow** — `curl`/HTTP client for APIs, running the binary
  with real args for CLIs, a scratch program exercising the public API for
  libraries, the browser for frontends.

Safety boundary: launch local/dev environments only. Never point at
production, never run destructive operations against shared state, and
prefer throwaway data (temp dirs, dedicated test records) you clean up.

## 3. Drive the changed flow — not just "the app starts"

Derive from the diff what user-visible behavior changed, and exercise *that*:

- The new/changed path with realistic input — capture the actual output
  (HTTP status + body, CLI stdout/exit code, log lines, rendered state).
- At least one **negative or edge case** the change claims to handle
  (invalid input, missing field, the old behavior that should now differ).
- One adjacent flow that should NOT have changed — the cheapest regression
  check available.

"The server boots" is not verification of a handler change. Boot it, then
hit the handler.

**Browser path (web frontends).** Check whether Playwright MCP tools are
available: `ToolSearch "playwright"` (they may be deferred — absent from the
active tool list until loaded; load them before concluding they are
unavailable). If available, drive the changed flow in the real browser and
treat screenshots and DOM (Document Object Model — the live page structure)
assertions as evidence. If not available, the existing CLI/curl recipe path
is unchanged — fallback intact.

Before driving anything in the browser, gate on a single per-flow human approval: present the planned flow as numbered steps, e.g. "open /login, fill the form with test data, assert the redirect — ok?". The human approves ONCE; the approved steps then run without re-asking. Anything outside the approved flow — a new origin, a destructive action (deleting data), or real credentials — requires a FRESH gate before it runs.

Every browser action is an MCP tool call recorded by telemetry — this is
the audit trail. Cite it alongside the screenshots/DOM evidence in step 4.

## 4. Judge with evidence

Compare observed vs expected, and report with the receipts:

- Verbatim output snippets (trimmed to the relevant lines), commands used,
  and what each demonstrates.
- If behavior differs from expected: **that is a finding — report it, do
  not silently fix and re-run** unless the user already asked you to fix.
  State what you saw, what you expected, and the shortest repro.
- If a step was unverifiable (missing credential, unreachable service), list
  it explicitly as NOT VERIFIED with the reason. An honest gap beats
  manufactured confidence.

## 5. Persist the recipe

If you discovered (or corrected) anything in step 2, save it so the next run
skips discovery — `mem_save`, type `testing`, topic
`testing/verify-recipe`:

- How to build + launch (exact commands), required env/config/services,
  the ports/URLs involved, and how to drive a typical flow.
- One recipe per note; update the existing note on drift rather than
  stacking new ones. Traps discovered along the way (e.g. "port 8080 must
  be free", "seed script must run first") are their own `gotcha` notes.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "Unit tests already cover this" | Tests assert what the author thought of; the running system shows what happens. |
| "It compiles / typechecks, it'll work" | Compilation proves shape, not behavior. |
| "Launching is too much setup" | That is exactly why the recipe gets persisted — pay the cost once. |
| "I'll verify the whole app by starting it" | Boot ≠ behavior. Drive the changed flow. |
| "The env needs credentials I don't have" | Correct — so report NOT VERIFIED and ask; do not substitute weaker evidence silently. |
