package memory

import "testing"

// TestNewID_HappyPath pins newID's contract on the success path: a 16-hex-char
// identifier, with no error, and unique across calls. The RNG-failure branch
// is not deterministically injectable without a speculative seam (see
// newID's doc comment) and is intentionally left uncovered by design.
func TestNewID_HappyPath(t *testing.T) {
	id1, err := newID()
	if err != nil {
		t.Fatalf("newID: %v", err)
	}
	if len(id1) != 16 {
		t.Errorf("len(newID()) = %d, want 16", len(id1))
	}
	for _, c := range id1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("newID() = %q, want lowercase hex only", id1)
			break
		}
	}
	id2, err := newID()
	if err != nil {
		t.Fatalf("newID: %v", err)
	}
	if id1 == id2 {
		t.Errorf("newID() returned the same id twice: %q", id1)
	}
}
