---
name: test-gen
description: Use when the user wants to generate a unit test for a specific function or method, or to add tests for the riskiest untested code. Keywords - test gen, generate test, unit test, untested, coverage, tu-agent.
---

# tu-agent test gen (plugin)

Generates one verified unit test for one target symbol. The graph analysis
is deterministic (binary, via MCP); generation happens here in Claude Code.

1. **Pick the target.** If the user named a symbol, use it. Otherwise call
   the `test_gaps` MCP tool and propose the top-ranked entry to the user.
   When a coverage report is available, call `test_gaps` with `coverage`
   (a report path) or `cover: true`; the result shows per-symbol covered%,
   so prefer high-risk, low-coverage symbols.
2. **Scaffold:** call the `test_scaffold` MCP tool with the target. It
   returns JSON: `context` (signature, body, real call sites, callees,
   domain notes, blast radius), `test_path`, `run_command`, and
   `prompt_fragment`.
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

For each scaffold in the array, generate the test functions and **merge them
into that scaffold's `test_path`** (the conventional file) using the `_gen`
marker and sentinels described above — never a `*_gen*` sibling file. Dedupe
imports when a file accumulates multiple generated methods. Run each
scaffold's `run_command` to verify only its generated tests.

To generate for the highest-risk untested symbols, first call `test_gaps`
(optionally with `coverage`/`cover`), then call `test_scaffold` for each
symbol you choose to cover.

## Optional: mutation testing (advisory)

After the generated tests pass, you may run mutation testing to gauge their
strength: call the `test_mutation` MCP tool with the same target. It wraps an
external engine (go-mutesting / PIT / mutmut / Stryker) and degrades with a
note when none is installed. Surviving mutants point at assertions the
generated tests are missing — strengthen them, but this is advisory: never
block on it and never regenerate solely to chase mutants.
