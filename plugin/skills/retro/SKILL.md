---
name: retro
description: Use on demand at the end of a work session to reflect on it — classify re-prompts, corrections, and guardrail violations, then capture the durable behavioral patterns as memory notes. Keywords - retro, retrospective, what went wrong, correcciones, re-prompt, behavior, tu-agent.
---

# tu-agent retro (plugin)

On-demand session retrospective. It turns friction from THIS session into durable
memory: it classifies where work had to be redone or redirected, cross-checks the
deterministic signals the binary already measured, and saves the patterns worth
remembering so the next session avoids them.

This is **generative and on-demand** — never wire it into a hook or run it
automatically. Run it when the user asks for a retro, or when a session had
visible friction worth capturing.

Define `TU="${CLAUDE_PLUGIN_ROOT}/bin/tu-agent"`.

## Steps

1. **Preflight:** run `"$TU" version`; require ≥ 0.3. If the binary is missing,
   tell the user to install tu-agent and stop.

2. **Pull the deterministic signals.** Run `"$TU" advise` and, if the user wants
   detail, `"$TU" stats --insights`. These are the counts the binary already
   measured (crystallize-ready clusters, edit-without-context and secret-guard
   violations, mem_search zero-result rate). Treat them as evidence, not
   conclusions — `advise` tells you *that* something happened N times; the retro
   explains *why* and *what to change*.

3. **Classify this session.** Using the conversation in context (and, if you need
   older detail, the session transcript under `~/.claude/projects/<repo-slug>/`),
   look for three kinds of friction. Be honest and specific:
   - **Re-prompts** — the user had to restate or clarify the same request more
     than once. Signal: a missing or wrong assumption on the first attempt.
   - **Corrections** — the user rejected, undid, or redirected work (wrong
     approach, wrong file, wrong scope). Signal: a convention or constraint that
     wasn't known up front.
   - **Violations** — the concrete guardrail hits `advise` surfaced
     (edit-without-context, secret-guard). Signal: a workflow step that was
     skipped.

4. **Keep only what's durable.** A one-off typo or a normal clarification is NOT
   worth saving. Save a pattern only if it is (a) likely to recur and (b)
   generalizable into a rule. When in doubt, ask the user before saving. Prefer a
   rule the user can act on ("query get_context before editing files in package
   X") over a description of what happened.

5. **Capture, without duplicating.** For each durable pattern:
   - First `mem_search` the topic area — if a note already covers it, refine that
     note instead of adding a near-duplicate.
   - Save one **atomic** note per pattern with `mem_save` (the graph MCP tool, or
     `"$TU" memory save`): topic `bug-pattern/behavior-<short-slug>`, type
     `bug-pattern`. Include, in the body, a one-line **Why:** (the root cause) and
     a one-line **How to apply:** (the concrete change next time). One trap per
     note — never bundle several.
   - If a correction points at a repo-wide rule the user keeps re-teaching,
     suggest they add a line to `.tu-agent/rules.md` (the user-owned rules file
     that survives regeneration) instead of, or in addition to, a memory note.

6. **Report.** Summarize what you classified and which notes you saved (topic +
   one line each), so the user can veto any of them. Do not save anything the
   user rejected.

## Notes

- Generic examples only. Never bake a specific codebase's names into a saved note
  unless that name is genuinely the subject of the pattern.
- retro reads and reflects; the only thing it writes is memory notes (and only the
  ones the user is fine with). It never edits code or settings.
