package codegen

import (
	"testing"
	"unicode/utf8"
)

func TestTrimToRuneBoundary_DoesNotSplitRune(t *testing.T) {
	// "é" is 2 bytes (0xC3 0xA9); "aé" is 3 bytes. Trimming to 2 bytes must drop
	// the incomplete é, yielding "a" (valid UTF-8), never "a\xc3".
	got := trimToRuneBoundary("aé", 2)
	if !utf8.ValidString(got) {
		t.Errorf("result is not valid UTF-8: %q", got)
	}
	if got != "a" {
		t.Errorf("want %q, got %q", "a", got)
	}
	if trimToRuneBoundary("abc", 0) != "abc" {
		t.Error("maxBytes<=0 means no limit")
	}
	if trimToRuneBoundary("abc", 10) != "abc" {
		t.Error("under budget must be unchanged")
	}
}
