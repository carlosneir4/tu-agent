package mutation

import (
	"strings"
	"testing"
)

// Real captured stdout of `mutmut run; mutmut results --all true` (mutmut 3.x):
// run progress noise followed by one line per mutant.
const mutmutFixture = `    done in 109ms (1 files mutated, 0 ignored, 0 unmodified)
140.52 mutations/second
    calc.x_add__mutmut_1: killed
    calc.x_is_positive__mutmut_1: survived
    calc.x_is_positive__mutmut_2: survived
`

func TestPythonEngineParse(t *testing.T) {
	rep, err := pythonEngine{}.Parse(mutmutFixture)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 3 || rep.Killed != 1 || rep.Survived != 2 {
		t.Fatalf("counts = %+v, want total3 killed1 survived2", rep)
	}
	if rep.Score < 0.33 || rep.Score > 0.34 {
		t.Errorf("score = %v, want ~0.333", rep.Score)
	}
	if len(rep.Survivors) != 2 {
		t.Fatalf("survivors = %+v, want 2", rep.Survivors)
	}
	if rep.Survivors[0].File != "calc" || rep.Survivors[0].Desc != "calc.x_is_positive__mutmut_1" {
		t.Fatalf("survivor[0] = %+v, want File=calc Desc=calc.x_is_positive__mutmut_1", rep.Survivors[0])
	}
	if rep.Survivors[1].File != "calc" || rep.Survivors[1].Desc != "calc.x_is_positive__mutmut_2" {
		t.Fatalf("survivor[1] = %+v, want File=calc Desc=calc.x_is_positive__mutmut_2", rep.Survivors[1])
	}
}

func TestPythonEngineParse_empty(t *testing.T) {
	rep, err := pythonEngine{}.Parse("no mutant lines here\n")
	if err != nil || rep.Total != 0 {
		t.Fatalf("empty parse = %+v, %v; want zero Total and no error", rep, err)
	}
}

func TestPythonEngineCommandRunsThenReports(t *testing.T) {
	argv := pythonEngine{}.Command("", "")
	if len(argv) == 0 || argv[0] != "sh" {
		t.Fatalf("Command = %v, want it to shell out via sh -c", argv)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "mutmut run") {
		t.Errorf("Command must RUN mutations: %q", joined)
	}
	if !strings.Contains(joined, "mutmut results --all true") {
		t.Errorf("Command must report all statuses: %q", joined)
	}
}
