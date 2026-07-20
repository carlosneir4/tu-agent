package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/carlosneir4/tu-agent/internal/graph/query"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// Named caps for the mechanical grounding block spliced into planning-stage
// prompts (see internal/tdd.ComposeStagePromptWithGrounding).
const (
	groundingSourceCap    = 2048           // per-source cap, bytes
	groundingMaxNotes     = 5              // decision+gotcha note count cap
	groundingMaxDecisions = 3              // reserved decision slots within groundingMaxNotes
	groundingMaxGotchas   = 2              // reserved gotcha slots within groundingMaxNotes
	groundingNoteBodyCap  = 400            // per-note body length cap, bytes
	groundingGapTop       = 5              // top-N untested gaps
	groundingTruncMarker  = " (truncated)" // appended when a source is cut
)

// groundingStopwords are generic English joiners dropped from the base-dir
// slug when deriving memory-search keywords.
var groundingStopwords = map[string]bool{
	"and": true, "or": true, "the": true, "a": true, "an": true,
	"of": true, "to": true, "in": true, "on": true, "for": true,
	"with": true, "is": true, "by": true,
}

// buildGrounding assembles a size-capped block of mechanical project facts —
// an architecture-overview excerpt, matching decision/gotcha memory notes,
// and blast-radius/test-gap lines — for the analyst and architect planning
// stages only. It returns "" for any other stage (skipping all store work)
// and whenever every source is empty or fails to load. relBase is the
// per-feature base dir (the --base value, e.g. ".tu-agent/tdd/<slug>") whose
// leaf name seeds the memory-search keywords for source 2.
//
// Fail-soft by design: a missing graph.db, missing memory.db, open error, or
// empty result just omits that source (logged via slog.Warn); buildGrounding
// never returns an error and never panics, so `tdd prompt` cannot fail
// because of grounding.
func buildGrounding(root, stage, relBase string) string {
	if stage != "analyst" && stage != "architect" {
		return ""
	}

	var sections []string
	if s := groundingArchitectureSection(root); s != "" {
		sections = append(sections, s)
	}
	if s := groundingDecisionsSection(root, relBase); s != "" {
		sections = append(sections, s)
	}
	if s := groundingGapsSection(root); s != "" {
		sections = append(sections, s)
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// groundingArchitectureSection returns the "### Architecture overview
// (excerpt)" sub-section from the graph store's synthesized narrative, or ""
// if none has been synthesized yet, graph.db does not exist, or the graph
// store cannot be opened.
func groundingArchitectureSection(root string) string {
	if !dbExists(graphDBPath(root)) {
		return ""
	}
	body, err := loadArchitecture()
	if err != nil {
		slog.Warn("grounding: architecture overview unavailable", "err", err)
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return "### Architecture overview (excerpt)\n\n" + capSource(body)
}

// groundingDecisionsSection returns the "### Relevant decisions & gotchas"
// sub-section: decision and gotcha memory notes matching keywords derived
// from relBase's leaf slug, or "" if the memory store is unavailable, relBase
// yields no keywords, or no note matches.
func groundingDecisionsSection(root, relBase string) string {
	keywords := groundingKeywords(relBase)
	if keywords == "" {
		return ""
	}
	if !dbExists(memoryDBPath(root)) {
		return ""
	}

	var decisions, gotchas []memory.Observation
	err := withMemStore(root, func(st *memory.Store) error {
		var err error
		decisions, _, err = st.Search(keywords, "decision", groundingMaxNotes)
		if err != nil {
			return err
		}
		gotchas, _, err = st.Search(keywords, "gotcha", groundingMaxNotes)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		slog.Warn("grounding: memory search unavailable", "err", err)
		return ""
	}
	if len(decisions) == 0 && len(gotchas) == 0 {
		return ""
	}
	decisionSlots, gotchaSlots := reservedSlots(len(decisions), len(gotchas))
	notes := make([]memory.Observation, 0, decisionSlots+gotchaSlots)
	notes = append(notes, decisions[:decisionSlots]...)
	notes = append(notes, gotchas[:gotchaSlots]...)

	var b strings.Builder
	for _, n := range notes {
		title := n.Title
		if title == "" {
			title = n.TopicKey
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", title, truncateRunes(n.Content, groundingNoteBodyCap))
	}
	return "### Relevant decisions & gotchas\n\n" + capSource(strings.TrimRight(b.String(), "\n"))
}

// groundingGapsSection returns the "### Blast radius & test gaps"
// sub-section: the top-N highest-risk untested exported symbols, reusing the
// same query.Graph.UntestedGaps machinery as `tu-agent test gaps`. Scoped to
// the whole repo — a reliable target symbol is not mechanically derivable
// from the base-dir slug. Returns "" if graph.db does not exist or the graph
// store is unavailable, empty, or there are no gaps.
func groundingGapsSection(root string) string {
	if !dbExists(graphDBPath(root)) {
		return ""
	}
	g, err := loadQueryGraph()
	if err != nil {
		slog.Warn("grounding: graph unavailable", "err", err)
		return ""
	}
	gaps, err := g.UntestedGaps(query.GapOptions{Top: groundingGapTop, MinLines: 4, Depth: 2})
	if err != nil {
		slog.Warn("grounding: untested gaps query failed", "err", err)
		return ""
	}
	if len(gaps) == 0 {
		return ""
	}
	return "### Blast radius & test gaps\n\n" + capSource(strings.TrimRight(query.FormatGaps(gaps), "\n"))
}

// groundingKeywords derives memory-search keywords from relBase's leaf slug
// (the per-feature base dir, e.g. ".tu-agent/tdd/<slug>"): hyphen-split, drop
// groundingStopwords, join with spaces. E.g.
// "mechanical-grounding-injection-and-spec" -> "mechanical grounding
// injection spec" (drops "and"). "" (relBase empty or all stopwords) means no
// keywords — the caller omits the source rather than falling back to any
// other name.
func groundingKeywords(relBase string) string {
	if strings.TrimSpace(relBase) == "" {
		return ""
	}
	words := strings.Split(filepath.Base(relBase), "-")
	kept := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ToLower(w)
		if w == "" || groundingStopwords[w] {
			continue
		}
		kept = append(kept, w)
	}
	return strings.Join(kept, " ")
}

// reservedSlots divides groundingMaxNotes between decisions and gotchas,
// reserving groundingMaxDecisions/groundingMaxGotchas each but backfilling
// unused slots from whichever type has more matches — so a keyword with 5+
// decision matches no longer starves gotchas out of the combined section
// entirely. The two returned counts never exceed the corresponding input
// count and always sum to at most groundingMaxNotes.
func reservedSlots(numDecisions, numGotchas int) (decisionSlots, gotchaSlots int) {
	decisionSlots, gotchaSlots = groundingMaxDecisions, groundingMaxGotchas
	if numDecisions < decisionSlots {
		gotchaSlots += decisionSlots - numDecisions
		decisionSlots = numDecisions
	}
	if numGotchas < gotchaSlots {
		decisionSlots += gotchaSlots - numGotchas
		if decisionSlots > numDecisions {
			decisionSlots = numDecisions
		}
		gotchaSlots = numGotchas
	}
	return decisionSlots, gotchaSlots
}

// dbExists reports whether a store file already exists at path, WITHOUT
// opening it. `tdd prompt` is a read-only, side-effect-free command: both
// openGraphStore and memory.Open MkdirAll + create their DB file on first
// open, so calling them on a repo with no store would leave behind an empty
// graph.db/memory.db (and, on a version-mismatch rebuild, could wipe an
// existing one). Stat-guarding here keeps a missing store a plain "skip this
// grounding source" instead of a disk write.
func dbExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// capSource truncates s to at most groundingSourceCap bytes on a UTF-8 rune
// boundary and appends groundingTruncMarker, so a multibyte character is
// never split and the cut is visible to the reader.
func capSource(s string) string {
	if len(s) <= groundingSourceCap {
		return s
	}
	cut := groundingSourceCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + groundingTruncMarker
}

// truncateRunes truncates s to at most n bytes on a UTF-8 rune boundary,
// without a marker (used for the compact per-note body excerpt).
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
