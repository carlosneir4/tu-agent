package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tu/tu-agent/internal/graph"
)

// Concept is a semantic unit of the codebase: one direct subpackage of the
// configured concept root (or one clustered domain when no root is set).
// Each concept becomes one thin index card.
type Concept struct {
	Name    string   // kebab leaf segment, e.g. "catalog"
	Package string   // full dotted package, e.g. "com.acme.shop.catalog"
	Files   []string // repo-relative paths; sorted
}

// DiscoverConcepts groups files into concepts by the direct subpackages of
// each root, unioning across roots. Files directly in a root form a residue
// concept named by the root's leaf segment. Files outside all roots
// (or without a package) are excluded. Returns nil when roots is empty or nil:
// the caller falls back to the domain map. Leaf-name collisions (same name from
// different roots) are qualified to kebabPackage(pkg, "") to avoid PRIMARY KEY
// conflicts; unique leaves stay bare. Pure and deterministic.
func DiscoverConcepts(units []SourceUnit, roots []string) []Concept {
	if len(roots) == 0 {
		return nil
	}
	groups := map[string][]string{} // concept package -> files
	for _, u := range units {
		if u.Package == "" {
			continue
		}
		for _, root := range roots {
			if u.Package == root {
				groups[root] = append(groups[root], u.Path)
				break
			}
			if strings.HasPrefix(u.Package, root+".") {
				seg, _, _ := strings.Cut(strings.TrimPrefix(u.Package, root+"."), ".")
				groups[root+"."+seg] = append(groups[root+"."+seg], u.Path)
				break
			}
		}
	}
	out := make([]Concept, 0, len(groups))
	for p, files := range groups {
		sorted := append([]string(nil), files...)
		sort.Strings(sorted)
		segs := strings.Split(p, ".")
		out = append(out, Concept{Name: strings.ToLower(segs[len(segs)-1]), Package: p, Files: sorted})
	}
	// Collision qualification: concepts.name is the store PRIMARY KEY, so a leaf
	// name shared by more than one concept (e.g. packages/app and
	// native-packages/app both leaf "app") would clobber. Rename ALL colliders to
	// the unique kebab slug of their full package; unique leaves stay bare.
	leafCount := map[string]int{}
	for _, c := range out {
		leafCount[c.Name]++
	}
	for i := range out {
		if leafCount[out[i].Name] > 1 {
			out[i].Name = kebabPackage(out[i].Package, "")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Package < out[j].Package
	})
	return out
}

// Landmark is a high-fan-in symbol that anchors a concept.
type Landmark struct {
	Name  string
	Path  string
	FanIn int  // incoming calls/extends/implements, methods aggregated to their type
	Entry bool // referenced from outside the concept
}

// Trait is an interface implemented both inside and outside a concept —
// changes to it propagate across concepts.
type Trait struct {
	Name              string
	Path              string   // where the interface is defined
	OtherImplementers []string // implementer type names outside this concept; sorted, capped
}

// ConceptCard is a Concept plus its computed knowledge-card content.
type ConceptCard struct {
	Concept
	Definition string // one-line generative description; "" = deterministic fallback
	Landmarks  []Landmark
	Traits     []Trait
}

const maxTraitImplementers = 6

// BuildConceptCards computes landmarks and shared traits for each concept from
// node-level graph data. Method fan-in aggregates to the containing type via
// contains edges. A landmark is an entry point when any incoming edge originates
// in a different concept. maxLandmarks caps the list (entry points rank by the
// same fan-in order). Pure and deterministic.
func BuildConceptCards(concepts []Concept, nodes []graph.Node, edges []graph.Edge, maxLandmarks int) []ConceptCard {
	conceptOf := map[string]int{} // file path -> concept index
	for i, c := range concepts {
		for _, f := range c.Files {
			conceptOf[f] = i
		}
	}
	nodeByID := make(map[string]graph.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}
	parentOf := map[string]string{} // member node -> containing type
	for _, e := range edges {
		if e.Kind == graph.EdgeContains {
			parentOf[e.To] = e.From
		}
	}
	// anchor resolves a node to its top-level containing type.
	anchor := func(id string) string {
		for {
			p, ok := parentOf[id]
			if !ok {
				return id
			}
			id = p
		}
	}
	nodeConcept := func(id string) (int, bool) {
		n, ok := nodeByID[id]
		if !ok || n.Path == "" {
			return 0, false
		}
		ci, ok := conceptOf[n.Path]
		return ci, ok
	}

	fanIn := map[string]int{}               // anchored type id -> incoming edge count
	external := map[string]bool{}           // anchored type id -> has caller in another concept
	implementersOf := map[string][]string{} // interface id -> implementer type ids
	for _, e := range edges {
		switch e.Kind {
		case graph.EdgeCalls, graph.EdgeExtends, graph.EdgeImplements:
			to := anchor(e.To)
			from := anchor(e.From)
			if to == from {
				continue
			}
			fanIn[to]++
			tc, tok := nodeConcept(to)
			fc, fok := nodeConcept(from)
			if tok && (!fok || fc != tc) {
				external[to] = true
			}
			if e.Kind == graph.EdgeImplements {
				implementersOf[e.To] = append(implementersOf[e.To], from)
			}
		}
	}

	out := make([]ConceptCard, 0, len(concepts))
	for ci, c := range concepts {
		card := ConceptCard{Concept: c}

		// Landmarks: types in this concept ranked by fan-in.
		seen := map[string]bool{}
		for _, n := range nodes {
			id := anchor(n.ID)
			if seen[id] || id != n.ID { // only anchored (top-level) nodes
				continue
			}
			if nc, ok := nodeConcept(id); !ok || nc != ci {
				continue
			}
			seen[id] = true
			card.Landmarks = append(card.Landmarks, Landmark{
				Name: n.Name, Path: n.Path, FanIn: fanIn[id], Entry: external[id],
			})
		}
		sort.Slice(card.Landmarks, func(i, j int) bool {
			if card.Landmarks[i].FanIn != card.Landmarks[j].FanIn {
				return card.Landmarks[i].FanIn > card.Landmarks[j].FanIn
			}
			return card.Landmarks[i].Name < card.Landmarks[j].Name
		})
		if maxLandmarks > 0 && len(card.Landmarks) > maxLandmarks {
			card.Landmarks = card.Landmarks[:maxLandmarks]
		}

		// Shared traits: interfaces implemented here AND elsewhere.
		traitSeen := map[string]bool{}
		for ifaceID, impls := range implementersOf {
			var inside bool
			var outside []string
			for _, impl := range impls {
				ic, ok := nodeConcept(impl)
				switch {
				case ok && ic == ci:
					inside = true
				default:
					if n, ok2 := nodeByID[impl]; ok2 {
						outside = append(outside, n.Name)
					}
				}
			}
			if !inside || len(outside) == 0 || traitSeen[ifaceID] {
				continue
			}
			traitSeen[ifaceID] = true
			sort.Strings(outside)
			if len(outside) > maxTraitImplementers {
				outside = outside[:maxTraitImplementers]
			}
			iface := nodeByID[ifaceID]
			card.Traits = append(card.Traits, Trait{Name: iface.Name, Path: iface.Path, OtherImplementers: outside})
		}
		sort.Slice(card.Traits, func(i, j int) bool { return card.Traits[i].Name < card.Traits[j].Name })

		out = append(out, card)
	}
	return out
}

