package codegen_test

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// supportedLangs are the four languages that have variant overlays.
var supportedLangs = []string{"go", "java", "python", "typescript"}

// variantRoles are the roles whose per-language body template was retired (all
// resolve to base now). Used by the LoadTemplate base-fallback tests.
var variantRoles = []string{"architect", "developer", "qa", "pr-reviewer", "security-reviewer"}

// overlayRoles are the roles composeStagePrompt actually injects a language
// overlay for — the dev-flow stage roles (see cmd/tu-agent tddStages). qa and
// security-reviewer are NOT dev-flow stages, so they carry no overlay.
var overlayRoles = []string{"architect", "developer", "pr-reviewer"}

// wantHeading is the language-specific section heading that each (lang, role)
// overlay must contain. Derived verbatim from
// internal/codegen/templates/<lang>/<role>.md — the single section that role
// specializes on. Substrings only (security-reviewer headings carry a trailing
// "(check all N)" qualifier that we deliberately do not pin).
var wantHeading = map[string]map[string]string{
	"go": {
		"architect":   "Go architecture rules",
		"developer":   "Go-specific rules",
		"pr-reviewer": "Go review checklist",
	},
	"java": {
		"architect":   "Java-specific design rules",
		"developer":   "Java-specific rules",
		"pr-reviewer": "Java-specific review checks",
	},
	"python": {
		"architect":   "Python-specific design rules",
		"developer":   "Python-specific rules",
		"pr-reviewer": "Python-specific review checks",
	},
	"typescript": {
		"architect":   "TypeScript-specific design rules",
		"developer":   "TypeScript-specific rules",
		"pr-reviewer": "TypeScript-specific review checks",
	},
}

// displayName maps a language key to the human display name expected to appear
// verbatim in its overlays.
var displayName = map[string]string{
	"go":         "Go",
	"java":       "Java",
	"python":     "Python",
	"typescript": "TypeScript",
}

// @s1 — every (supported-lang, overlay-role) pair returns a non-empty overlay
// that contains the language display name and the section heading that pair
// specializes on. Overlays exist only for the dev-flow stage roles that
// composeStagePrompt injects: 4 langs × 3 roles = 12 non-empty overlays.
func TestLangOverlay_AllPairsNonEmptyWithHeading(t *testing.T) {
	count := 0
	for _, lang := range supportedLangs {
		for _, role := range overlayRoles {
			lang, role := lang, role
			t.Run(lang+"/"+role, func(t *testing.T) {
				got := codegen.LangOverlay(lang, role)
				if got == "" {
					t.Fatalf("LangOverlay(%q, %q) = empty, want non-empty overlay", lang, role)
				}
				if !strings.Contains(got, displayName[lang]) {
					t.Errorf("LangOverlay(%q, %q) does not contain display name %q", lang, role, displayName[lang])
				}
				heading := wantHeading[lang][role]
				if !strings.Contains(got, heading) {
					t.Errorf("LangOverlay(%q, %q) missing specialization heading %q\noverlay:\n%s", lang, role, heading, got)
				}
			})
			count++
		}
	}
	if count != 12 {
		t.Fatalf("expected to exercise 12 (lang, role) pairs, exercised %d", count)
	}
}

// @s2 — overlays are injected raw, so none may carry a Go template token.
func TestLangOverlay_NoTemplateTokens(t *testing.T) {
	forbidden := []string{"{{", "{{.TestCommand}}", "{{.BuildTool}}"}
	for _, lang := range supportedLangs {
		for _, role := range overlayRoles {
			got := codegen.LangOverlay(lang, role)
			for _, tok := range forbidden {
				if strings.Contains(got, tok) {
					t.Errorf("LangOverlay(%q, %q) contains template token %q; overlays must be injected raw", lang, role, tok)
				}
			}
		}
	}
}

// @s3 — roles that are not dev-flow stages (process-only analyst/scribe, plus
// qa and security-reviewer which no stage composes) have no language overlay.
func TestLangOverlay_ProcessOnlyRolesEmpty(t *testing.T) {
	for _, lang := range supportedLangs {
		for _, role := range []string{"analyst", "scribe", "qa", "security-reviewer"} {
			if got := codegen.LangOverlay(lang, role); got != "" {
				t.Errorf("LangOverlay(%q, %q) = %q, want \"\" (not an injected stage role)", lang, role, got)
			}
		}
	}
}

// @s4 — an unsupported or empty language yields no overlay.
func TestLangOverlay_UnsupportedLanguageEmpty(t *testing.T) {
	if got := codegen.LangOverlay("rust", "developer"); got != "" {
		t.Errorf("LangOverlay(\"rust\", \"developer\") = %q, want \"\"", got)
	}
	if got := codegen.LangOverlay("", "developer"); got != "" {
		t.Errorf("LangOverlay(\"\", \"developer\") = %q, want \"\"", got)
	}
}

// @s5 — an unknown role yields "" rather than a panic.
func TestLangOverlay_UnknownRoleEmptyNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LangOverlay(\"go\", \"not-a-role\") panicked: %v", r)
		}
	}()
	if got := codegen.LangOverlay("go", "not-a-role"); got != "" {
		t.Errorf("LangOverlay(\"go\", \"not-a-role\") = %q, want \"\"", got)
	}
}

// @s6 — the overlay tree is embedded, not read from disk: after changing the
// working directory away from the repo root, go/developer content is still
// served (from the embed.FS).
func TestLangOverlay_ServedFromEmbedAfterChdir(t *testing.T) {
	t.Chdir(t.TempDir())

	got := codegen.LangOverlay("go", "developer")
	if got == "" {
		t.Fatalf("LangOverlay(\"go\", \"developer\") = empty after chdir; overlay must be served from embed.FS")
	}
	if !strings.Contains(got, wantHeading["go"]["developer"]) {
		t.Errorf("LangOverlay(\"go\", \"developer\") after chdir missing %q; got:\n%s", wantHeading["go"]["developer"], got)
	}
}
