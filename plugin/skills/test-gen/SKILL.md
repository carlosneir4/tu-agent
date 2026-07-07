---
name: test-gen
description: Use when the user wants to generate unit tests — for a specific function or method, a whole class, a feature or execution flow, a package/area, or the riskiest untested code. The skill opens by asking what to test and offers options. Keywords - test gen, generate test, unit test, untested, coverage, flow, feature, tu-agent.
---

# tu-agent test gen (plugin)

Generates verified unit tests for the code the user wants to cover. The graph
analysis is deterministic (binary, via MCP); generation happens here in Claude
Code.

## Start here — ask what to test

Do not make the user hunt for a symbol to pass in. **When the skill is invoked
without a clear target, open by asking what they want to test and offer
concrete options drawn from the graph:**

- **The riskiest untested code** — call `test_gaps` (add `coverage`/`cover`
  when a report is available) and show the top entries (symbol, file, risk and
  coverage) so they can pick one or several.
- **A specific function or class** — they name it (e.g. `OrderService`,
  `parsePermalink`).
- **A feature / execution flow** — they name an entry point (e.g. "the
  checkout request handler"); this runs **Flow mode** — trace the flow, then
  cover its untested steps.
- **A whole area / package** — they name it (e.g. `internal/api`); rank its
  gaps with `test_gaps`.

Let the user answer in free form ("test the payment flow", "OrderService", "the
riskiest stuff in the api package"). Map the answer to the right path:

- one symbol → the single-target pipeline below;
- a class, several symbols, or "top N" → the **Batch targets** gate;
- a feature / flow → **Flow mode**.

If the user already named a clear target when invoking the skill, skip the
question and proceed.

## Before you write any test (hard rules — do not skip)

- **Don't decide silently — keep the human in the loop.** Any judgment call
  that changes *what* gets tested is the user's to confirm, not yours to
  assume: which targets to cover or drop, how wide to make the set, which
  cases or branches matter, which technique to use, or whether to deviate
  from a convention. When you reach one, pause and ask — a short, plain
  question with your reasoning and a recommendation — so the user can
  redirect. Never quietly omit or narrow something because you judged it not
  worth it; what looks trivial or unhelpful to you may be exactly what they
  need. When unsure whether a choice is yours to make, assume it is theirs.
- **One target at a time by default.** For a multi-target run (a class,
  top-N, or several named symbols) FIRST present the plan — each symbol, its
  target file, and what its test will cover — and WAIT for the user's
  approval (they may trim the set). Then generate and verify the whole set.
  Generate strictly one-by-one only if the user asks for that. Never write
  several files in a batch without this gate.
- **Use `scaffold.run_command` verbatim.** Never infer the build or test
  command (mvn vs gradle, npx vs bunx, etc.) yourself — the binary already
  resolved it for this repo and target.
- **Conform to the repo.** If `context.conventions` reports a shared test
  base/fixture or a grouping pattern, follow it instead of inventing a new
  shape; if `context.capabilities` lists techniques (mockStatic, the mocker
  fixture…), use them. When conforming looks wrong (e.g. a base with global
  state it never restores), do NOT silently rebuild — surface the deviation
  with your reason and confirm with the user first.
- **Pre-write checklist, every file:** the `_gen` name marker and the
  `tu-agent:gen:start` / `tu-agent:gen:end` sentinels are present · you are
  MERGING, never overwriting hand-written tests · the source under test is
  untouched.

1. **Confirm the target** chosen in *Start here*. For a single named symbol,
   proceed. When a coverage report is available, call `test_gaps` with
   `coverage` (a report path) or `cover: true`; the result shows per-symbol
   covered%, so prefer high-risk, low-coverage symbols.
2. **Scaffold:** call the `test_scaffold` MCP tool with the target. It always
   returns a JSON array of scaffolds (one element for a single function; one
   per exported method for a class) — for a single-symbol target, use the
   sole element. Each scaffold has `context` (signature, body, real call
   sites, callees, domain notes, blast radius), `test_path`, `run_command`,
   and `prompt_fragment`.
3. **Generate** the test following `prompt_fragment` exactly — file name,
   package/class naming, the mandatory `_gen` test-name marker (Go
   `TestX_Gen`, Python `test_x_gen`, Java camelCase `xGen()`, TypeScript a
   `"… (gen)"` describe), and the `tu-agent:gen:start` / `tu-agent:gen:end`
   sentinels (Python/Java/TypeScript). Derive expected behavior from the call
   sites. The source under test is correct: NEVER modify it.
4. **Write or merge** at `test_path`:
   - If no file exists there, write the generated file verbatim.
   - If a file exists, MERGE — never overwrite. Read it, add the generated
     `_gen` functions (replacing any prior `_gen` functions for this target
     between the sentinels), union imports, and leave every hand-written test
     byte-for-byte intact. Go: insert the `_Gen` funcs and dedupe imports.
     Python/TypeScript: replace the sentinel region. Java: insert the methods
     inside the existing test class before its final brace.
5. **Verify:** run `run_command` via Bash from the repo root (timeout ~120s);
   it is already scoped to this target's `_gen` tests. On failure, fix the
   generated tests and re-run — at most 2 repair rounds. "no tests to run"
   counts as a failure: re-check the `_gen` naming rule.
6. **Still failing after 2 repairs:** leave the hand-written tests untouched
   and comment out the generated block under a
   `FIXME: generated tests failed verification (<short reason>)` note
   (`//` for Go/Java/TypeScript, `#` for Python), then report what failed.

Same conventional-file target, `_gen` marker, and FIXME behavior as
`tu-agent test gen`, so CLI and plugin outputs are comparable.

## Batch targets (class / top-N)

`test_scaffold` returns a **JSON array** of scaffolds:

- A **function** target → one scaffold.
- A **class** target → one scaffold per exported method.

This is a multi-target run, so the batch gate (hard rules above) applies:

1. **Plan first.** Build the plan from the scaffolds — for each, the symbol,
   its `test_path`, and the branches/cases its test will cover. Present the
   plan and **wait for approval**; the user may trim or reorder it (e.g.
   "leaf views last"). Do not write anything yet.
2. **Then generate the approved set.** For each scaffold, generate the test
   functions and **merge them into that scaffold's `test_path`** (the
   conventional file) using the `_gen` marker and sentinels — never a
   `*_gen*` sibling file. Dedupe imports when a file accumulates multiple
   generated methods. Run each scaffold's `run_command` (verbatim) to verify
   only its generated tests.

To generate for the highest-risk untested symbols, first call `test_gaps`
(optionally with `coverage`/`cover`), then call `test_scaffold` for each
symbol you choose to cover — and present that set for approval before writing.

## Flow mode (test a feature / execution flow)

When the user picks "a feature / flow" in *Start here*, cover the untested
steps along an execution path, not just one symbol.

1. **Seed.** Call the `get_flow` MCP tool on the entry symbol to get the call
   tree.
2. **Curate — GATE 1.** The static call tree is a *seed, not the truth*: it
   carries noise (small helpers, even test classes) and MISSES
   framework/registry/DI dispatch — routing, handler/view resolution,
   anything wired at runtime. Prune the noise and follow the dispatch the
   graph cannot see, reading the source where needed. Then present the
   curated flow and **WAIT for the user to approve or edit it** — they may add
   a step you missed or drop one that does not matter. Do not silently decide
   the flow's shape.
3. **Find the gaps — GATE 2.** Call `test_gaps` (with `coverage`/`cover` when
   available) and intersect it with the approved flow: keep the flow's
   symbols that are untested or low-coverage, ranked by risk. Present this as
   the plan — each symbol, its target file, what its test will cover, and any
   testability note from `context.capabilities` — and **WAIT for approval**.
   The user may trim or reorder (e.g. "leaf views last").
4. **Generate the approved set.** For each symbol, run the single-target
   pipeline below (scaffold → generate → merge → verify), conforming to
   `context.conventions`. The set is already approved, so no further gate.

Both gates are mandatory: curating the flow and choosing which gaps to cover
are exactly the judgment calls the governing rule says to confirm, not assume.

## Optional: mutation testing (advisory)

After the generated tests pass, you may run mutation testing to gauge their
strength: call the `test_mutation` MCP tool with the same target. It wraps an
external engine (go-mutesting / PIT / mutmut / Stryker) and degrades with a
note when none is installed. Surviving mutants point at assertions the
generated tests are missing — strengthen them, but this is advisory: never
block on it and never regenerate solely to chase mutants.
