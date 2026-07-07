package codegen

import (
	"os"
	"path/filepath"
	"sort"
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
	// qa has Write+mem_save+Bash (Verify step runs tests) but does not carry Edit/Skill (tdd-dispatched only).
	qaTools, qaOK := AgentTools("qa")
	if !qaOK || !strings.Contains(qaTools, "Write") || !strings.Contains(qaTools, "mem_save") {
		t.Errorf("qa = (%q,%v), want Write+mem_save", qaTools, qaOK)
	}
	if !strings.Contains(qaTools, "Bash") {
		t.Errorf("qa = %q, want Bash present (Verify step must be able to run tests)", qaTools)
	}
	if strings.Contains(qaTools, "Edit") || strings.Contains(qaTools, "Skill") {
		t.Errorf("qa = %q, want no Edit/Skill (tdd-dispatched only)", qaTools)
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

// claudeToSubset maps a Claude Code frontmatter `tools:` token to its
// tool_subset equivalent, so surface (a)/(b) tokens normalize onto the same
// vocabulary as surface (c)'s already-bare tool_subset entries.
// mcp__tu-agent-graph__X tokens normalize to X via prefix-stripping, handled
// separately in normalizeClaudeTools.
var claudeToSubset = map[string]string{
	"Read":  "read_file",
	"Grep":  "grep",
	"Glob":  "find",
	"Bash":  "bash",
	"Write": "write_file",
	"Skill": "load_skill",
}

// roleSurfaceExtras lists, per role, tokens legitimately present on only one
// of the three surfaces today — pre-existing asymmetries that are out of
// scope for the tool-matrix task (not drift to fix now) and are excluded
// before the equality comparison in TestToolMatrix_ThreeSurfaces:
//   - developer: (a)/(b) grant Edit that (c) has no equivalent for; (c) grants
//     list_dir/dispatch_agent that (a)/(b) have no equivalent for (tdd-dispatch
//     specific tools).
//   - qa / security-reviewer: (c) grants load_skill; (a)/(b) never granted
//     Skill to these roles.
//   - pr-reviewer: (a)/(b) keep Bash/Write for review artifacts and scoped
//     test runs (scoped by prose); (c)'s tool_subset never carried bash or
//     write_file for this role.
//   - architect / scribe: untouched by this task (see task-13 brief file
//     list); (b) already carries Bash/Write/graph tools that (c) has never
//     had for these two specifically.
var roleSurfaceExtras = map[string][]string{
	"developer":         {"Edit", "list_dir", "dispatch_agent"},
	"qa":                {"load_skill"},
	"pr-reviewer":       {"bash", "write_file"},
	"security-reviewer": {"load_skill"},
	"architect":         {"bash", "write_file", "get_context", "get_impact", "find_symbol"},
	"scribe":            {"load_skill", "get_concept"},
}

// normalizeClaudeTools parses a Claude Code `tools:` frontmatter value into a
// normalized capability set comparable to a tool_subset set.
func normalizeClaudeTools(toolsLine string) map[string]bool {
	set := map[string]bool{}
	for _, raw := range strings.Split(toolsLine, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "mcp__tu-agent-graph__") {
			tok = strings.TrimPrefix(tok, "mcp__tu-agent-graph__")
		} else if mapped, ok := claudeToSubset[tok]; ok {
			tok = mapped
		}
		set[tok] = true
	}
	return set
}

// parseToolSubset reads the `tool_subset:` YAML list from a codegen agent
// template's frontmatter into a normalized capability set. Entries are
// already bare (read_file, bash, get_context, ...), matching normalizeClaudeTools'
// output vocabulary.
func parseToolSubset(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	set := map[string]bool{}
	inSubset := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "tool_subset:" {
			inSubset = true
			continue
		}
		if !inSubset {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			set[strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))] = true
			continue
		}
		break // first non "- " line after tool_subset: ends the block
	}
	return set
}

// diffSets reports whether a and b are equal, and — when not — the
// (sorted) elements present in only one side, for a readable failure message.
func diffSets(a, b map[string]bool) (equal bool, onlyA, onlyB []string) {
	for k := range a {
		if !b[k] {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if !a[k] {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return len(onlyA) == 0 && len(onlyB) == 0, onlyA, onlyB
}

// TestToolMatrix_ThreeSurfaces is the drift pin across all three agent-tool
// surfaces: (a) the AgentTools Go map, (b) plugin/agent-templates/<role>.md
// `tools:` frontmatter, and (c) internal/codegen/templates/base/<role>.md
// `tool_subset:` frontmatter. (a) only defines 5 of the 7 roles; analyst and
// scribe are compared (b)-vs-(c) only. Known, accepted, out-of-scope
// asymmetries are excluded per roleSurfaceExtras — see its doc comment.
func TestToolMatrix_ThreeSurfaces(t *testing.T) {
	roles := []string{"developer", "qa", "architect", "pr-reviewer", "security-reviewer", "analyst", "scribe"}
	hasSurfaceA := map[string]bool{
		"developer": true, "qa": true, "architect": true,
		"pr-reviewer": true, "security-reviewer": true,
	}

	for _, role := range roles {
		extras := roleSurfaceExtras[role]

		pluginPath := filepath.Join("..", "..", "plugin", "agent-templates", role+".md")
		pluginData, err := os.ReadFile(pluginPath)
		if err != nil {
			t.Fatalf("%s: read plugin skeleton: %v", role, err)
		}
		var toolsLine string
		for _, line := range strings.Split(string(pluginData), "\n") {
			if strings.HasPrefix(line, "tools:") {
				toolsLine = strings.TrimSpace(strings.TrimPrefix(line, "tools:"))
				break
			}
		}
		setB := normalizeClaudeTools(toolsLine)

		subsetPath := filepath.Join("templates", "base", role+".md")
		setC := parseToolSubset(t, subsetPath)

		for _, e := range extras {
			delete(setB, e)
			delete(setC, e)
		}

		if hasSurfaceA[role] {
			want, ok := AgentTools(role)
			if !ok {
				t.Fatalf("AgentTools(%q) unknown", role)
			}
			setA := normalizeClaudeTools(want)
			for _, e := range extras {
				delete(setA, e)
			}
			if eq, onlyA, onlyC := diffSets(setA, setC); !eq {
				t.Errorf("role %s: (a) AgentTools vs (c) tool_subset drift — only in (a): %v, only in (c): %v", role, onlyA, onlyC)
			}
			if eq, onlyA, onlyB := diffSets(setA, setB); !eq {
				t.Errorf("role %s: (a) AgentTools vs (b) plugin drift — only in (a): %v, only in (b): %v", role, onlyA, onlyB)
			}
		}
		if eq, onlyB, onlyC := diffSets(setB, setC); !eq {
			t.Errorf("role %s: (b) plugin vs (c) tool_subset drift — only in (b): %v, only in (c): %v", role, onlyB, onlyC)
		}
	}
}
