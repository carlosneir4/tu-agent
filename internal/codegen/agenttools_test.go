package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTools_Variants(t *testing.T) {
	capture, ok := AgentTools("developer")
	if !ok || !strings.Contains(capture, "mem_save") || !strings.Contains(capture, "Write") {
		t.Fatalf("developer = (%q,%v), want capture variant with Write+mem_save", capture, ok)
	}
	if !strings.Contains(capture, "Edit") || !strings.Contains(capture, "Bash") || !strings.Contains(capture, "Skill") {
		t.Errorf("developer = %q, want Edit, Bash, Skill present", capture)
	}
	// qa has Write+mem_save but does not carry Edit/Bash/Skill (tdd-dispatched only).
	qaTools, qaOK := AgentTools("qa")
	if !qaOK || !strings.Contains(qaTools, "Write") || !strings.Contains(qaTools, "mem_save") {
		t.Errorf("qa = (%q,%v), want Write+mem_save", qaTools, qaOK)
	}
	arch, ok := AgentTools("architect")
	if !ok || !strings.Contains(arch, "Write") || !strings.Contains(arch, "mem_save") {
		t.Errorf("architect = %q, want Write and mem_save present", arch)
	}
	if !strings.Contains(arch, "Bash") || !strings.Contains(arch, "Skill") {
		t.Errorf("architect = %q, want Bash, Skill present", arch)
	}
	rev, ok := AgentTools("pr-reviewer")
	if !ok || !strings.Contains(rev, "Write") || strings.Contains(rev, "mem_save") {
		t.Errorf("pr-reviewer = %q, want Write present but no mem_save", rev)
	}
	if !strings.Contains(rev, "Bash") || !strings.Contains(rev, "Skill") {
		t.Errorf("pr-reviewer = %q, want Bash, Skill present", rev)
	}
	// security-reviewer is recall-only and has no Write/Bash/Skill (not tdd-dispatched).
	sec, secOK := AgentTools("security-reviewer")
	if !secOK || strings.Contains(sec, "mem_save") || strings.Contains(sec, "Write") {
		t.Errorf("security-reviewer = (%q,%v), want no mem_save and no Write", sec, secOK)
	}
	if _, ok := AgentTools("unknown"); ok {
		t.Error("unknown role must return ok=false")
	}
}

// TestAgentTools_PinnedToSkeletons fails if the Go map drifts from the plugin
// skeletons. Test CWD is the package dir, so the plugin dir is two levels up.
func TestAgentTools_PinnedToSkeletons(t *testing.T) {
	for _, role := range []string{"developer", "qa", "architect", "pr-reviewer", "security-reviewer"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "plugin", "agent-templates", role+".md"))
		if err != nil {
			t.Fatalf("read skeleton %s: %v", role, err)
		}
		var skeletonTools string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "tools:") {
				skeletonTools = strings.TrimSpace(strings.TrimPrefix(line, "tools:"))
				break
			}
		}
		want, ok := AgentTools(role)
		if !ok {
			t.Fatalf("AgentTools(%q) unknown", role)
		}
		if skeletonTools != want {
			t.Errorf("role %s drift:\n skeleton: %q\n AgentTools: %q", role, skeletonTools, want)
		}
	}
}

func TestReplaceFrontmatterTools(t *testing.T) {
	agent := "---\nname: x-developer\ndescription: \"d\"\ntools: Read, Grep\n---\nbody\n\n## Project Context\n- enriched bullet\n"
	out, changed, had := ReplaceFrontmatterTools(agent, "Read, Write, Grep, Glob")
	if !changed || !had {
		t.Fatalf("want changed+had, got changed=%v had=%v", changed, had)
	}
	if !strings.Contains(out, "tools: Read, Write, Grep, Glob\n") {
		t.Errorf("tools line not replaced: %q", out)
	}
	if !strings.Contains(out, "## Project Context\n- enriched bullet\n") {
		t.Errorf("enriched body not preserved: %q", out)
	}
	if !strings.Contains(out, "name: x-developer\n") {
		t.Errorf("other frontmatter not preserved")
	}
	// Idempotent: replacing with the same value reports unchanged.
	out2, changed2, _ := ReplaceFrontmatterTools(out, "Read, Write, Grep, Glob")
	if changed2 || out2 != out {
		t.Errorf("second replace must be a no-op (unchanged)")
	}
	// No tools line → hadToolsLine false, content untouched.
	noTools := "---\nname: y\n---\nbody\n"
	out3, changed3, had3 := ReplaceFrontmatterTools(noTools, "Read")
	if changed3 || had3 || out3 != noTools {
		t.Errorf("no tools line must yield (content,false,false), got changed=%v had=%v", changed3, had3)
	}
	// No frontmatter → untouched.
	noFront := "body without frontmatter\ntools: x\n"
	if _, _, had4 := ReplaceFrontmatterTools(noFront, "Read"); had4 {
		t.Errorf("a tools: line outside frontmatter must not match")
	}
}

