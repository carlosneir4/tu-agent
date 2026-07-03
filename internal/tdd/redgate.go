package tdd

import (
	"strings"

	"github.com/tu/tu-agent/internal/testresult"
)

// RedResult is the outcome of the RED check. OK means every new test file is red.
// GreenFiles lists new test files whose cases all passed — green-on-arrival or
// order-violation candidates for the conductor to classify.
type RedResult struct {
	DetResult
	GreenFiles []string
}

// NewTestsRed reports whether every file in newTestFiles is red. A file is red
// when it has a failing/errored case, or when it has no case at all (the build
// failed, so the new test could not run — the expected first red of TDD). A file
// whose cases all pass is green-on-arrival; if any exist, OK is false and they
// are returned in GreenFiles. When the suite passed overall (overallPassed),
// nothing is red and OK is false.
func NewTestsRed(overallPassed bool, rep testresult.Report, newTestFiles []string) RedResult {
	if overallPassed {
		return RedResult{DetResult: DetResult{
			Feedback: "suite is green — no failing test drove the change",
		}}
	}
	var green []string
	for _, f := range newTestFiles {
		cases := casesForFile(rep, f)
		if len(cases) == 0 {
			continue // no report for this file => build failed => red
		}
		allPass := true
		for _, c := range cases {
			if c.Status == testresult.Fail || c.Status == testresult.Error {
				allPass = false
				break
			}
		}
		if allPass {
			green = append(green, f)
		}
	}
	if len(green) > 0 {
		return RedResult{
			DetResult:  DetResult{Feedback: "tests green without production: " + strings.Join(green, ", ")},
			GreenFiles: green,
		}
	}
	return RedResult{DetResult: DetResult{OK: true}}
}

// casesForFile returns the report cases whose class maps into file. A class
// "com.acme.FooTest" maps to any path ending in "com/acme/FooTest" (before the
// extension). Matching is language-agnostic on the class→path suffix, but requires
// a path-segment boundary to avoid false matches (e.g., AFooTest matching FooTest).
func casesForFile(rep testresult.Report, file string) []testresult.Case {
	stem := strings.TrimSuffix(file, extOf(file))
	stem = strings.ReplaceAll(stem, "\\", "/")
	var out []testresult.Case
	for _, c := range rep.Cases {
		classPath := strings.ReplaceAll(c.Class, ".", "/")
		if stem == classPath || strings.HasSuffix(stem, "/"+classPath) {
			out = append(out, c)
		}
	}
	return out
}

// extOf returns the file extension including the dot, or "".
func extOf(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i:]
	}
	return ""
}
