package testgen

import "strings"

// AnnotateMutation inserts (or replaces) a "MUTATION:" comment line in the
// generated test block. For Go the generated funcs sit at EOF, so the note is
// appended; for sentinel languages it goes just before the genEnd marker.
// Idempotent: an existing MUTATION line is replaced so re-runs do not stack.
func AnnotateMutation(language, content, note string) string {
	cp := commentPrefix(language)
	marker := cp + " MUTATION: "
	content = stripMutationLines(content, marker)
	line := marker + note

	idx := strings.Index(content, genEnd)
	if idx < 0 {
		// No sentinel region (Go): append at EOF.
		return strings.TrimRight(content, "\n") + "\n" + line + "\n"
	}
	// Insert before the line containing genEnd.
	start := strings.LastIndex(content[:idx], "\n") + 1
	return content[:start] + line + "\n" + content[start:]
}

// stripMutationLines removes any existing MUTATION comment lines for this block.
func stripMutationLines(content, marker string) string {
	prefix := strings.TrimSpace(marker)
	lines := strings.Split(content, "\n")
	out := lines[:0]
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), prefix) {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
