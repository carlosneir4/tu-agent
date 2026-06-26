package mutation

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type pythonEngine struct{}

func (pythonEngine) Name() string { return "mutmut" }

func (pythonEngine) Available(string, string) bool {
	_, err := exec.LookPath("mutmut")
	return err == nil
}

func (pythonEngine) WorkDir(repoRoot, _ string) string { return repoRoot }

func (pythonEngine) Command(_, _ string) []string {
	return []string{"mutmut", "results"}
}

func (pythonEngine) ReportPath(_, _ string) string { return "" }

// categoryRe matches a mutmut category header: "Survived 🙁 (2)" → name, count.
var categoryRe = regexp.MustCompile(`^(\w+)\s+\S+\s+\((\d+)\)`)

// fileSectionRe matches a survivor file section: "---- src/foo.py (2) ----".
var fileSectionRe = regexp.MustCompile(`^----\s+(.+?)\s+\(\d+\)\s+----`)

func (pythonEngine) Parse(output string) (Report, error) {
	var rep Report
	for _, raw := range strings.Split(output, "\n") {
		ln := strings.TrimSpace(raw)
		if m := categoryRe.FindStringSubmatch(ln); m != nil {
			n, _ := strconv.Atoi(m[2])
			rep.Total += n
			switch m[1] {
			case "Killed":
				rep.Killed = n
			case "Survived":
				rep.Survived = n
			}
			continue
		}
		if m := fileSectionRe.FindStringSubmatch(ln); m != nil {
			rep.Survivors = append(rep.Survivors, Survivor{File: m[1], Desc: "surviving mutant(s)"})
		}
	}
	if rep.Total > 0 {
		rep.Score = float64(rep.Killed) / float64(rep.Total)
	}
	return rep, nil
}
