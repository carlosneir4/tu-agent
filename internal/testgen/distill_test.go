package testgen

import (
	"fmt"
	"strings"
	"testing"
)

func TestDistillFailure(t *testing.T) {
	goOut := `=== RUN   TestStoreSave
some chatty log line
    store_gen_test.go:14: Save() = nil, want error
--- FAIL: TestStoreSave (0.00s)
FAIL
FAIL	example.com/store	0.012s
./store_gen_test.go:9:2: undefined: NewStore
`
	got := DistillFailure(goOut, 4096)
	for _, want := range []string{"--- FAIL: TestStoreSave", "undefined: NewStore"} {
		if !strings.Contains(got, want) {
			t.Errorf("go distill missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "chatty log line") {
		t.Errorf("go distill kept noise:\n%s", got)
	}

	mvnOut := `[INFO] Scanning for projects...
[INFO] Building demo 1.0
[ERROR] /src/test/java/com/acme/FooGenTest.java:[12,8] cannot find symbol
[INFO] BUILD FAILURE
`
	got = DistillFailure(mvnOut, 4096)
	if !strings.Contains(got, "cannot find symbol") {
		t.Errorf("maven distill missing error line:\n%s", got)
	}
	if strings.Contains(got, "Scanning for projects") {
		t.Errorf("maven distill kept noise:\n%s", got)
	}
}

func TestDistillFailureFallbackAndCap(t *testing.T) {
	// No recognizable markers → last 60 lines.
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	got := DistillFailure(b.String(), 4096)
	if strings.Contains(got, "line 40\n") || !strings.Contains(got, "line 99") {
		t.Errorf("fallback tail wrong:\n%s", got)
	}

	// Cap in bytes.
	got = DistillFailure(b.String(), 50)
	if len(got) > 50 {
		t.Errorf("cap exceeded: %d bytes", len(got))
	}
}
