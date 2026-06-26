package testgen

import (
	"fmt"
	"strings"
)

// SystemPrompt instructs the model for both generation and repair calls.
const SystemPrompt = `You are a senior engineer writing one unit test file for one target symbol.
The source under test is correct — never propose changes to it.
Respond with a single fenced code block containing the complete test file and nothing else.`

// BuildGenerationPrompt renders the initial user message.
func BuildGenerationPrompt(gc *GenContext, fragment, testPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write a unit test for `%s` (%s).\n\n", gc.Target.Name, gc.Target.Path)
	fmt.Fprintf(&b, "## Target\n\nSignature: `%s%s %s`\nBlast radius: %d dependent nodes.\n\n```\n%s\n\n%s\n```\n\n",
		gc.Target.Name, gc.Target.Params, gc.Target.ReturnType, gc.BlastRadius, gc.PackageClause, gc.Body)
	if len(gc.CallSites) > 0 {
		b.WriteString("## Real call sites (derive expected behavior from these)\n\n")
		for _, cs := range gc.CallSites {
			fmt.Fprintf(&b, "%s:\n```\n%s\n```\n\n", cs.Caller, cs.Snippet)
		}
	}
	if len(gc.Callees) > 0 {
		b.WriteString("## Callees (signatures only)\n\n")
		for _, c := range gc.Callees {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}
	if gc.SkillExcerpt != "" {
		fmt.Fprintf(&b, "## Domain notes\n\n%s\n\n", gc.SkillExcerpt)
	}
	fmt.Fprintf(&b, "## Conventions\n\n%s\n", fragment)
	return b.String()
}

// BuildRepairPrompt renders the user message after a failed attempt. Calls
// are stateless single turns, so the full context is repeated.
func BuildRepairPrompt(gc *GenContext, fragment, testPath, currentTest, failure string) string {
	var b strings.Builder
	b.WriteString(BuildGenerationPrompt(gc, fragment, testPath))
	fmt.Fprintf(&b, "\n## Previous attempt (failed verification)\n\n```\n%s\n```\n\n## Failure output\n\n```\n%s\n```\n\nFix the test. The source under test is correct — only the test may change. Respond with the complete corrected file in one fenced code block.\n",
		currentTest, failure)
	return b.String()
}

// ExtractCode returns the contents of the first fenced code block in a model
// response. Responses with no fence that already look like a source file are
// used verbatim.
func ExtractCode(response string) (string, error) {
	if i := strings.Index(response, "```"); i >= 0 {
		rest := response[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:] // drop the language tag line
			if k := strings.Index(rest, "```"); k >= 0 {
				code := strings.TrimRight(rest[:k], "\n") + "\n"
				if strings.TrimSpace(code) != "" {
					return code, nil
				}
			}
		}
		return "", fmt.Errorf("testgen.ExtractCode: unterminated or empty code fence")
	}
	trimmed := strings.TrimSpace(response)
	for _, p := range []string{"package ", "import ", "// ", "/*"} {
		if strings.HasPrefix(trimmed, p) {
			return trimmed + "\n", nil
		}
	}
	return "", fmt.Errorf("testgen.ExtractCode: no code block in response")
}
