package crystallize

// RED-phase property tests for cohesion-based clustering (feature
// @feature:cohesion-clustering, scenarios @s1..@s12). These assert the NEW
// behavior described in the spec/design:
//   - clustering is by COHESION (Jaccard >= 0.15 over a note's specific-token
//     set), not by each note's single strongest shared token;
//   - "umbrella" tokens (document-frequency ratio > 0.4 of the corpus) are
//     demoted out of the specific set, so they neither force-merge notes nor
//     become labels;
//   - clusters are connected components of the note-similarity graph;
//   - Spanish and English stopwords are excluded as signal and as labels;
//   - loose notes (no specific overlap with any cluster) are left OUT, not
//     absorbed;
//   - the label is the most-common specific token, alphabetical tie-break.
//
// The current implementation groups by single strongest token, so the
// new-behavior assertions here are EXPECTED to fail until Detect is rewritten.
//
// Data-design notes (why the numbers are what they are):
//   - The tokenizer drops tokens shorter than 3 chars, so every specific token
//     below is >= 3 chars. Numeric id suffixes on topic keys stay < 100 (1-2
//     digits) so they tokenize to nothing and only serve to keep keys unique.
//   - Umbrella demotion removes a token from a note's specific set when its
//     df-ratio exceeds 0.4. To keep a genuine shared token ALIVE in a small
//     dense cluster we pad the corpus with loose filler notes so the shared
//     token's df-ratio stays comfortably below 0.4. To make an umbrella token
//     exceed 0.4 we place it in (near) every note.
//   - "Window" clusters chain notes: consecutive notes share 2 specific tokens
//     (Jaccard 0.5), so the group is one connected component while no single
//     token's df-ratio climbs high enough to be demoted.

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/memory"
)

var baseTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// mkNote builds an observation whose specific-token signal is exactly `tokens`
// (placed in both the content and the topic-key slug). id (kept < 100) makes
// the topic key unique and sets a distinct UpdatedAt for stable ordering.
func mkNote(typ string, id int, tokens ...string) memory.Observation {
	slug := strings.Join(tokens, "-")
	return memory.Observation{
		TopicKey:  typ + "/" + slug + "-" + strconv.Itoa(id),
		Type:      typ,
		Content:   strings.Join(tokens, " "),
		UpdatedAt: baseTime.AddDate(0, 0, -id),
	}
}

// windowNotes builds a chain of notes over `tokens` using a sliding window of
// width `win`; each note also carries `umbrella` (empty string = none).
// Consecutive notes share win-1 tokens, forming one connected component.
func windowNotes(typ string, startID int, umbrella string, tokens []string, win int) []memory.Observation {
	var out []memory.Observation
	for i := 0; i+win <= len(tokens); i++ {
		set := make([]string, 0, win+1)
		if umbrella != "" {
			set = append(set, umbrella)
		}
		set = append(set, tokens[i:i+win]...)
		out = append(out, mkNote(typ, startID+i, set...))
	}
	return out
}

// fillerNotes returns n mutually-unrelated loose notes (each a unique token),
// used to pad a corpus so genuine shared tokens stay under the umbrella cutoff.
func fillerNotes(n, startID int) []memory.Observation {
	out := make([]memory.Observation, 0, n)
	for i := range n {
		out = append(out, mkNote("reference", startID+i, "filleruniq"+string(rune('a'+i))))
	}
	return out
}

func keySet(obs []memory.Observation) map[string]bool {
	m := make(map[string]bool, len(obs))
	for _, o := range obs {
		m[o.TopicKey] = true
	}
	return m
}

// allMembersIn reports whether every member of c has a topic key in want.
func allMembersIn(c Cluster, want map[string]bool) bool {
	for _, m := range c.Members {
		if !want[m.TopicKey] {
			return false
		}
	}
	return true
}

func hasLabel(cs []Cluster, label string) bool {
	for _, c := range cs {
		if c.Label == label {
			return true
		}
	}
	return false
}

