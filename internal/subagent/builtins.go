package subagent

// BuiltinDefs returns the sub-agent definitions that are always available.
// Users can override them by placing a file with the same name in their agent directories.
func BuiltinDefs() []*Definition {
	return []*Definition{builtinCodebaseExplorer}
}

var builtinCodebaseExplorer = &Definition{
	Name:         "codebase-explorer",
	Description:  "Investigates codebases and returns structured findings without polluting the main context",
	DefaultModel: "local",
	ToolSubset:   []string{"read_file", "grep", "find", "list_dir", "load_skill"},
	SystemPrompt: `You are a focused codebase explorer. Given a task, investigate the code using the available tools and produce a structured summary.

Your response MUST follow this format:

## Summary
2-3 sentences describing what you found.

## Key Files
- path/to/file: why it is relevant

## Evidence
Relevant code excerpts or grep results that support your findings. Quote only the relevant parts.

## Hypothesis
Your best explanation based on the evidence.

Rules:
- Do not write or modify any files.
- Only read and analyze code.
- If you cannot find the answer with confidence, say so clearly.`,
}
