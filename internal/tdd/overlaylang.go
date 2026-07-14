package tdd

import (
	"path/filepath"
	"strings"

	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/config"
)

// resolveOverlayLang resolves the language whose LANG-OVERLAY the dev-flow stage
// prompt should inject. A supported configured language wins; otherwise a light
// build-tool detection at root decides. Returns "" when nothing resolves — the
// overlay is never guessed.
func resolveOverlayLang(cfg config.Config, root string) string {
	lang := strings.TrimSpace(cfg.Tdd.Language)
	for _, l := range codegen.SupportedLanguages {
		if lang == l {
			return lang
		}
	}
	return buildToolToLang(codegen.DetectBuildTool(root))
}

// buildToolToLang maps a build tool (see codegen.DetectBuildTool) to the
// dev-flow overlay language, or "" when the tool has no overlay.
func buildToolToLang(tool string) string {
	switch tool {
	case "gradle", "maven":
		return "java"
	case "npm", "yarn", "pnpm", "bun":
		return "typescript"
	case "pyproject", "pip":
		return "python"
	case "go":
		return "go"
	default:
		return ""
	}
}

// resolveOverlayLangForRoot resolves the dev-flow overlay language for a repo
// root, reading that root's project tdd.language first (config-supported-wins →
// build-tool detection at root → ""). A load error degrades to an empty config.
//
// Loader.Load reads only the userDir and projectDir layers; both are pointed at
// root/.tu-agent (claudeDir is unused) so resolution stays hermetic to the given
// root — an empty userDir would make Load read a cwd-relative config.yaml,
// leaking the process working directory into a root-scoped query.
func resolveOverlayLangForRoot(root string) string {
	projectDir := filepath.Join(root, ".tu-agent")
	loader := config.NewLoader("", projectDir, projectDir)
	cfg, err := loader.Load()
	if err != nil {
		cfg = config.Config{}
	}
	return resolveOverlayLang(cfg, root)
}