// shuffled returns a fixed (deterministic) reordering of obs.
func shuffled(obs []memory.Observation) []memory.Observation {
	out := make([]memory.Observation, len(obs))
	for i := range obs {
		out[i] = obs[len(obs)-1-i]
	}
	return out
}

var spanishStops = []string{
	"que", "para", "con", "los", "las", "del", "una", "por", "como",
	"este", "esta", "pero", "desde", "sobre", "entre", "sin", "hay", "son", "más",
}

var englishStops = []string{"the", "and", "for", "with"}

func assertNoStopwordLabels(t *testing.T, cs []Cluster) {
	t.Helper()
	for _, c := range cs {
		for _, s := range spanishStops {
			if c.Label == s {
				t.Errorf("cluster label %q is a Spanish stopword", c.Label)
			}
		}
		for _, s := range englishStops {
			if c.Label == s {
				t.Errorf("cluster label %q is an English stopword", c.Label)
			}
		}
	}
}

// @s1 — Cohesion drives membership; a lone umbrella token does not cluster.
func TestDetect_CohesionDrivesMembership_UmbrellaOnlyNotAbsorbed(t *testing.T) {
	tokens := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
	cohesive := windowNotes("gotcha", 1, "webapi", tokens, 3) // 8 notes, chained
	umbrellaOnly := []memory.Observation{
		mkNote("reference", 30, "webapi"),
		mkNote("reference", 31, "webapi"),
	}
	all := append(append([]memory.Observation{}, cohesive...), umbrellaOnly...)

	got := Detect(all, 5)

	if len(got) != 1 {
		t.Fatalf("want exactly 1 cluster (the 8 cohesive notes), got %d: %v", len(got), labels(got))
	}
	if got[0].Size != 8 {
		t.Errorf("want cluster size 8, got %d", got[0].Size)
	}
	cohKeys := keySet(cohesive)
	if !allMembersIn(got[0], cohKeys) {
		t.Errorf("cluster contains a non-cohesive (umbrella-only) member: %v", got[0].Members)
	}
	umbKeys := keySet(umbrellaOnly)
	for _, c := range got {
		for _, m := range c.Members {
			if umbKeys[m.TopicKey] {
				t.Errorf("umbrella-only note %q must not be a cluster member", m.TopicKey)
			}
		}
	}
	if hasLabel(got, "webapi") {
		t.Errorf("no cluster label may equal the umbrella token %q: %v", "webapi", labels(got))
	}
}

// @s2 — Two dense themes sharing one umbrella token separate; umbrella never
// merges or labels.
func TestDetect_TwoThemesShareUmbrella_StaySeparate(t *testing.T) {
	profTokens := []string{"profa", "profb", "profc", "profd", "profe", "proff", "profg", "profh"}
	testTokens := []string{"testx", "testy", "testz", "testp", "testq", "testr", "tests", "testt"}
	prof := windowNotes("decision", 1, "webapi", profTokens, 3) // 6 notes
	test := windowNotes("testing", 20, "webapi", testTokens, 3) // 6 notes
	all := append(append([]memory.Observation{}, prof...), test...)

	got := Detect(all, 5)

	if len(got) != 2 {
		t.Fatalf("want 2 clusters (profile, testing), got %d: %v", len(got), labels(got))
	}
	theme := func(o memory.Observation) string {
		switch {
		case strings.Contains(o.Content, "prof"):
			return "profile"
		case strings.Contains(o.Content, "test"):
			return "testing"
		default:
			return "?"
		}
	}
	var sawProfile, sawTesting bool
	for _, c := range got {
		themes := map[string]bool{}
		for _, m := range c.Members {
			themes[theme(m)] = true
		}
		if themes["profile"] && themes["testing"] {
			t.Errorf("a cluster mixes profile and testing notes: %v", c.Members)
		}
		if themes["profile"] {
			sawProfile = true
			if c.Size != 6 {
				t.Errorf("profile cluster size = %d, want 6", c.Size)
			}
		}
		if themes["testing"] {
			sawTesting = true
			if c.Size != 6 {
				t.Errorf("testing cluster size = %d, want 6", c.Size)
			}
		}
	}
	if !sawProfile || !sawTesting {
		t.Errorf("want one profile cluster and one testing cluster; sawProfile=%v sawTesting=%v", sawProfile, sawTesting)
	}
	if hasLabel(got, "webapi") {
		t.Errorf("shared umbrella token %q must not label any cluster: %v", "webapi", labels(got))
	}
}

