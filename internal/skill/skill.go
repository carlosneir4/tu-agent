package skill

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Entry is the parsed frontmatter of one SKILL.md file.
type Entry struct {
	Name        string
	Description string
	Triggers    []string
	Path        string // absolute path to the SKILL.md file
}

// Index is an in-memory map of skill name → Entry.
type Index struct {
	entries map[string]Entry
}

// New returns an empty Index.
func New() *Index {
	return &Index{entries: make(map[string]Entry)}
}

// Add adds or replaces an entry.
func (idx *Index) Add(e Entry) {
	idx.entries[e.Name] = e
}

// Get returns the entry for the given name, or false if not found.
func (idx *Index) Get(name string) (Entry, bool) {
	e, ok := idx.entries[name]
	return e, ok
}

// All returns all entries sorted by name.
func (idx *Index) All() []Entry {
	entries := make([]Entry, 0, len(idx.entries))
	for _, e := range idx.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Len returns the number of entries in the index.
func (idx *Index) Len() int {
	return len(idx.entries)
}

// Summary returns a compact listing of all skills: "- name: description\n..."
// Entries are sorted by name. Returns empty string if the index is empty.
func (idx *Index) Summary() string {
	entries := idx.All()
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("- %s: %s", e.Name, e.Description))
	}
	return strings.Join(lines, "\n")
}

// LoadContent reads and returns the full content of the named skill's SKILL.md.
func (idx *Index) LoadContent(name string) (string, error) {
	e, ok := idx.entries[name]
	if !ok {
		return "", fmt.Errorf("skill %q not found in index", name)
	}
	data, err := os.ReadFile(e.Path)
	if err != nil {
		return "", fmt.Errorf("reading skill %q: %w", name, err)
	}
	return string(data), nil
}
