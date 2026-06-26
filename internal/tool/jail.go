package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ConfinedPath resolves userPath to an absolute path and verifies it is within root.
// If root is empty, userPath is returned as-is (no confinement).
// Returns an error if the resolved path escapes root after filepath.Clean.
func ConfinedPath(root, userPath string) (string, error) {
	if root == "" {
		return userPath, nil
	}
	abs, err := filepath.Abs(userPath)
	if err != nil {
		return "", fmt.Errorf("jail: resolving path %q: %w", userPath, err)
	}
	rootClean := filepath.Clean(root)
	if abs != rootClean && !strings.HasPrefix(abs, rootClean+string(filepath.Separator)) {
		return "", fmt.Errorf("jail: path %q is outside the allowed root %q", userPath, root)
	}
	return abs, nil
}
