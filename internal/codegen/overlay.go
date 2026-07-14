package codegen

import (
	"embed"
	"path"
)

//go:embed overlays
var overlayFS embed.FS

// LangOverlay returns the language-specific guidance block for a role, injected
// verbatim at runtime into the composed dev-flow stage prompt. It returns "" when
// the (lang, role) pair has no overlay — unsupported languages, process-only roles
// (analyst, scribe), and unknown roles all yield "" rather than panicking.
func LangOverlay(lang, role string) string {
	data, err := overlayFS.ReadFile(path.Join("overlays", lang, role+".md"))
	if err != nil {
		return ""
	}
	return string(data)
}
