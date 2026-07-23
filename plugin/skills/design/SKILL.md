---
name: design
description: Use for a new feature or subsystem designed from zero — architecture, greenfield, brainstorm — before any spec or code exists. A proportionality-gated architecture guild (a panel of production lenses) critiques a from-zero design and the human picks what enters it; converses, never decides for the human. Chain position: groundwork (single change) → design (new feature/subsystem from zero) → tdd (implementation). Keywords - design, architecture, new feature, subsystem, from zero, greenfield, brainstorm, tu-agent.
---

# tu-agent design — the architecture guild

Produces an ant-sized design for an ant-sized problem: from-zero, proportionality-
gated, human-in-the-loop. Chain position: `groundwork` (single change) →
**`design` (new feature/subsystem from zero)** → `tdd` (implementation). This
fills the slot that brainstorming previously held.

The shape is a **guild, not a lone architect**: once a base design is drafted,
a panel of production lenses (Security, Operations, Reliability, Data,
Contracts, Quality) critiques it and proposes ideas. The skill converses — it
exists to give the human ideas and record their choices, never to decide for
them. Every idea the guild returns still has to earn its place: proportionality
is the rule that runs through the whole flow, not just Step 5.

## Step 1 — Anchor

Even a greenfield design lives inside an existing codebase. Before asking the
human anything, gather what the repo already knows:

- `get_architecture` — where the new area will sit relative to the whole.
- `get_context(<neighboring-file-or-symbol>)` — conventions, dependents, and
  tests already touching the area this will live next to.
- `mem_search <area>` — prior decisions and gotchas that carry the "why".

If a graph tool you expect (e.g. `get_architecture`, `get_context`) is absent
from your active tool list, it is likely deferred, not missing — load it with
your tool-search mechanism before concluding the project has no graph.

## Step 2 — Interrogate the forces

Ask the human ONE question at a time — this is an interrogation, not a form.
Collect:

- **Goal in one sentence.** If it takes two sentences, it is not one goal yet.
- **Scale facts**: users, data volume, change frequency, number of consumers,
  team size maintaining it.
- **Hard constraints**: dependency policy, latency budget, compliance
  requirements.
- **Production tier** — one of:
  - throwaway script,
  - internal tool,
  - production service.

  This single choice calibrates how much guild scrutiny the design earns in
  Step 5, so pin it down explicitly rather than inferring it.

Facts, not wishes: **"future flexibility" is not a force.** A force is a
scale fact, a constraint, or a consumer that exists today (or is contractually
committed to exist by a known date) — never a feeling that something might be
useful eventually.

## Step 3 — Size first

Pick the LIGHTEST shape that satisfies the declared forces, in this order:

```
function  <  module  <  package-with-interface  <  subsystem-with-boundary
```

For whichever shape you propose, state explicitly why the next size DOWN
fails — which declared force from Step 2 a smaller shape cannot satisfy. If
you cannot name that force, the smaller shape is the right answer, not the
one you first reached for.

## Step 4 — Pattern budget

Every pattern in the design (interface, adapter, queue, cache, abstraction
layer, event bus, …) must name the concrete force from Step 2 that demands it
**today**. A pattern with no named force is deleted from the design — not
deferred, not simplified, deleted.

## Step 5 — Convene the guild

The guild has a fixed CATALOG of lenses. The production tier from Step 2 plus
the declared forces select WHICH lenses convene:

| Tier | Guild roster | Scrutiny |
|---|---|---|
| throwaway script | no guild | sketch only |
| internal tool | Security + Quality | checklist pass |
| production service | full roster | full critique |

Show the user the roster you selected and WHY (the tier that produced it),
then let them add or remove lenses before dispatch — the table is a default, not a mandate.

**Dispatch mechanism.** Named agents like `security-reviewer` or `qa` are not
dispatchable from inside a skill session (in-session agent names do not
resolve to a dispatch target). Each lens instead runs as a **general-purpose**
agent with an inline-composed prompt built from four parts — nothing the lens
needs is left for it to go fetch:

1. **Lens name + role framing.** For the Security lens: "You are the Security
   lens. Review this draft design the way the security-reviewer agent would —
   trust boundaries, authn/z, input validation, secret handling,
   dependency/supply-chain risk." For the Quality lens: "You are the Quality
   lens. Review this draft the way the qa agent would — testability of the
   boundaries, seams for mocking, the test pyramid for this shape." The other
   lenses get the equivalent framing from their own checklist below.
2. **The lens checklist** — that lens's table rows, inlined into the prompt so
   the agent has the full concern list without loading this skill itself.
3. **The draft design + declared forces** — the Step 3-4 output: the chosen
   shape, the patterns kept, the tier, and the Step 2 scale facts.
4. **The ideas contract**: "Return 2 to 4 concrete ideas. Each idea states:
   (a) what to add or change; (b) the DECLARED force that justifies it — no
   force, no idea; (c) the cost of skipping it. Do not exceed 4."

Dispatch every selected lens in parallel — they review the same draft
independently, so there is nothing to sequence. Proportionality applies
TWICE in this step: the tier selects the roster (the table above), and every
returned idea must still cite a declared force — an internal CLI does not
earn a Kubernetes review just because a lens was convened for it.

### Lens catalog

**Security** (frames its review the way the security-reviewer agent would):