// @s3 — A loose note is left out, not absorbed, and its input value is unchanged.
func TestDetect_LooseNoteNotAbsorbed_InputUnchanged(t *testing.T) {
	tokens := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india"}
	cohesive := windowNotes("gotcha", 1, "webapi", tokens, 3) // 7 notes
	loose := mkNote("reference", 30, "webapi", "que", "los")  // only umbrella + stopwords
	all := append(append([]memory.Observation{}, cohesive...), loose)
	// Snapshot the WHOLE input (a deep copy of every element) so we catch any
	// mutation of any element or of the backing array, not just the loose note.
	before := append([]memory.Observation{}, all...)

	got := Detect(all, 5)

	if len(got) != 1 {
		t.Fatalf("want exactly 1 cluster (the 7 cohesive notes), got %d: %v", len(got), labels(got))
	}
	if got[0].Size != 7 {
		t.Errorf("want cluster size 7, got %d", got[0].Size)
	}
	if !allMembersIn(got[0], keySet(cohesive)) {
		t.Errorf("cluster absorbed the loose note: %v", got[0].Members)
	}
	for _, c := range got {
		for _, m := range c.Members {
			if m.TopicKey == loose.TopicKey {
				t.Errorf("loose note %q must not appear in any cluster", loose.TopicKey)
			}
		}
	}
	if !reflect.DeepEqual(all, before) {
		t.Errorf("Detect mutated its input slice:\n got:  %+v\n want: %+v", all, before)
	}
}

// @s4 — Spanish (and English) stopwords never drive clustering or labels.
func TestDetect_SpanishStopwordsExcludedFromLabels(t *testing.T) {
	// Six Spanish notes share two specific domain tokens (factura, cliente) plus
	// the Spanish stopword "que". "que" is the single most frequent word (also in
	// three filler notes), so the OLD single-token logic would label the cluster
	// "que"; the NEW logic must filter it and label with a specific token.
	var dense []memory.Observation
	for i := 1; i <= 6; i++ {
		dense = append(dense, mkNote("decision", i, "factura", "cliente", "que"))
	}
	plain := fillerNotes(9, 40)
	queFiller := []memory.Observation{
		mkNote("reference", 60, "palabrauno", "que"),
		mkNote("reference", 61, "palabrados", "que"),
		mkNote("reference", 62, "palabratres", "que"),
	}
	all := append(append(append([]memory.Observation{}, dense...), plain...), queFiller...)

	got := Detect(all, 5)

	if len(got) != 1 {
		t.Fatalf("want exactly 1 cluster (the 6 Spanish notes), got %d: %v", len(got), labels(got))
	}
	c := got[0]
	if c.Size != 6 || !allMembersIn(c, keySet(dense)) {
		t.Fatalf("cluster is not the 6 dense Spanish notes: size=%d members=%v", c.Size, c.Members)
	}
	assertNoStopwordLabels(t, got)
	if c.Label != "factura" && c.Label != "cliente" {
		t.Errorf("label %q must be one of the shared specific tokens {factura, cliente}", c.Label)
	}
}

