package codegen

import (
	"sort"
)

// BuildFileToDomain maps each member file path to its owning skill Name. On
// collision the first skill by sorted Name wins (deterministic). Member files
// come from the store's concept->files link (Skill.Files), not the card body.
func BuildFileToDomain(skills []Skill) map[string]string {
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	out := make(map[string]string)
	for _, s := range sorted {
		for _, f := range s.Files {
			if _, exists := out[f]; !exists {
				out[f] = s.Name
			}
		}
	}
	return out
}
