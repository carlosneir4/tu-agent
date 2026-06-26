package testgen

import (
	"os"
	"path/filepath"
)

// nearestPackageDir returns the repo-relative directory of the nearest ancestor
// of relPath that contains a package.json, walking up and bounded by repoRoot.
// It returns "." (the repo root) when no package.json is found on the chain,
// reproducing the single-package behavior. Pure except for the filesystem reads
// rooted at repoRoot.
func nearestPackageDir(repoRoot, relPath string) string {
	dir := filepath.Dir(relPath)
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, dir, "package.json")); err == nil {
			return dir
		}
		if dir == "." {
			return "."
		}
		dir = filepath.Dir(dir)
	}
}