// @s5 — Determinism: same input in any order yields identical clusters.
func TestDetect_DeterministicUnderShuffle(t *testing.T) {
	aTokens := []string{"atoka", "atokb", "atokc", "atokd", "atoke", "atokf", "atokg"}
	bTokens := []string{"btoka", "btokb", "btokc", "btokd", "btoke", "btokf", "btokg"}
	a := windowNotes("decision", 1, "webapi", aTokens, 3) // 5 notes
	b := windowNotes("testing", 20, "webapi", bTokens, 3) // 5 notes
	all := append(append([]memory.Observation{}, a...), b...)

	got1 := Detect(all, 5)
	got2 := Detect(shuffled(all), 5)

	if len(got1) != 2 {
		t.Fatalf("want 2 equal-size clusters, got %d: %v", len(got1), labels(got1))
	}
	if got1[0].Size != 5 || got1[1].Size != 5 {
		t.Errorf("want two size-5 clusters, got sizes %d and %d", got1[0].Size, got1[1].Size)
	}
	if !(got1[0].Label < got1[1].Label) {
		t.Errorf("equal-size clusters must be ordered by Label ascending, got %v", labels(got1))
	}
	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("shuffled input produced different clusters:\n original: %v\n shuffled: %v", got1, got2)
	}
}

