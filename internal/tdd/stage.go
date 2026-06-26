package tdd

import (
	"context"
	"fmt"
)

// Dispatcher runs a named non-interactive sub-agent and returns its final text.
// Satisfied by *subagent.Dispatcher.
type Dispatcher interface {
	Dispatch(ctx context.Context, agent, task string) (string, error)
}

// StageRunner dispatches a stage agent and parses its contract.
type StageRunner struct {
	D Dispatcher
}

// Run dispatches agent with task and parses the returned contract.
func (r StageRunner) Run(ctx context.Context, agent, task string) (Contract, error) {
	out, err := r.D.Dispatch(ctx, agent, task)
	if err != nil {
		return Contract{}, fmt.Errorf("tdd.StageRunner.Run(%s): %w", agent, err)
	}
	c, err := ParseContract(out)
	if err != nil {
		return Contract{}, fmt.Errorf("tdd.StageRunner.Run(%s): %w", agent, err)
	}
	return c, nil
}

// DefaultToolGrant is wired into every stage agent (D8): skills + graph (via
// `tu-agent graph ...` through bash) + memory. Native CLI has no graph MCP tools,
// so graph/test-gen access is through bash.
var DefaultToolGrant = []string{
	"read_file", "grep", "find", "list_dir", "bash",
	"load_skill", "mem_search", "mem_recent", "mem_save",
}

// CraftsmanToolGrant adds write access for the TDD stage.
var CraftsmanToolGrant = append(append([]string{}, DefaultToolGrant...), "write_file")

const contractInstruction = "\n\nEnd your reply with a single fenced ```json block " +
	"containing your contract: {stage, status, complexity?, artifacts[], scenarios[], " +
	"risks[], assumptions[], handoff, verdict?}. Write all real output to files and " +
	"reference them by path — never paste file contents into chat."

// AnalystPrompt is the TDD-stage overlay for the interrogation stage. It is
// appended to the project's analyst agent body at dispatch.
const AnalystPrompt = `tu-agent TDD task — analyst stage. Ignore any default output format
from your role definition; produce exactly what this task asks. BEFORE your first question,
pre-load context: recall memory (mem_search) and load the graph for the affected area
(get_concept/get_context) so you interrogate from real context, not from zero. Then converse
with the user to produce .tu-agent/tdd/spec.md before any design or code. Ask exactly ONE
question per turn. On non-trivial decisions propose >=2 options and record the chosen one with
its reason; mark unresolved points "OPEN QUESTION". When the spec is complete, write
.tu-agent/tdd/spec.md (purpose, contract, edge cases, decisions+why) and only then emit a
contract with status "pass".` + contractInstruction

// ArchitectPrompt is the TDD-stage overlay for design + Gherkin + complexity.
const ArchitectPrompt = `tu-agent TDD task — architect stage. Ignore any default output format
from your role definition; produce exactly the contract below. Read .tu-agent/tdd/spec.md. You
MUST consult the graph for blast-radius before classifying: run get_impact/get_context on the
affected symbols. CLASSIFY the task complexity from that blast-radius and set the contract
"complexity" field:
- trivial: no new behavior to test (status "pass", complexity "trivial"). No Gherkin, no TDD.
- standard: a bounded area — local dependents within a single domain/subsystem. ONE feature:
  write .tu-agent/tdd/features/<slug>.feature with @s1..@sn tagged scenarios (each Then asserts
  something measurable), then status "pass", complexity "standard", and return
  "features": [{"name":"<slug>","scenarios":["@s1",...]}].
- complex: WIDE blast-radius — spans more than one domain/subsystem, or many dependents.
  DECOMPOSE into several small, independently-testable sub-features: write one .feature per
  sub-feature under .tu-agent/tdd/features/<slug>.feature (unique slugs, each with its own @s
  scenarios in dependency order), then status "pass", complexity "complex", and return
  "features" with one entry per sub-feature.
Keep slugs unique.` + contractInstruction

// CraftsmanPrompt is the TDD-stage overlay for strict TDD with a test-gen safety net.
const CraftsmanPrompt = `tu-agent TDD task — craftsman stage. Ignore any default output format
from your role definition; produce exactly the contract below. Implement ONE approved feature
by strict TDD (Red -> Green -> Refactor, one test at a time). Work ONE @s scenario at a time:
write its single failing test FIRST, run the suite and CONFIRM it fails (red) for the right
reason, THEN write the minimal production code to make it pass (green), then refactor. Never
write production code before its failing test, and never batch several scenarios'
implementation and add tests afterward; that defeats TDD and leaves vacuous tests the mutation
gate flags as survivors. Before the first scenario, check whether the code you will touch has
tests ("tu-agent test gaps" / graph tested_by); if it has NONE, run "tu-agent test gen
<target>" to lay a safety net BEFORE the cycle. Greenfield code is hand-written test-first.
Write a @s->test map to .tu-agent/tdd/progress/tdd_<name>.md. In the contract, "scenarios"
MUST list every @s tag covered with a concrete test. Address each judge-feedback point if any.
Report the primary source file as an artifact {"kind":"source","path":"<repo-relative>"} so
the mutation gate can target it.` + contractInstruction

// JudgePrompt is the TDD-stage overlay for gate 1 (design/discipline review).
const JudgePrompt = `tu-agent TDD task — judge stage. Ignore any default output format from your
role definition; produce exactly the contract below. The deterministic gate (tests green +
every @s covered) already passed. Judge DESIGN and DISCIPLINE only: short functions, revealing
names, no duplication, correct error contract (stderr + exit code), and NO production code that
no failing test demanded (scope creep). You do not edit code — you prune. Write your review to
.tu-agent/tdd/progress/judge_<name>.md and set contract.verdict to {result: pass|revise|fail,
feedback, score}. Be concrete: cite file:line.` + contractInstruction

// ScribePrompt is the TDD-stage overlay for phase 3 (archive to memory).
const ScribePrompt = `tu-agent TDD task — scribe stage. Ignore any default output format from
your role definition; produce exactly what this task asks. The feature is complete and all
gates passed. Read .tu-agent/tdd/spec.md and the .tu-agent/tdd/progress/ notes, then call
mem_save once with topic "decision/<feature-slug>" and content capturing WHAT changed and WHY
(decision, rationale, scenarios covered, files touched). Be concise and durable. Do not edit
code.` + contractInstruction
