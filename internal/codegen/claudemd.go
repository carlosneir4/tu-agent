package codegen

import (
	"bytes"
	"fmt"
	"path"
	"text/template"
)

// RenderCLAUDEMDTemplate produces a CLAUDE.md from the static template, used
// for --no-llm and empty/new repos where there is nothing to analyze.
func RenderCLAUDEMDTemplate(data AgentTemplateData) (string, error) {
	raw, err := templateFS.ReadFile(path.Join("templates", "CLAUDE.md.tmpl"))
	if err != nil {
		return "", fmt.Errorf("codegen.RenderCLAUDEMDTemplate: read template: %w", err)
	}
	t, err := template.New("claudemd").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("codegen.RenderCLAUDEMDTemplate: parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("codegen.RenderCLAUDEMDTemplate: execute: %w", err)
	}
	return buf.String(), nil
}