func TestGraphAgentTools(t *testing.T) {
	got := GraphAgentTools()
	if len(got) != 6 || got[0] != "mcp__tu-agent-graph__get_context" || got[5] != "mcp__tu-agent-graph__mem_recent" {
		t.Errorf("unexpected GraphAgentTools: %v", got)
	}
}

func TestFrontmatterToolsIsInline(t *testing.T) {
	// inline scalar form → supported
	inline := "---\nname: x\ntools: Read, Write\n---\nbody"
	if isInline, had := FrontmatterToolsIsInline(inline); !isInline || !had {
		t.Errorf("inline: want (true,true), got (%v,%v)", isInline, had)
	}

	// no tools: line → still inline (union will create one)
	noTools := "---\nname: x\n---\nbody"
	if isInline, had := FrontmatterToolsIsInline(noTools); !isInline || !had {
		t.Errorf("no tools line: want (true,true), got (%v,%v)", isInline, had)
	}

	// JSON array form → unsupported
	jsonArr := "---\nname: x\ntools: [\"Write\", \"Read\"]\n---\nbody"
	if isInline, had := FrontmatterToolsIsInline(jsonArr); isInline || !had {
		t.Errorf("json array: want (false,true), got (%v,%v)", isInline, had)
	}

	// YAML block-list form → unsupported
	blockList := "---\nname: x\ntools:\n  - Read\n  - Write\n---\nbody"
	if isInline, had := FrontmatterToolsIsInline(blockList); isInline || !had {
		t.Errorf("block list: want (false,true), got (%v,%v)", isInline, had)
	}

	// no frontmatter → (false, false)
	noFront := "body without frontmatter\ntools: x\n"
	if isInline, had := FrontmatterToolsIsInline(noFront); isInline || had {
		t.Errorf("no frontmatter: want (false,false), got (%v,%v)", isInline, had)
	}
}

func TestUnionFrontmatterTools(t *testing.T) {
	add := []string{"mcp__tu-agent-graph__get_context", "mcp__tu-agent-graph__mem_save"}

	// existing tools line: union appends missing, preserving order
	in := "---\nname: x\ntools: Read, Write, Bash\n---\nbody"
	out, changed, had := UnionFrontmatterTools(in, add)
	if !changed || !had {
		t.Fatalf("expected changed+had, got changed=%v had=%v", changed, had)
	}
	if !strings.Contains(out, "tools: Read, Write, Bash, mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__mem_save") {
		t.Errorf("union wrong:\n%s", out)
	}

	// idempotent: a second run makes no change
	out2, changed2, _ := UnionFrontmatterTools(out, add)
	if changed2 || out2 != out {
		t.Errorf("expected idempotent no-op, changed=%v", changed2)
	}

	// frontmatter but no tools line: one is created
	in2 := "---\nname: y\n---\nbody"
	out3, changed3, had3 := UnionFrontmatterTools(in2, add)
	if !changed3 || !had3 {
		t.Fatalf("expected created tools line, changed=%v had=%v", changed3, had3)
	}
	if !strings.Contains(out3, "tools: mcp__tu-agent-graph__get_context, mcp__tu-agent-graph__mem_save") {
		t.Errorf("created line wrong:\n%s", out3)
	}

	// no frontmatter: had=false, unchanged
	in3 := "no frontmatter here"
	out4, changed4, had4 := UnionFrontmatterTools(in3, add)
	if changed4 || had4 || out4 != in3 {
		t.Errorf("expected had=false unchanged for no-frontmatter, got changed=%v had=%v", changed4, had4)
	}
}
