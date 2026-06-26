package codegen_test

import (
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/codegen"
)

func TestFrontmatterField_Quoted(t *testing.T) {
	content := "---\nname: foo\ndescription: \"hello world\"\ntools: a, b\n---\nbody\n"
	if got := codegen.FrontmatterField(content, "description"); got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestFrontmatterField_Unquoted(t *testing.T) {
	content := "---\nname: foo\ndescription: plain text here\n---\n"
	if got := codegen.FrontmatterField(content, "description"); got != "plain text here" {
		t.Errorf("got %q, want %q", got, "plain text here")
	}
}

func TestFrontmatterField_Missing(t *testing.T) {
	content := "---\nname: foo\ntools: a\n---\nbody\n"
	if got := codegen.FrontmatterField(content, "description"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFrontmatterField_NoFrontmatter(t *testing.T) {
	if got := codegen.FrontmatterField("just a body\n", "description"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSpecialistsBlock_Empty(t *testing.T) {
	if got := codegen.SpecialistsBlock(nil); got != "" {
		t.Errorf("got %q, want empty for no specialists", got)
	}
}

func TestSpecialistsBlock_ListsNamesAndDescriptions(t *testing.T) {
	block := codegen.SpecialistsBlock([]codegen.AgentRef{
		{Name: "nextjs-expert", Description: "Expert for Next.js apps"},
		{Name: "graphql-expert", Description: "Expert for the GraphQL backend"},
	})
	if !strings.HasPrefix(block, codegen.SpecialistsOpen) {
		t.Errorf("block must start with open marker, got:\n%s", block)
	}
	if !strings.HasSuffix(strings.TrimRight(block, "\n"), codegen.SpecialistsClose) {
		t.Errorf("block must end with close marker, got:\n%s", block)
	}
	for _, want := range []string{"nextjs-expert", "Expert for Next.js apps", "graphql-expert", "Expert for the GraphQL backend"} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
}