// @s6 — Minimum size preserved: a sub-minSize cohesive group is not surfaced.
func TestDetect_MinSizePreserved(t *testing.T) {
	var dense []memory.Observation
	for i := 1; i <= 4; i++ {
		dense = append(dense, mkNote("bug", i, "mska", "mskb"))
	}
	all := append(dense, fillerNotes(7, 40)...) // pad so mska/mskb survive demotion

	if got := Detect(all, 5); len(got) != 0 {
		t.Errorf("minSize 5: a 4-note group must not surface, got %v", labels(got))
	}
	got := Detect(all, 3)
	if len(got) != 1 {
		t.Fatalf("minSize 3: want exactly 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Size != 4 {
		t.Errorf("minSize 3: want cluster size 4, got %d", got[0].Size)
	}
}

// @s7 — Skill records are excluded from clustering input.
func TestDetect_SkillRecordsExcluded(t *testing.T) {
	var dense []memory.Observation
	for i := 1; i <= 5; i++ {
		dense = append(dense, mkNote("gotcha", i, "srva", "srvb"))
	}
	skills := []memory.Observation{
		mkNote("skill", 10, "srva", "srvb"),
		mkNote("skill", 11, "srva", "srvb"),
	}
	all := append(append([]memory.Observation{}, dense...), skills...)
	all = append(all, fillerNotes(9, 40)...)

	got := Detect(all, 5)

	if len(got) == 0 {
		t.Fatalf("want the 5 cohesive non-skill notes to form a cluster, got none")
	}
	skillKeys := keySet(skills)
	for _, c := range got {
		for _, m := range c.Members {
			if m.Type == "skill" {
				t.Errorf("cluster member has Type \"skill\": %q", m.TopicKey)
			}
			if skillKeys[m.TopicKey] {
				t.Errorf("skill record %q must not be clustered", m.TopicKey)
			}
		}
	}
}

// @s8 — A single genuinely coherent theme is not over-split.
func TestDetect_SingleThemeNotOverSplit(t *testing.T) {
	tokens := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo"}
	cohesive := windowNotes("decision", 1, "", tokens, 3) // 9 notes, one chain

	got := Detect(cohesive, 5)

	if len(got) != 1 {
		t.Fatalf("want exactly 1 cluster for one dense theme, got %d: %v", len(got), labels(got))
	}
	if got[0].Size != 9 || !allMembersIn(got[0], keySet(cohesive)) {
		t.Errorf("want all 9 notes in one cluster, got size %d members %v", got[0].Size, got[0].Members)
	}
}

// @s9 — Empty and tiny corpora degrade gracefully.
func TestDetect_EmptyAndTinyCorpora(t *testing.T) {
	if got := Detect(nil, 5); len(got) != 0 {
		t.Errorf("empty corpus: want no clusters, got %v", labels(got))
	}
	three := []memory.Observation{
		mkNote("bug", 1, "uniqaa"),
		mkNote("bug", 2, "uniqbb"),
		mkNote("decision", 3, "uniqcc"),
	}
	if got := Detect(three, 5); len(got) != 0 {
		t.Errorf("three unrelated notes: want no clusters, got %v", labels(got))
	}
}

// @s10 — Label is a stable specific token (majority-present), never an umbrella.
func TestDetect_LabelIsStableMajoritySpecificToken(t *testing.T) {
	// Six dense notes all carry "checkout" (majority specific token) plus "cart"
	// on some. An umbrella token "webapi" sits in 8 of 18 notes (df-ratio ~0.44):
	// under the NEW rule it is demoted (> 0.4) and cannot label; under the OLD
	// rule (df below the 60% cutoff, higher than checkout's df) it would win.
	dense := []memory.Observation{
		mkNote("gotcha", 1, "webapi", "checkout", "cart"),
		mkNote("gotcha", 2, "webapi", "checkout", "cart"),
		mkNote("gotcha", 3, "webapi", "checkout", "cart"),
		mkNote("gotcha", 4, "webapi", "checkout", "cart"),
		mkNote("gotcha", 5, "webapi", "checkout"),
		mkNote("gotcha", 6, "webapi", "checkout"),
	}
	// Two filler notes also carry the umbrella (raising its df-ratio past 0.4);
	// ten plain filler notes keep "checkout"'s df-ratio well under 0.4.
	umbFiller := []memory.Observation{
		mkNote("reference", 30, "webapi", "solouno"),
		mkNote("reference", 31, "webapi", "solodos"),
	}
	all := append(append([]memory.Observation{}, dense...), umbFiller...)
	all = append(all, fillerNotes(10, 40)...) // total 18 notes

	got1 := Detect(all, 5)
	got2 := Detect(all, 5)

	if len(got1) == 0 {
		t.Fatalf("want a checkout cluster, got none")
	}
	// Find the cluster containing the dense checkout notes.
	denseKeys := keySet(dense)
	var checkout *Cluster
	for i := range got1 {
		if allMembersIn(got1[i], denseKeys) && got1[i].Size == 6 {
			checkout = &got1[i]
			break
		}
	}
	if checkout == nil {
		t.Fatalf("no single cluster holds exactly the 6 dense checkout notes: %v", got1)
	}
	if checkout.Label != "checkout" {
		t.Errorf("want label \"checkout\" (majority specific token), got %q", checkout.Label)
	}
	if hasLabel(got1, "webapi") {
		t.Errorf("umbrella token %q must not label any cluster: %v", "webapi", labels(got1))
	}
	assertNoStopwordLabels(t, got1)
	// Label present in a majority of members.
	present := 0
	for _, m := range checkout.Members {
		if strings.Contains(m.Content, checkout.Label) {
			present++
		}
	}
	if present*2 <= checkout.Size {
		t.Errorf("label %q present in only %d/%d members (need a majority)", checkout.Label, present, checkout.Size)
	}
	// Stable across runs.
	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("Detect is not stable across runs:\n run1: %v\n run2: %v", got1, got2)
	}
}

