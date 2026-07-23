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
4. **Widen the net — catch notes clustered elsewhere.** A note that belongs to
   this topic can land in a different cluster (community detection is not
   perfect), which would leave the guide incomplete. Before synthesizing, look
   for related notes NOT already in this cluster:
   - Call `mem_search` with the cluster's key terms (from the label and the
     member topic-keys), and/or `mem_related` on the member notes, to surface
     candidates.
   - **Present the candidates the user might have expected here** — those not
     already in the cluster — and let the user pick which belong. Do not pull
     them in silently.
   - Read each confirmed note and treat it as source material alongside the
     cluster's own notes. Optionally call `mem_relate` with `type="related"`
     linking a confirmed note to a cluster member, so the next crystallization
     surfaces it too.
5. **Flag contradictions — with the user, non-destructively.** Across all the
   notes you will use (the cluster's plus any you pulled in), watch for pairs
   that disagree about the same thing (e.g. one note recommends one approach,
   another a conflicting one). **Present each suspected contradiction to the
   user** — do not decide silently. For each one the user confirms, record it by
   calling `mem_relate` with `type="conflicts_with"` between the two notes' ids.
   NEVER delete or edit a note — resolution (delete, rescope, or marking one
   `supersedes` the other) is the user's. If the notes genuinely conflict, say
   so in the generated skill rather than silently picking a side.
6. **Generate the SKILL.md body.** Write ONE SKILL.md — terse, imperative, skimmable:
   - Frontmatter: `name:` = the cluster label; `description:` = WHEN to use it
     (drives auto-activation for that task area).
   - Body: a checklist or table of the standard — each convention, pattern,
     gotcha, and concrete example the notes establish as an imperative WITH its
     oracle (how to know it is done or right). Prefer delimiters
     (bullets/tables/checklists) over prose paragraphs. Cut whys and narration;
     keep commands, gotchas, and exact values. A standalone guide, not links.
   - Do NOT invent facts beyond the notes. Do NOT add any provenance marker or
     HTML comment — the binary adds provenance.
7. **Save.** Call the `crystallize_save` MCP tool with `label` (the cluster
   label) and `body` (your SKILL.md). The binary stores it as the canonical
   `skill/<label>` record and materializes `.claude/skills/<label>/SKILL.md`.
   Report the path it returns.

Same record and file as `tu-agent memory crystallize <cluster>` from the CLI, so
the two paths are comparable.
