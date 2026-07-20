package tdd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/subagent"
)

// StageOverlay returns the generic TDD contract overlay for a stage. It is the
// single source the plugin conductor fetches (via `tu-agent tdd overlay`) and
// the CLI references through the consts directly.
func StageOverlay(stage string) (string, bool) {
	switch stage {
	case "analyst":
		return AnalystPrompt, true
	case "architect":
		return ArchitectPrompt, true
	case "craftsman":
		return CraftsmanPrompt, true
	case "judge":
		return JudgePrompt, true
	case "review":
		return ReviewPrompt, true
	case "review-fixer":
		return ReviewFixerPrompt, true
	case "scribe":
		return ScribePrompt, true
	case "test-writer":
		return TestWriterPrompt, true
	case "implementer":
		return ImplementerPrompt, true
	case "refactor":
		return RefactorPrompt, true
	case "spec-judge":
		return SpecJudgePrompt, true
	default:
		return "", false
	}
}

// ComposeStagePrompt builds the general-purpose dispatch prompt for a stage:
// the project's agent body (role knowledge), the runtime language overlay, the
// user-owned project rules, and the generic TDD overlay — joined in that order.
// It is exposed so the plugin can dispatch general-purpose without depending on
// agent registration. It intentionally diverges from the frozen TddStageDefs
// (body + overlay only): the language overlay and rules injection are live-path
// features that the deprecated `tdd run` harness does not carry.
//
// It delegates to ComposeStagePromptWithGrounding with an empty grounding
// string, so every existing caller and test is unaffected by the grounding
// splice.
func ComposeStagePrompt(root, stage, relBase string) (string, error) {
	return ComposeStagePromptWithGrounding(root, stage, relBase, "")
}

// ComposeStagePromptWithGrounding is ComposeStagePrompt plus an optional
// mechanical grounding block (architecture excerpt, relevant decisions, blast
// radius/test gaps) supplied by the caller. internal/tdd never queries the
// graph or memory stores itself — the CMD layer computes grounding text and
// passes it in here; this function only splices it.
//
// The grounding section is inserted between the project rules and the stage
// overlay, and ONLY when both stage is a planning stage (analyst or
// architect) and grounding is non-empty. The scoping guard lives here (not
// only in the caller) so it holds structurally even if a future caller passes
// grounding text for a non-planning stage.
func ComposeStagePromptWithGrounding(root, stage, relBase, grounding string) (string, error) {
	for _, st := range tddStages() {
		if st.stage == stage {
			body, err := LoadAgentBody(root, st.role)
			if err != nil {
				return "", fmt.Errorf("tdd prompt: %w — run /tu-agent:prepare", err)
			}
			overlay := WithBaseDir(st.overlay, relBase)
			parts := []string{body}
			if lo := strings.TrimRight(codegen.LangOverlay(resolveOverlayLangForRoot(root), st.role), "\n"); lo != "" {
				parts = append(parts, lo)
			}
			if rules := loadProjectRules(root, st.role); rules != "" {
				parts = append(parts, rules)
			}
			if (stage == "analyst" || stage == "architect") && grounding != "" {
				parts = append(parts, groundingHeader+grounding)
			}
			parts = append(parts, overlay)
			return strings.Join(parts, "\n\n"), nil
		}
	}
	return "", fmt.Errorf("tdd prompt: unknown stage %q", stage)
}

// tddStage pairs a flow stage with the agent-file role that supplies its
// project knowledge, the generic TDD overlay, and the stage's tool grant.
type tddStage struct {
	stage   string
	role    string
	overlay string
	tools   []string
}

