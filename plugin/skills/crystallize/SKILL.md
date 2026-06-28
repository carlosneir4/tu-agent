---
name: crystallize
description: Use when the user wants to consolidate a dense cluster of related memory notes into a reusable project skill (the "start here" standard for a task area). Keywords - crystallize, consolidate notes, make a skill, standard, cluster, tu-agent.
---

# tu-agent crystallize (plugin)

Turns a dense cluster of related memory notes into one project skill. Detection
is deterministic (the binary, via MCP); the skill body is generated here in
Claude Code; storage + materialization happen in the binary so the result is a
single canonical record shared via the memory chunk.

## Flow

1. **List candidates.** Call the `mem_clusters` MCP tool. It returns the dense
   note clusters (label, member topic-keys, size). If none are dense enough,
   tell the user and stop.
2. **Pick one — with the user.** Present the clusters and let the user choose
   which to crystallize (or confirm the top one). Do not pick silently.
3. **Read the member notes.** For the chosen cluster, read its member notes'
   content (via `mem_search` / the notes already shown) so you synthesize from
   real facts, not assumptions.
4. **Flag contradictions — with the user, non-destructively.** While reading the
   member notes, watch for pairs that disagree about the same thing (e.g. one
   note recommends one approach, another recommends a conflicting one).
   **Present each suspected contradiction to the user** — do not decide silently.
   For each one the user confirms, record it by calling `mem_relate` with
   `type="conflicts_with"` between the two notes' ids. NEVER delete or edit a
   note — resolution (delete, rescope, or marking one `supersedes` the other) is
   the user's. If the notes genuinely conflict, say so in the generated skill
   rather than silently picking a side.
5. **Generate the SKILL.md body.** Write ONE SKILL.md document:
   - YAML frontmatter: `name:` = the cluster label; `description:` stating WHEN
     to use the skill so it auto-activates for that task area.
   - Body: the consolidated standard — conventions, patterns, gotchas, and
     concrete examples the notes establish. A standalone guide, not links.
   - Do NOT invent facts beyond the notes. Do NOT add any provenance marker or
     HTML comment — the binary adds provenance.
6. **Save.** Call the `crystallize_save` MCP tool with `label` (the cluster
   label) and `body` (your SKILL.md). The binary stores it as the canonical
   `skill/<label>` record and materializes `.claude/skills/<label>/SKILL.md`.
   Report the path it returns.

Same record and file as `tu-agent memory crystallize <cluster>` from the CLI, so
the two paths are comparable.
