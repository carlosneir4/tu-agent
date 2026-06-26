package coverage

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Supported coverage source languages.
const (
	LangGo     = "go"
	LangJava   = "java"
	LangPython = "python"
	LangTS     = "typescript"
)

// DetectFormat sniffs a coverage report file and returns the language whose
// parser handles it.
func DetectFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("coverage.DetectFormat: %w", err)
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("coverage.DetectFormat: %w", err)
	}
	head := strings.TrimSpace(string(buf[:n]))
	switch {
	case strings.HasPrefix(head, "mode:"):
		return LangGo, nil
	case strings.Contains(head, "<report") || strings.Contains(head, "JACOCO"):
		return LangJava, nil
	case strings.Contains(head, "<coverage") || strings.Contains(head, "<!DOCTYPE coverage"):
		return LangPython, nil
	case strings.HasPrefix(head, "{"):
		return LangTS, nil
	default:
		return "", fmt.Errorf("coverage.DetectFormat: unrecognized coverage format in %s", path)
	}
}

// LoadAuto sniffs the report format and parses it. modulePath (go.mod module)
// strips the Go import-path prefix; repoRoot makes Istanbul absolute paths
// repo-relative. Both may be "".
func LoadAuto(path, modulePath, repoRoot string) (Profile, error) {
	lang, err := DetectFormat(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("coverage.LoadAuto: %w", err)
	}
	defer f.Close()
	return parseFor(lang, f, modulePath, repoRoot)
}

// parseFor dispatches an open reader to the language's parser. Shared by
// LoadAuto and Generate.
func parseFor(lang string, r io.Reader, modulePath, repoRoot string) (Profile, error) {
	switch lang {
	case LangGo:
		return ParseGoProfile(r, modulePath)
	case LangJava:
		return ParseJaCoCo(r)
	case LangPython:
		return ParseCobertura(r)
	case LangTS:
		return ParseIstanbul(r, repoRoot)
	default:
		return nil, fmt.Errorf("coverage.parseFor: no parser for %q", lang)
	}
}
