package crystallize

// RED-phase tests for the community-detection rewrite of Detect
// (@feature:detect-community-detection, scenarios @s1..@s4).
//
// Today Detect clusters by CONNECTED COMPONENTS of the note-similarity graph
// (single-linkage): any transitive chain of Jaccard->=cohesionThreshold edges
// collapses into one component. On a dense multi-theme corpus bridged by a
// couple of "epic" notes that transitively links every theme into one giant
// "blob". The feature replaces components with a modularity community pass over
// internal/cluster, quantizing each Jaccard weight to round(jaccard*10000).
//
// @s1 asserts the DESIRED post-rewrite behavior (the blob splits into >=2
// thematic communities, none a majority) -> RED today. @s2 calls the not-yet-
// existing quantize helper -> compile error -> RED. @s3/@s4 are determinism and
// purity guards that pass today and must keep passing through the new path.
//
// Corpus (generic, per CLAUDE.md Section 9): an "acme-checkout" migration epic
// with three thematic spokes -- auth {oauth,login,session}, telemetry
// {metrics,tracing,spans,latency}, retry {retry,backoff,jitter} -- wired into a
// single connected component by two epic bridge notes that each share two
// tokens with two adjacent spokes. Every token's document frequency stays at
// most 4/11 (0.36), below umbrellaCutoff(0.4) at n>=umbrellaMinCorpus(8), so no
// token is demoted and the chain link is never erased.

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

// cnote builds one observation whose specific-token signal is exactly `tokens`.
// The topic key slug is a bare integer (all-digit -> tokenizes to nothing) so
// the note's specific set is precisely `tokens`; id also sets a distinct
// UpdatedAt (via obs's daysAgo) for stable ordering. Uses the existing obs
// helper from crystallize_test.go.
func cnote(typ string, id int, tokens ...string) memory.Observation {
	return obs(typ+"/"+strconv.Itoa(id), typ, "", strings.Join(tokens, " "), id)
}

// denseMultiThemeCorpus returns the 11-note bridged corpus described above.
// Three dense spokes (pairwise Jaccard 1.0 within a spoke) plus two epic bridge
// notes (Jaccard ~0.33-0.4 to each adjacent spoke). Under today's single-
// linkage Detect this is ONE component of 11; under community detection it must
// separate into the underlying themes.
func denseMultiThemeCorpus() []memory.Observation {
	return []memory.Observation{
		// auth spoke
		cnote("gotcha", 1, "oauth", "login", "session"),
		cnote("gotcha", 2, "oauth", "login", "session"),
		cnote("gotcha", 3, "oauth", "login", "session"),
		// telemetry spoke
		cnote("gotcha", 4, "metrics", "tracing", "spans", "latency"),
		cnote("gotcha", 5, "metrics", "tracing", "spans", "latency"),
		cnote("gotcha", 6, "metrics", "tracing", "spans", "latency"),
		// retry spoke
		cnote("gotcha", 7, "retry", "backoff", "jitter"),
		cnote("gotcha", 8, "retry", "backoff", "jitter"),
		cnote("gotcha", 9, "retry", "backoff", "jitter"),
		// epic bridges (the acme-checkout migration touching two spokes each)
		cnote("decision", 10, "login", "session", "metrics", "spans"),   // auth <-> telemetry
		cnote("decision", 11, "tracing", "latency", "retry", "backoff"), // telemetry <-> retry
	}
}

