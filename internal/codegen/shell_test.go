package codegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

// TestGenericShell_FullAgentFile (@s1) asserts each of the 7 roles yields a full
// embedded agent file — frontmatter block, no unrendered template tokens.
func TestGenericShell_FullAgentFile(t *testing.T) {
	if len(codegen.AgentRoles) != 7 {
		t.Fatalf("expected 7 roles, got %d: %v", len(codegen.AgentRoles), codegen.AgentRoles)
	}
	for _, role := range codegen.AgentRoles {
		s, ok := codegen.GenericShell(role)
		if !ok {
			t.Errorf("GenericShell(%q) = (_, false), want ok=true", role)
			continue
		}
		if !strings.HasPrefix(strings.TrimLeft(s, "\n"), "---") {
			t.Errorf("GenericShell(%q) must start with a YAML frontmatter block %q, got prefix %q", role, "---", head(s))
		}
		if strings.Contains(s, "{{") {
			t.Errorf("GenericShell(%q) contains unrendered template token %q", role, "{{")
		}
	}
}

// TestGenericShell_UnknownRole (@s2) — an unknown role reports (_, false).
func TestGenericShell_UnknownRole(t *testing.T) {
	if s, ok := codegen.GenericShell("not-a-role"); ok || s != "" {
		t.Errorf("GenericShell(\"not-a-role\") = (%q, %v), want (\"\", false)", s, ok)
	}
}

// TestGenericShell_PluginDriftGuard (@s6) pins plugin/agents/<role>.md to the
// embedded shell byte-for-byte. Test CWD is the package dir, so the plugin dir
// is two levels up (mirrors TestAgentTools_PinnedToSkeletons).
func TestGenericShell_PluginDriftGuard(t *testing.T) {
	for _, role := range codegen.AgentRoles {
		data, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agents", role+".md"))
		if err != nil {
			t.Fatalf("read plugin/agents/%s.md: %v", role, err)
		}
		want, ok := codegen.GenericShell(role)
		if !ok {
			t.Fatalf("GenericShell(%q) unknown", role)
		}
		if string(data) != want {
			t.Errorf("role %s drift: plugin/agents/%s.md != GenericShell(%q) (byte-for-byte)", role, role, role)
		}
	}
}

func head(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}
