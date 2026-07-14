package main

import (
	"github.com/carlosneir4/tu-agent/internal/codegen"
	"github.com/carlosneir4/tu-agent/internal/memory"
)

// filterSecretRecords partitions records into kept (safe) and excluded (a note
// whose Title or Content trips codegen.ContentLikelySecret). Team chunks are
// shared via git; a leaked secret must never be written to one.
func filterSecretRecords(recs []memory.ChunkRecord) (kept, excluded []memory.ChunkRecord) {
	kept = make([]memory.ChunkRecord, 0, len(recs))
	for _, r := range recs {
		if codegen.ContentLikelySecret(r.Title) || codegen.ContentLikelySecret(r.Content) {
			excluded = append(excluded, r)
			continue
		}
		kept = append(kept, r)
	}
	return kept, excluded
}
