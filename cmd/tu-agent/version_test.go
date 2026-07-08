package main

import (
	"regexp"
	"testing"
)

// semverPrefix matches a leading MAJOR.MINOR.PATCH, the shape the plugin
// preflight (plugin/skills/prepare/SKILL.md) and the shim's installed_version()
// grep from `tu-agent version`. The dev default must satisfy it so a
// non-release build is still parseable.
var semverPrefix = regexp.MustCompile(`^\d+\.\d+\.\d+`)

func TestVersionDefaultIsParseable(t *testing.T) {
	if !semverPrefix.MatchString(version) {
		t.Errorf("version default %q must start with MAJOR.MINOR.PATCH so preflight/shim can parse it", version)
	}
}

// version is a var (not a const) so release builds can override it via
// -ldflags "-X main.version=<tag>". This assignment compiling proves it stays
// assignable; if someone reverts it to a const, this file fails to build.
func TestVersionIsOverridable(t *testing.T) {
	orig := version
	defer func() { version = orig }()
	version = "v9.9.9"
	if version != "v9.9.9" {
		t.Fatalf("version not overridable")
	}
}