// @s1 — A dense multi-theme corpus splits into communities, not one blob.
// Today's connected-components Detect returns ONE cluster holding all 11 notes
// (verified: clusters=1 maxSize=11 label "backoff"); the two ends of the chain
// (auth vs retry) share no token, but the epic bridges transitively merge every
// theme. The DESIRED post-rewrite behavior: at least two thematic communities
// and no single community holding a majority of the corpus. RED today.
func TestDetect_MultiThemeSplitsNotBlob_s1(t *testing.T) {
	in := denseMultiThemeCorpus()

	got := Detect(in, 3)

	// Run-note: what today's single-linkage Detect produces on this corpus.
	maxSize := 0
	for _, c := range got {
		if c.Size > maxSize {
			maxSize = c.Size
		}
	}
	t.Logf("@s1 run-note: corpus=%d clusters=%d maxSize=%d labels=%v", len(in), len(got), maxSize, labels(got))

	// DESIRED behavior (fails today: one blob).
	if len(got) < 2 {
		t.Errorf("want >=2 thematic communities, got %d: %v", len(got), labels(got))
	}
	half := len(in) / 2 // 5
	for _, c := range got {
		if c.Size > half {
			t.Errorf("cluster %q has size %d > len(corpus)/2 (%d): single-linkage blob not split: %v",
				c.Label, c.Size, half, labels(got))
		}
	}
}

// @s2 — Quantization keeps realistic near-tie Jaccard values distinct.
// 3/8 = 0.3750 and 4/11 ~= 0.363636 are genuinely different ratios that are
// numerically close; at scale quantScale(10000) they must NOT collapse to the
// same integer weight. quantize is undefined until the implementer adds it, so
// this file will not compile today -> RED (compile error) for the package.
func TestQuantize_NearTiesStayDistinct_s2(t *testing.T) {
	x := 3.0 / 8.0  // 0.375
	y := 4.0 / 11.0 // 0.36363636...

	if quantize(x) == quantize(y) {
		t.Errorf("quantize collapsed distinct Jaccard values: quantize(%v)=%d == quantize(%v)=%d",
			x, quantize(x), y, quantize(y))
	}
	// Pin the exact rounding semantics round(j*10000).
	if got, want := quantize(x), int(math.Round(x*10000)); got != want {
		t.Errorf("quantize(%v) = %d, want %d", x, got, want)
	}
	if got, want := quantize(y), int(math.Round(y*10000)); got != want {
		t.Errorf("quantize(%v) = %d, want %d", y, got, want)
	}
	if quantize(x) != 3750 {
		t.Errorf("quantize(3/8) = %d, want 3750", quantize(x))
	}
	if quantize(y) != 3636 {
		t.Errorf("quantize(4/11) = %d, want 3636", quantize(y))
	}
}

// @s3 — Determinism holds through the community + quantization path. Running
// Detect on the corpus and on a deterministically reversed copy (shuffled, from
// crystallize's existing helper -- no math/rand) must yield identical clusters,
// members, and ordering. Passes today; a guard that the new path stays
// order-independent.
func TestDetect_DeterministicUnderReverse_s3(t *testing.T) {
	in := denseMultiThemeCorpus()

	got1 := Detect(in, 3)
	got2 := Detect(shuffled(in), 3)

	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("reversed input produced different clusters:\n original: %v\n reversed: %v",
			labels(got1), labels(got2))
	}
}

// @s4 — Detect remains a pure function of its arguments. Detect's signature is
// Detect([]memory.Observation, int) []Cluster: it takes no store, DB, or FS
// handle, so it structurally admits no I/O -- purity is therefore a property of
// the input slice only. This guards that (a) Detect does not mutate the input
// slice or its elements, and (b) it is a pure function of its arguments: two
// calls on the same input, with no external setup (no temp dir, no sqlite
// handle), return DeepEqual results. Passes today.
func TestDetect_PureNoInputMutation_s4(t *testing.T) {
	in := denseMultiThemeCorpus()
	// Snapshot every element by value (Observation has no reference fields), so
	// any mutation of an element or the backing array is caught.
	before := append([]memory.Observation{}, in...)

	got1 := Detect(in, 3)

	if !reflect.DeepEqual(in, before) {
		t.Errorf("Detect mutated its input slice:\n got:  %+v\n want: %+v", in, before)
	}

	// Pure function of arguments: a second call with no external setup returns
	// the same result. The signature admits no store/DB handle, so there is no
	// I/O path to make this differ.
	got2 := Detect(in, 3)
	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("Detect is not a pure function of its arguments:\n run1: %v\n run2: %v",
			labels(got1), labels(got2))
	}
}