| Concern | What to check |
|---|---|
| Trust boundaries | Where does untrusted input cross into this component? |
| AuthN / authZ | Who is allowed to call this, and is that enforced at the boundary, not assumed? |
| Input validation | Is every external input validated or sanitized before use? |
| Secret handling | Are secrets loaded from environment/vault, never hardcoded or logged? |
| Dependency / supply-chain | Does this add a dependency, and is it justified and pinned? |

**Operations / deployment**:

| Concern | What to check |
|---|---|
| Config & environments | Does this need new config, and does it vary by environment? |
| Deploy / rollback | Can this ship and roll back independently of everything else? |
| Migrations | Does this require a schema or data migration, and is it reversible? |
| Observability | What logs, metrics, health checks, and alerts exist when it breaks? |
| Runbook basics | Could an on-call engineer diagnose a failure from what this produces? |

**Reliability & performance**:

| Concern | What to check |
|---|---|
| Failure modes & graceful degradation | What happens when a dependency is down or slow? |
| Timeouts / retries / idempotency | Does every boundary call have a timeout, and is retrying safe? |
| Load vs. capacity | What load is expected at the declared scale, and does the shape handle it? |
| Hot paths & caching | Is there an obvious hot path, and does it need caching? |
| Cost of the chosen shape | What does this shape cost at the declared scale — compute, latency, ops burden? |

**Data & privacy**:

| Concern | What to check |
|---|---|
| Persistence choice | Does the storage choice match the access pattern and volume? |
| Schema evolution | Can the schema change later without a painful migration? |
| Backup / restore | Is there a backup story, and has restore ever actually been tested? |
| Retention | How long is data kept, and is that a declared policy, not an assumption? |
| PII & compliance | Does this touch personal data, and what compliance constraint applies? |

**Contracts** (only when the design exposes an API or interface others
consume — otherwise this lens is not convened):

| Concern | What to check |
|---|---|
| Contract clarity | Is the request/response (or event) shape documented and unambiguous? |
| Versioning | How does a breaking change reach consumers without breaking them today? |
| Backward compatibility | Can existing consumers keep working through a rollout? |
| Error surface | Are error cases part of the contract, or an afterthought? |

**Quality** (frames its review the way the qa agent would):

| Concern | What to check |
|---|---|
| Testability of boundaries | Can each boundary be tested without the whole system running? |
| Seams for mocking | Are dependencies injected rather than reached for globally, so they can be faked? |
| Test pyramid for this shape | What is the right mix of unit / integration / end-to-end for this size? |

## Step 6 — Human chooses

Present the guild's ideas as a menu grouped by lens — one line plus its
justification (the force it cites) per idea. For each idea the human picks
exactly one:

- **accept** — becomes a scenario or a design constraint for `tdd`;
- **defer** — goes into the design's `## Non-goals` section with a reason, so
  it reads as consciously postponed, not missed;
- **discard** — dropped, no trace needed beyond the conversation.

Deferred ideas are recorded (in the design doc's Non-goals, with the reason)
so the next design session on this area starts from where this one left off
instead of re-litigating the same idea from scratch.

## Step 7 — Rocket detector

A skeptic pass over the design AFTER the guild has added its ideas. For every
component and every pattern still in the design, ask this question:

> "delete it; what breaks today?"

If nothing breaks, it comes out — no exceptions for patterns that merely feel
prudent.

> the guild adds; the detector prunes; the human arbitrates both.

Neither the guild nor the detector has final say — every addition and every
deletion still passes through Step 6's human choice.

Use this table to catch the rationalizations that usually defeat the
detector — each one sounds like a reason, and each one collapses under its
counter-question:

| Rationalization | Counter-question |
|---|---|
| "It's more scalable" | Which declared force needs that scale today? |
| "We'll need it later" | Is "later" a declared force with a date, or a wish? What breaks without it now? |
| "The pattern is best practice" | Best practice for what force, at what scale — does THIS problem have that force? |
| "Everyone uses [queue/microservice/framework] for this" | Does this system have the force that made that the right call somewhere else? |
| "It's cleaner with an interface" | Is there a second implementation today, or a declared, dated reason there will be? |
| "Future-proofing" | Future flexibility is not a force — what PRESENT force does this satisfy? |

## Step 8 — Output

Write the design as two artifacts, then hand off:

1. **architecture sketch** — components, boundaries, and data flow, described
   in prose, plus exactly ONE mermaid diagram showing the same shape visually.
   For example, a generic fictional Go HTTP service accepting webhooks:

   ```mermaid
   flowchart LR
       Client -->|POST /webhooks| Handler
       Handler -->|validate| Validator
       Handler -->|enqueue| Queue[(Queue)]
       Worker -->|consume| Queue
       Worker -->|persist| Store[(Store)]
   ```

2. **ADR-style decision record**, written via
   `mem_save` with topic `decision/<area>-architecture`, capturing:
   - **chosen** — the shape and patterns that survived Steps 3-4 and 7;
   - **rejected** — ideas the human discarded in Step 6, and why;
   - **deferred** — ideas the human deferred, with the reason, mirroring what
     went into the design doc's Non-goals;
   - **forces** — the Step 2 facts and constraints that drove every choice
     above, so a future reader can tell whether a force has since changed.

When the human is ready to proceed, hand off to `/tu-agent:tdd` seeded with
the design doc you just wrote — the tdd skill's Step 1 (Analyst) already has a
design-doc seeding path: point it at this design doc and it reads the doc,
seeds the spec from it, and asks only about gaps or contradictions instead of
interrogating from zero. That is the same hand-off point whether the design
doc came from this skill or from any other source.
