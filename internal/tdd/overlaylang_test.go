package tdd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

// @s6: a supported configured language wins over build-tool detection.
func TestResolveOverlayLang_ConfigWins(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Tdd: config.TddConfig{Language: "java"}}
	if got := resolveOverlayLang(cfg, root); got != "java" {
		t.Fatalf("resolveOverlayLang = %q, want java (config must win over detection)", got)
	}
}

// @s7: an unsupported configured language is dropped, falling back to detection.
func TestResolveOverlayLang_UnsupportedFallsBackToDetection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Tdd: config.TddConfig{Language: "rust"}}
	if got := resolveOverlayLang(cfg, root); got != "go" {
		t.Fatalf("resolveOverlayLang = %q, want go (unsupported config value must be dropped)", got)
	}
}

// @s8: with no configured language, build-tool detection resolves the overlay.
func TestResolveOverlayLang_DetectsBuildTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Tdd: config.TddConfig{Language: ""}}
	if got := resolveOverlayLang(cfg, root); got != "java" {
		t.Fatalf("resolveOverlayLang = %q, want java (pom.xml should detect Java)", got)
	}
}

// @s9: nothing configured and no recognizable build tool resolves to "".
func TestResolveOverlayLang_NothingResolves(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{Tdd: config.TddConfig{Language: ""}}
	if got := resolveOverlayLang(cfg, root); got != "" {
		t.Fatalf("resolveOverlayLang = %q, want empty (never guess an overlay)", got)
	}
}
