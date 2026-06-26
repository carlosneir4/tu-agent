package codegen

import "testing"

func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain content unchanged", "---\nname: x\n---\nbody", "---\nname: x\n---\nbody"},
		{"strips wrapping fence", "```\n---\nname: x\n---\n# X\n```", "---\nname: x\n---\n# X"},
		{"strips fence with language", "```markdown\n---\nname: x\n---\nbody\n```", "---\nname: x\n---\nbody"},
		{"fence-only collapses to empty", "```", ""},
		{"open fence no close", "```\n---\nname: x\n---", "---\nname: x\n---"},
		{"surrounding whitespace trimmed", "  \n```\n---\nname: x\n---\n```\n  ", "---\nname: x\n---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripCodeFence(tt.in); got != tt.want {
				t.Errorf("stripCodeFence(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