const (
	cardMaxBytes = 1024
	cardMaxLines = 30
)

// RenderConceptCard renders a card as SKILL.md, enforcing the hard budget
// (≤ cardMaxLines lines, ≤ cardMaxBytes bytes) by shrinking the landmark and
// trait lists until it fits.
func RenderConceptCard(c ConceptCard) string {
	landmarks, traits := c.Landmarks, c.Traits
	for {
		out := renderCard(c, landmarks, traits)
		if len(out) <= cardMaxBytes && strings.Count(out, "\n") <= cardMaxLines {
			return out
		}
		switch {
		case len(traits) > 3:
			traits = traits[:3]
		case len(landmarks) > 5:
			landmarks = landmarks[:5]
		case len(traits) > 0:
			traits = traits[:len(traits)-1]
		case len(landmarks) > 1:
			landmarks = landmarks[:len(landmarks)-1]
		default:
			return out // minimal card; accept marginal overflow rather than loop forever
		}
	}
}

func renderCard(c ConceptCard, landmarks []Landmark, traits []Trait) string {
	desc := c.Definition
	if desc == "" {
		names := make([]string, 0, 3)
		for i, l := range landmarks {
			if i == 3 {
				break
			}
			names = append(names, l.Name)
		}
		desc = fmt.Sprintf("%s — %d files; landmarks: %s", c.Package, len(c.Files), strings.Join(names, ", "))
	}
	var sb strings.Builder
	// Quote description if it contains characters that break YAML bare values.
	if strings.ContainsAny(desc, `:"'`) {
		desc = `"` + strings.ReplaceAll(desc, `"`, `\"`) + `"`
	}
	fmt.Fprintf(&sb, "---\nname: %s\ndescription: %s\n---\n\n# %s (%s)\n\n", c.Name, desc, c.Name, c.Package)
	if len(landmarks) > 0 {
		fmt.Fprintf(&sb, "Landmarks (by fan-in):\n")
		for _, l := range landmarks {
			// File-level landmarks (e.g. Java) have Name == Path; render the
			// location once instead of "path (path)".
			loc := l.Name
			if l.Path != "" && l.Path != l.Name {
				loc = fmt.Sprintf("%s (%s)", l.Name, l.Path)
			}
			if l.Entry {
				fmt.Fprintf(&sb, "- %s — entry point\n", loc)
			} else {
				fmt.Fprintf(&sb, "- %s\n", loc)
			}
		}
	}
	if len(traits) > 0 {
		fmt.Fprintf(&sb, "\nShared traits (changes propagate beyond this concept):\n")
		for _, tr := range traits {
			fmt.Fprintf(&sb, "- %s — also implemented by: %s\n", tr.Name, strings.Join(tr.OtherImplementers, ", "))
		}
	}
	fmt.Fprintf(&sb, "\nFresh structure: use get_context/get_impact on a landmark; find_symbol to locate.\n")
	return sb.String()
}

// ConceptsFromDomains adapts a clustered domain map into concepts (the
// fallback when no concept root is configured). Parent markers (Files == nil)
// are skipped. Output is sorted by Name then Package, matching DiscoverConcepts.
func ConceptsFromDomains(domains []Domain) []Concept {
	out := make([]Concept, 0, len(domains))
	for _, d := range domains {
		if d.Files == nil {
			continue
		}
		files := append([]string(nil), d.Files...)
		sort.Strings(files)
		out = append(out, Concept{Name: d.Name, Package: d.Package, Files: files})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Package < out[j].Package
	})
	return out
}
