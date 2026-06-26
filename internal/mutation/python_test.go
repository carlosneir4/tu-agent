package mutation

import "testing"

const mutmutFixture = `Killed 🎉 (4)

Survived 🙁 (2)

---- src/order.py (2) ----
3, 4

Timeout ⏰ (0)

Suspicious 🤔 (0)
`

func TestPythonEngineParse(t *testing.T) {
	rep, err := pythonEngine{}.Parse(mutmutFixture)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Killed != 4 || rep.Survived != 2 || rep.Total != 6 {
		t.Fatalf("counts = %+v, want killed4 survived2 total6", rep)
	}
	if rep.Score < 0.66 || rep.Score > 0.67 {
		t.Errorf("score = %v, want ~0.667", rep.Score)
	}
	if len(rep.Survivors) != 1 || rep.Survivors[0].File != "src/order.py" {
		t.Fatalf("survivor = %+v", rep.Survivors)
	}
}