// tddStages is the single mapping of flow stages onto init-provisioned agents.
func tddStages() []tddStage {
	writeGrant := append([]string{}, CraftsmanToolGrant...) // Default + write_file
	defaultGrant := append([]string{}, DefaultToolGrant...)
	return []tddStage{
		{"analyst", "analyst", AnalystPrompt, writeGrant},
		{"architect", "architect", ArchitectPrompt, writeGrant},
		{"craftsman", "developer", CraftsmanPrompt, writeGrant},
		{"judge", "pr-reviewer", JudgePrompt, writeGrant},
		// review/review-fixer are gate-2 (whole-branch) stages fetched by the
		// plugin conductor via `tu-agent tdd prompt <name>`; they reuse the
		// existing pr-reviewer and developer roles.
		{"review", "pr-reviewer", ReviewPrompt, writeGrant},
		{"review-fixer", "developer", ReviewFixerPrompt, writeGrant},
		{"scribe", "scribe", ScribePrompt, defaultGrant},
		// test-writer/implementer are not execution stages — Run/runFeatureTDD
		// (internal/tdd/flow.go) dispatch the sandwich via the "craftsman" stage
		// name only. These two exist solely so the plugin conductor can fetch
		// their composed prompt via `tu-agent tdd prompt <name>`.
		{"test-writer", "developer", TestWriterPrompt, writeGrant},
		{"implementer", "developer", ImplementerPrompt, writeGrant},
		// refactor is not dispatched by Run/runFeatureTDD either — it exists so
		// the plugin conductor can fetch its composed prompt via `tu-agent tdd
		// prompt refactor` for architect-emitted kind:"refactor" features.
		{"refactor", "developer", RefactorPrompt, writeGrant},
		// spec-judge is the pre-code scope skeptic the plugin conductor dispatches
		// before the human gate; it only reads plan artifacts and emits a verbatim
		// verdict, so it gets the read-only defaultGrant (like scribe), not write.
		{"spec-judge", "pr-reviewer", SpecJudgePrompt, defaultGrant},
	}
}

// ValidateTddAgents returns the roles whose .claude/agents/<role>.md file is
// missing. Empty means the flow can run; otherwise the caller tells the user to
// run /tu-agent:prepare. Roles are deduplicated: test-writer/implementer share the
// "developer" role with craftsman, so a missing developer.md is reported once.
func ValidateTddAgents(root string) []string {
	var missing []string
	seen := make(map[string]bool)
	for _, st := range tddStages() {
		if seen[st.role] {
			continue
		}
		seen[st.role] = true
		if _, err := os.Stat(filepath.Join(root, ".claude", "agents", st.role+".md")); err != nil {
			missing = append(missing, st.role)
		}
	}
	return missing
}

// LoadAgentBody returns the markdown body of .claude/agents/<role>.md with the
// YAML frontmatter stripped — the role's durable project knowledge.
func LoadAgentBody(root, role string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "agents", role+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			if shell, ok := codegen.GenericShell(role); ok {
				return stripFrontmatter(shell), nil
			}
		}
		return "", fmt.Errorf("loadAgentBody(%s): %w", role, err)
	}
	return stripFrontmatter(string(data)), nil
}

// stripFrontmatter removes a leading `---`…`---` YAML block if present.
func stripFrontmatter(s string) string {
	t := strings.TrimLeft(s, "\n")
	if !strings.HasPrefix(t, "---\n") {
		return s
	}
	rest := t[len("---\n"):]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		return strings.TrimLeft(rest[i+len("\n---"):], "\n")
	}
	return s
}

// TddStageDefs builds the dispatched stage definitions (architect, craftsman,
// judge, scribe) whose system prompt = agent body + stage overlay. The analyst
// runs in the foreground and is built separately.
func TddStageDefs(root, relBase string) ([]*subagent.Definition, error) {
	var defs []*subagent.Definition
	for _, st := range tddStages() {
		if st.stage == "analyst" {
			continue
		}
		body, err := LoadAgentBody(root, st.role)
		if err != nil {
			return nil, err
		}
		defs = append(defs, &subagent.Definition{
			Name:         st.stage,
			Description:  st.stage,
			SystemPrompt: body + "\n\n" + WithBaseDir(st.overlay, relBase),
			ToolSubset:   append([]string{}, st.tools...),
		})
	}
	return defs, nil
}
