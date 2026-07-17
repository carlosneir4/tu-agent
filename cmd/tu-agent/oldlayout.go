package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// oldLayoutGuard detects a repo still on the OLD flat .tu-agent layout and, when
// found with the NEW counterpart absent, returns a non-nil error naming each old
// path and where to move it (old -> new form). It creates nothing — pure
// detection. See the old-layout-guard feature for the marker table.
//
// Detection keys ONLY on exact old FILE paths and the chunk-file glob — never on
// directory existence. In particular the old chunks dir ".tu-agent/memory/chunks"
// and the NEW memory subsystem dir ".tu-agent/memory" share the "memory/" prefix,
// so a repo carrying only ".tu-agent/memory/memory.db" (new layout) must not be
// flagged. Chunks are keyed on actual chunk FILES via glob, not on the dir.
func oldLayoutGuard(root string) error {
	// Fresh repo: no .tu-agent at all → nothing to guard.
	if _, err := os.Stat(tuAgentDir(root)); os.IsNotExist(err) {
		return nil
	}

	// A pair violates when the OLD marker exists on disk AND its NEW counterpart
	// is absent. Order is fixed so the message is deterministic.
	type marker struct {
		oldPath string // absolute on-disk path of the old file (empty ⇒ glob-based)
		oldGlob string // absolute glob for the old chunk files (empty ⇒ path-based)
		newPath string // absolute on-disk path of the new counterpart
		mapping string // "old -> new" forward-slash literal for the message
	}
	markers := []marker{
		{
			oldPath: filepath.Join(tuAgentDir(root), "memory.db"),
			newPath: memoryDBPath(root),
			mapping: ".tu-agent/memory.db -> .tu-agent/memory/memory.db",
		},
		{
			oldPath: filepath.Join(tuAgentDir(root), "graph.db"),
			newPath: graphDBPath(root),
			mapping: ".tu-agent/graph.db -> .tu-agent/graph/graph.db",
		},
		{
			oldPath: filepath.Join(tuAgentDir(root), "telemetry.jsonl"),
			newPath: telemetryPath(root),
			mapping: ".tu-agent/telemetry.jsonl -> .tu-agent/logs/telemetry.jsonl",
		},
		{
			oldPath: filepath.Join(tuAgentDir(root), "rules.md"),
			newPath: filepath.Join(tuAgentDir(root), "rules", "all.md"),
			mapping: ".tu-agent/rules.md -> .tu-agent/rules/all.md",
		},
		{
			oldGlob: filepath.Join(tuAgentDir(root), "memory", "chunks", "chunk-*.jsonl.gz"),
			newPath: memoryChunksDir(root),
			mapping: ".tu-agent/memory/chunks -> .tu-agent/share/memory/chunks",
		},
	}

	var violations []string
	for _, m := range markers {
		if !oldMarkerPresent(m.oldPath, m.oldGlob) {
			continue
		}
		if _, err := os.Stat(m.newPath); err == nil {
			continue // new counterpart present → migrated → not a violation
		}
		violations = append(violations, m.mapping)
	}

	if len(violations) == 0 {
		return nil
	}

	msg := "old layout detected — move these before continuing:\n  " +
		strings.Join(violations, "\n  ")
	return fmt.Errorf("oldLayoutGuard: %s", msg)
}

// oldMarkerPresent reports whether an old marker exists: either the exact file at
// oldPath, or — when oldGlob is set — at least one file matching the chunk glob.
func oldMarkerPresent(oldPath, oldGlob string) bool {
	if oldGlob != "" {
		matches, err := filepath.Glob(oldGlob)
		return err == nil && len(matches) > 0
	}
	_, err := os.Stat(oldPath)
	return err == nil
}
