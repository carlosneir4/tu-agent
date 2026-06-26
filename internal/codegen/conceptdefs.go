package codegen

import (
	"context"
	"fmt"
	"strings"

	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/telemetry"
)

// GenerateConceptDefinitions asks the model for one definition line per card in
// a single merged call. The prompt carries each card's name, package, and
// landmark names. Output format: one "name: definition" line per concept.
// Lines for unknown names and unparseable lines are dropped; missing concepts
// keep their deterministic fallback description. tel may be nil.
func GenerateConceptDefinitions(ctx context.Context, cards []ConceptCard, prov provider.Provider, tel *telemetry.Logger) (map[string]string, error) {
	var sb strings.Builder
	sb.WriteString("For each codebase concept below, write ONE line: `<name>: <definition>`.\n")
	sb.WriteString("The definition is a single plain-English sentence saying what the concept IS\n")
	sb.WriteString("(domain meaning, not implementation). No extra lines, no markdown.\n\nConcepts:\n")
	for _, c := range cards {
		names := make([]string, 0, 5)
		for i, l := range c.Landmarks {
			if i == 5 {
				break
			}
			names = append(names, l.Name)
		}
		fmt.Fprintf(&sb, "- %s (package %s; key types: %s)\n", c.Name, c.Package, strings.Join(names, ", "))
	}
	text, err := callProviderForText(ctx, prov, sb.String(), tel)
	if err != nil {
		return nil, fmt.Errorf("GenerateConceptDefinitions: %w", err)
	}
	known := map[string]bool{}
	for _, c := range cards {
		known[c.Name] = true
	}
	defs := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		name, def, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-")), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		def = strings.TrimSpace(def)
		if known[name] && def != "" {
			defs[name] = def
		}
	}
	return defs, nil
}

// ApplyDefinitions fills card.Definition in place for every name present in defs.
func ApplyDefinitions(cards []ConceptCard, defs map[string]string) {
	for i := range cards {
		if d, ok := defs[cards[i].Name]; ok {
			cards[i].Definition = d
		}
	}
}

// IsDeterministicDescription reports whether desc is a deterministic fallback
// description produced by renderCard ("<package> — <n> files; landmarks: ...")
// rather than a generative or hand-written one. The " files; landmarks:" marker
// is unique to the fallback shape.
func IsDeterministicDescription(desc string) bool {
	return strings.Contains(desc, " files; landmarks:")
}

// SetCardDescription returns a copy of a rendered SKILL.md card (content) with
// its frontmatter description: line replaced by desc, YAML-quoted the same way
// renderCard quotes. The frontmatter name line and the entire body are preserved
// verbatim. It errors if content has no frontmatter or no description line.
//
// This is the write-back path for generative concept definitions when cards live
// in the graph store (no on-disk SKILL.md to edit): the caller reads the stored
// content, rewrites the description here, and persists it again.
func SetCardDescription(content, desc string) (string, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return "", fmt.Errorf("codegen.SetCardDescription: %w", err)
	}
	q := desc
	if strings.ContainsAny(q, `:"'`) {
		q = `"` + strings.ReplaceAll(q, `"`, `\"`) + `"`
	}
	lines := strings.Split(fm, "\n")
	replaced := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "description:") {
			lines[i] = "description: " + q
			replaced = true
			break
		}
	}
	if !replaced {
		return "", fmt.Errorf("codegen.SetCardDescription: no description line in frontmatter")
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body, nil
}

// PreservedDefinition decides the Definition a concept card should be written
// with, given the description already on disk (existing, "" when no SKILL.md
// exists). A non-empty cardDef — a fresh generative description from this run —
// always wins. When cardDef is empty (the card would render a deterministic
// fallback), a previously generated, non-fallback description is preserved so a
// model-free run (--skip-llm or an unreachable provider) does not clobber it.
// Returns "" to let renderCard build the deterministic fallback.
func PreservedDefinition(cardDef, existing string) string {
	if cardDef != "" {
		return cardDef
	}
	if existing != "" && !IsDeterministicDescription(existing) {
		return existing
	}
	return ""
}
