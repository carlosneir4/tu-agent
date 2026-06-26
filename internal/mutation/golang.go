package mutation

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type goEngine struct{}

func (goEngine) Name() string { return "go-mutesting" }

func (goEngine) Available(string, string) bool {
	_, err := exec.LookPath("go-mutesting")
	return err == nil
}

func (goEngine) WorkDir(repoRoot, _ string) string { return repoRoot }

func (goEngine) Command(_, pkgDir string) []string {
	return []string{"go-mutesting", "./" + pkgDir}
}

func (goEngine) ReportPath(_, _ string) string { return "" }

// passLineRe matches a surviving-mutant line: PASS "<file>.<n>" with checksum.
var passLineRe = regexp.MustCompile(`^PASS\s+"?([^"]+?)\.\d+"?\s+with checksum`)

// summaryRe matches go-mutesting's final summary; passed=survived, failed=killed.
var summaryRe = regexp.MustCompile(`\((\d+) passed, (\d+) failed,.*total is (\d+)\)`)

func (goEngine) Parse(output string) (Report, error) {
	var rep Report
	for _, ln := range strings.Split(output, "\n") {
		if m := passLineRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			rep.Survivors = append(rep.Survivors, Survivor{File: m[1], Desc: "surviving mutant"})
		}
	}
	m := summaryRe.FindStringSubmatch(output)
	if m == nil {
		return Report{}, fmt.Errorf("goEngine.Parse: no go-mutesting summary line found")
	}
	survived, _ := strconv.Atoi(m[1])
	killed, _ := strconv.Atoi(m[2])
	total, _ := strconv.Atoi(m[3])
	rep.Survived, rep.Killed, rep.Total = survived, killed, total
	if total > 0 {
		rep.Score = float64(killed) / float64(total)
	}
	return rep, nil
}