// @s11 — pins a DOCUMENTED, ACCEPTED limitation (spec §Limitations): umbrella
// demotion only applies once the corpus reaches umbrellaMinCorpus notes. Below
// that size no token is demoted, so two disjoint themes that share only an
// umbrella token merge into one cluster labelled with it. This is accepted
// because real crystallization corpora are dozens of notes (default minSize 5),
// and frequency alone cannot tell a bridging umbrella from the single token
// that DEFINES a small one-theme corpus without regressing legitimate small
// clusters (e.g. a 3-note "checkout" cluster). The guaranteed large-corpus
// separation is covered by @s2. A future bridge-aware demotion could close the
// gap; see the design's follow-up note.
func TestDetect_SmallCorpusUmbrellaLimitation(t *testing.T) {
	const umbrella = "webapi"
	var in []memory.Observation
	// Theme A: three notes sharing {alpha, bravo}. Theme B: {charlie, delta}.
	// All six carry the umbrella token; no specific token crosses the themes.
	for i := range 3 {
		in = append(in, mkNote("decision", i, umbrella, "alpha", "bravo"))
	}
	for i := range 3 {
		in = append(in, mkNote("gotcha", 10+i, umbrella, "charlie", "delta"))
	}
	got := Detect(in, 3) // 6 notes: below umbrellaMinCorpus, so no demotion
	if len(got) != 1 {
		t.Errorf("documented small-corpus limitation changed: want 1 merged cluster "+
			"below umbrellaMinCorpus(%d), got %d: %v — if demotion now covers tiny "+
			"corpora, update spec §Limitations and this test", umbrellaMinCorpus, len(got), labels(got))
	}
}

// @s12 — cluster Labels must be UNIQUE across clusters. The label is the
// load-bearing key downstream storage addresses a single cluster by
// (SkillName/SkillTopic, and the FormatWithStatus status map); two clusters
// sharing a label makes one unreachable to crystallize and collapses its
// status. Two disconnected chains whose most-common specific token is the same
// ("shared", in every note of both chains) must still get DISTINCT labels.
// Cross-chain notes share ONLY "shared", so their 5-token sets give Jaccard
// 1/9 < threshold and the chains stay separate components; within each chain
// consecutive notes overlap enough to connect, and "shared" (count 3) strictly
// outranks every chain-local token (count 2), so both chains prefer "shared".
func TestDetect_LabelsAreUniqueAcrossClusters(t *testing.T) {
	mkChain := func(typ string, startID int, pool [6]string) []memory.Observation {
		const s = "shared"
		return []memory.Observation{
			mkNote(typ, startID+0, s, pool[0], pool[1], pool[2], pool[3]),
			mkNote(typ, startID+1, s, pool[2], pool[3], pool[4], pool[5]),
			mkNote(typ, startID+2, s, pool[0], pool[1], pool[4], pool[5]),
		}
	}
	var in []memory.Observation
	in = append(in, mkChain("decision", 0, [6]string{"aaa", "bbb", "ccc", "ddd", "eee", "fff"})...)
	in = append(in, mkChain("gotcha", 20, [6]string{"ggg", "hhh", "iii", "jjj", "kkk", "lll"})...)
	got := Detect(in, 3) // 6 notes: below umbrellaMinCorpus, so "shared" is not demoted
	if len(got) != 2 {
		t.Fatalf("want 2 separate clusters, got %d: %v", len(got), labels(got))
	}
	if got[0].Label == got[1].Label {
		t.Errorf("clusters share a label %q — labels must be unique (load-bearing key): %v", got[0].Label, labels(got))
	}
}

// TestUniqueLabel_Fallback pins the disambiguation fallback directly (it is
// unreachable through Detect for minSize >= 2, since two separate components
// cannot share their entire specific-token set without a merging edge).
func TestUniqueLabel_Fallback(t *testing.T) {
	if got := uniqueLabel([]string{"alpha", "bravo"}, map[string]bool{"alpha": true, "bravo": true}); got != "alpha-2" {
		t.Errorf("all candidates taken: got %q, want %q", got, "alpha-2")
	}
	if got := uniqueLabel(nil, map[string]bool{}); got != "cluster-2" {
		t.Errorf("empty specific set: got %q, want %q", got, "cluster-2")
	}
	if got := uniqueLabel([]string{"xxx"}, map[string]bool{"xxx": true, "xxx-2": true}); got != "xxx-3" {
		t.Errorf("suffix must skip taken suffixes: got %q, want %q", got, "xxx-3")
	}
}
