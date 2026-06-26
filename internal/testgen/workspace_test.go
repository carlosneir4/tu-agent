package testgen

import "testing"

func TestNearestPackageDir(t *testing.T) {
	root := "testdata/ws"
	cases := []struct {
		name, relPath, want string
	}{
		{"deep file in app", "packages/app/src/widget.ts", "packages/app"},
		{"file in web", "packages/web/src/card.ts", "packages/web"},
		{"file at repo root", "main.ts", "."},
		{"no enclosing package returns root", "packages/app/src/deep/nested/x.ts", "packages/app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nearestPackageDir(root, c.relPath); got != c.want {
				t.Errorf("nearestPackageDir(%q,%q) = %q, want %q", root, c.relPath, got, c.want)
			}
		})
	}
}
