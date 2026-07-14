---
name: performance-investigation
description: Use when something is slow, uses too much memory, or needs optimizing — BEFORE changing any code. Requires a baseline measurement, a profile identifying the dominant cost, one change at a time, and a re-measurement proving the delta; speculative optimization is refused. Keywords - slow, performance, optimize, latency, memory usage, profiling, benchmark, hot path, tu-agent.
---

# tu-agent performance-investigation

Performance work is `systematic-debugging` where the bug is a number. The
failure mode is identical too: "optimizing" the place that *looks* slow,
which complicates the code without moving the number.

## The iron law

**No optimization without a baseline measurement showing the problem and a
re-measurement showing the delta.** "This should be faster now" is not a
result; two numbers under the same conditions are.

## 0. Recall

`mem_search("performance <area>")` — prior investigations, known hot paths,
and benchmarks that already exist beat re-deriving them.

## 1. Define the number, then baseline it

- Which metric, on which operation, under which load: "p95 latency of the
  search endpoint at 50 rps", "wall time of the import job over 100k rows",
  "resident memory after 1h idle". "It feels slow" is not investigable.
- What would satisfy? A target (or "as fast as before commit X") prevents
  endless polishing.
- Measure the baseline reproducibly: release/optimized build, warm-up
  accounted for, several runs (report median + spread — a single run is an
  anecdote), same machine/conditions you will re-measure on. Language
  benchmark harnesses (e.g. `go test -bench`, JMH, pytest-benchmark,
  hyperfine for CLIs) are worth the setup: they become the regression guard.

## 2. Profile before hypothesizing

Find where the time/memory actually goes — profiler for the language
(pprof, JFR/async-profiler, cProfile/py-spy, `node --prof`/clinic), or
coarse timing logs around the suspected phases when no profiler fits. The
graph helps scope (`get_flow`/`get_context` for what the operation touches),
the profile decides. Intuition about hot paths is wrong often enough that
measuring first is cheaper on average.

## 3. Attack the dominant cost

- Amdahl's law does the triage: a phase consuming 5% of the time caps your
  win at 5% — skip it no matter how ugly it looks.
- State a mechanism before changing: "N+1 queries — batch them", "O(n²) scan
  under the loop — index/map it", "allocation per item in the hot loop —
  reuse the buffer". A named mechanism predicts the size of the win; no
  mechanism, no change.
- **One change per measurement.** Two changes at once = you can't attribute
  the delta, and one of them may be a regression the other masks.

## 4. Re-measure, same conditions

- Same benchmark, same load, same machine. Report both numbers and the
  delta ("p95 420ms → 180ms, −57%").
- No improvement → revert the change, kill the hypothesis, back to the
  profile. Complexity without a delta is pure cost.
- Check the trade: faster but 3× memory, or faster-but-unreadable, is a
  decision the user makes, not a default.

## 5. Guard and capture

- Keep the benchmark in the repo where the suite can run it — today's fix
  is next quarter's regression otherwise.
- Correctness verification still applies (`tu-agent:verification-before-completion`):
  an optimization that flips a test is a bug with good intentions.
- `mem_save` what was learned: the hot path, the mechanism, the numbers —
  `decision/perf-<area>` or a `gotcha` if the trap generalizes.

## Rationalizations to refuse

| The excuse | Why it is wrong |
|---|---|
| "This code is obviously inefficient, just fix it" | Obvious-looking code is on the cold path more often than not. Profile. |
| "Caching will solve it" | Caching is a mechanism guess wearing a solution costume. Profile first; cache what's hot *and* re-computable. |
| "I optimized 4 things, overall it's faster" | Which paid? Which regressed? Four changes, zero attribution. |
| "Debug build numbers are fine for comparing" | Optimizers reorder everything; debug ratios routinely lie. |
| "One quick run shows it's faster" | Noise regularly exceeds real deltas. Median of several runs. |
| "It's faster; readability can suffer a bit" | That trade has an owner, and it isn't you. Present both versions' costs. |
