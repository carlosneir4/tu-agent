package mutation

import (
	"os/exec"
	"regexp"
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
	// mutmut splits running from reporting: run the mutations, then list every
	// mutant's status on stdout. One sh -c keeps the single-command Run model
	// (sh -c is already used by the tdd test runner). Reads stdout, not a file.
	return []string{"sh", "-c", "mutmut run; mutmut results --all true"}
}

func (pythonEngine) ReportPath(_, _ string) string { return "" }

// mutmutResultRe matches a mutmut 3.x `mutmut results --all` line, e.g.
//
//	"    calc.x_is_positive__mutmut_1: survived"
//
// group 1 = full mutant name, group 2 = status. Progress/spinner/summary lines
// (e.g. "140.52 mutations/second") do not match and are ignored.
var mutmutResultRe = regexp.MustCompile(`^\s+(\S+__mutmut_\d+):\s+(\w+)\s*$`)

func (pythonEngine) Parse(output string) (Report, error) {
	var rep Report
	for _, raw := range strings.Split(output, "\n") {
		m := mutmutResultRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		rep.Total++
		if m[2] == "killed" {
			rep.Killed++
			continue
		}
		// Anything not killed (survived, no_tests, timeout, suspicious, …) is a
		// gap — conservative and robust to unknown statuses.
		rep.Survived++
		rep.Survivors = append(rep.Survivors, Survivor{File: mutmutModule(m[1]), Desc: m[1]})
	}
	if rep.Total > 0 {
		rep.Score = float64(rep.Killed) / float64(rep.Total)
	}
	return rep, nil
}

// mutmutModule returns the module prefix of a mutant name, dropping the trailing
// `.x_<func>__mutmut_<n>` segment: "pkg.sub.x_f__mutmut_2" -> "pkg.sub",
// "calc.x_add__mutmut_1" -> "calc". A name with no dot is returned unchanged.
func mutmutModule(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}
