package memory

import (
	"testing"
	"time"
)

// TestTimeFormatFixedWidthOrdersLexically guards that timeFormat produces a
// fixed-width fractional second, so the string comparisons SQLite does on
// timestamp columns (range filters like `created_at >= ?` and `ORDER BY ... DESC`)
// match chronological order. RFC3339Nano trims trailing zeros, giving
// variable-width fractions where a later instant can sort BEFORE an earlier one
// as a string (".0945Z" vs ".094545Z" → 'Z' > '4'), which is the root cause of
// the flaky session/summary ordering.
func TestTimeFormatFixedWidthOrdersLexically(t *testing.T) {
	// earlier has nanoseconds ending in zeros (.094500000); later does not.
	earlier := time.Date(2026, 6, 20, 14, 37, 37, 94500000, time.UTC)
	later := time.Date(2026, 6, 20, 14, 37, 37, 94545000, time.UTC)
	if !earlier.Before(later) {
		t.Fatal("test setup wrong: earlier must be chronologically before later")
	}
	es := earlier.Format(timeFormat)
	ls := later.Format(timeFormat)
	if es >= ls {
		t.Errorf("lexical order broken: %q (earlier) must sort before %q (later)", es, ls)
	}
	if len(es) != len(ls) {
		t.Errorf("fractional width not fixed: %q (%d) vs %q (%d)", es, len(es), ls, len(ls))
	}
}
