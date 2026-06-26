package mutation

import "testing"

const goMutestingFixture = `PASS "internal/store/store.go.0" with checksum 9f...
--- Original
+++ New
PASS "internal/store/store.go.1" with checksum a1...
FAIL "internal/store/store.go.2" with checksum b2...
The mutation score is 0.333333 (1 passed, 2 failed, 0 duplicated, 0 skipped, total is 3)
`

func TestGoEngineParse(t *testing.T) {
	rep, err := goEngine{}.Parse(goMutestingFixture)
	if err != nil {
		t.Fatal(err)
	}
	// go-mutesting: passed=survived(1), failed=killed(2), total=3
	if rep.Total != 3 || rep.Killed != 2 || rep.Survived != 1 {
		t.Fatalf("counts = %+v, want total3 killed2 survived1", rep)
	}
	if rep.Score < 0.66 || rep.Score > 0.67 {
		t.Errorf("score = %v, want ~0.667 (killed/total)", rep.Score)
	}
	if len(rep.Survivors) != 2 {
		t.Fatalf("survivors = %d, want 2 (the PASS lines)", len(rep.Survivors))
	}
	if rep.Survivors[0].File != "internal/store/store.go" {
		t.Errorf("survivor file = %q", rep.Survivors[0].File)
	}
}

func TestGoEngineCommand(t *testing.T) {
	got := goEngine{}.Command("/repo", "internal/store")
	want := []string{"go-mutesting", "./internal/store"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Command = %v, want %v", got, want)
	}
}
