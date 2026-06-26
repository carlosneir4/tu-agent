package mutation

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type javaEngine struct{}

func (javaEngine) Name() string { return "pitest" }

func (javaEngine) Available(repoRoot, _ string) bool {
	for _, f := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(repoRoot, f)); err == nil {
			return true
		}
	}
	return false
}

func (javaEngine) WorkDir(repoRoot, _ string) string { return repoRoot }

// javaBuildTool reports "gradle" when a Gradle build file is present, else
// "maven". Gradle is checked first because a repo may carry both.
func javaBuildTool(repoRoot string) string {
	for _, f := range []string{"build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(repoRoot, f)); err == nil {
			return "gradle"
		}
	}
	return "maven"
}

// javaBin prefers the repo's wrapper script (./mvnw, ./gradlew) over the bare
// tool on PATH.
func javaBin(repoRoot, wrapper, bare string) string {
	if _, err := os.Stat(filepath.Join(repoRoot, wrapper)); err == nil {
		return "./" + wrapper
	}
	return bare
}

// javaModuleDir walks up from repoRoot/pkgDir to the nearest directory holding
// a build file (build.gradle/.kts or pom.xml), bounded by repoRoot. Returns the
// repo-relative module dir, or "." for a single-module repo / when none is
// found. pkgDir is the source-file dir the caller passes (filepath.Dir(path)).
func javaModuleDir(repoRoot, pkgDir string) string {
	if filepath.IsAbs(pkgDir) {
		// Callers must pass a repo-relative dir; degrade gracefully rather than loop.
		return "."
	}
	dir := pkgDir
	if dir == "" {
		dir = "."
	}
	for {
		for _, f := range []string{"build.gradle", "build.gradle.kts", "pom.xml"} {
			if _, err := os.Stat(filepath.Join(repoRoot, dir, f)); err == nil {
				return dir
			}
		}
		if dir == "." {
			return "."
		}
		dir = filepath.Dir(dir)
	}
}

// gradleTask maps a repo-relative module dir to its Gradle pitest task path:
// "." -> "pitest", "core" -> ":core:pitest", "a/b" -> ":a:b:pitest".
func gradleTask(moduleDir string) string {
	if moduleDir == "." {
		return "pitest"
	}
	return ":" + strings.ReplaceAll(moduleDir, "/", ":") + ":pitest"
}

func (javaEngine) Command(repoRoot, pkgDir string) []string {
	mod := javaModuleDir(repoRoot, pkgDir)
	if javaBuildTool(repoRoot) == "gradle" {
		return []string{javaBin(repoRoot, "gradlew", "gradle"), gradleTask(mod)}
	}
	argv := []string{javaBin(repoRoot, "mvnw", "mvn"), "-q"}
	if mod != "." {
		argv = append(argv, "-pl", mod)
	}
	return append(argv, "org.pitest:pitest-maven:mutationCoverage", "-DoutputFormats=XML", "-DtimestampedReports=false")
}

func (javaEngine) ReportPath(repoRoot, pkgDir string) string {
	mod := javaModuleDir(repoRoot, pkgDir)
	if javaBuildTool(repoRoot) == "gradle" {
		return filepath.Join(repoRoot, mod, "build", "reports", "pitest", "mutations.xml")
	}
	return filepath.Join(repoRoot, mod, "target", "pit-reports", "mutations.xml")
}

type pitMutations struct {
	Mutations []pitMutation `xml:"mutation"`
}

type pitMutation struct {
	Detected   bool   `xml:"detected,attr"`
	Status     string `xml:"status,attr"`
	SourceFile string `xml:"sourceFile"`
	LineNumber int    `xml:"lineNumber"`
	Mutator    string `xml:"mutator"`
}

func (javaEngine) Parse(output string) (Report, error) {
	var doc pitMutations
	dec := xml.NewDecoder(stringReader(output))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return Report{}, fmt.Errorf("javaEngine.Parse: %w", err)
	}
	var rep Report
	for _, m := range doc.Mutations {
		rep.Total++
		if m.Detected {
			rep.Killed++
		} else {
			rep.Survived++
			rep.Survivors = append(rep.Survivors, Survivor{File: m.SourceFile, Line: m.LineNumber, Desc: m.Mutator})
		}
	}
	if rep.Total > 0 {
		rep.Score = float64(rep.Killed) / float64(rep.Total)
	}
	return rep, nil
}
