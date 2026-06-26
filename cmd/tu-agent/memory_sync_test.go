package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/memory"
)

func TestMemoryExportImportCLI(t *testing.T) {
	// Author A records a decision and exports it to a chunk directory.
	chunks := t.TempDir()

	srcDB := filepath.Join(t.TempDir(), "memory.db")
	s, err := memory.Open(srcDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/cache", "use redis", memory.UpsertOpts{Author: "alice"}); err != nil {
		t.Fatal(err)
	}
	recs, err := s.ExportRecords("alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := memory.WriteChunk(chunks, "alice", recs); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Author B imports the same chunk dir into a fresh DB via ReadAllChunks.
	back, err := memory.ReadAllChunks(chunks)
	if err != nil {
		t.Fatal(err)
	}
	dstDB := filepath.Join(t.TempDir(), "memory.db")
	d, err := memory.Open(dstDB)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	res, err := d.ImportRecords(back)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 {
		t.Fatalf("want 1 inserted, got %+v", res)
	}
	got, _ := d.Search("redis", "")
	if len(got) != 1 {
		t.Fatalf("imported decision not searchable: %+v", got)
	}
}

func TestMemoryChunksDirPath(t *testing.T) {
	got := memoryChunksDir("/repo")
	want := filepath.Join("/repo", ".tu-agent", "memory", "chunks")
	if got != want {
		t.Fatalf("memoryChunksDir = %q, want %q", got, want)
	}
}

func TestMemoryImportQuiet(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	memImportQuiet = true
	t.Cleanup(func() { memImportQuiet = false })

	var out bytes.Buffer
	memoryImportCmd.SetOut(&out)
	if err := memoryImportCmd.RunE(memoryImportCmd, nil); err != nil {
		t.Fatalf("import --quiet: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("quiet import must print nothing, got %q", out.String())
	}
}

func TestMemoryExportQuiet(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	memExportQuiet = true
	t.Cleanup(func() { memExportQuiet = false })

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export --quiet: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("quiet export must print nothing, got %q", out.String())
	}
}

func TestMemoryExportCmdRun(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(memoryDBPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert("decision/x", "body", memory.UpsertOpts{Author: "tester"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	memoryExportCmd.SetOut(&out)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("export run: %v", err)
	}
	entries, _ := os.ReadDir(memoryChunksDir("."))
	if len(entries) != 1 {
		t.Fatalf("export must write one chunk file, got %d", len(entries))
	}

	// Second export with no changes must take the "up to date" branch and not error.
	var out2 bytes.Buffer
	memoryExportCmd.SetOut(&out2)
	if err := memoryExportCmd.RunE(memoryExportCmd, nil); err != nil {
		t.Fatalf("second export run: %v", err)
	}
	if got := out2.String(); !strings.Contains(got, "up to date") {
		t.Fatalf("second export: want 'up to date' message, got %q", got)
	}
	entries2, _ := os.ReadDir(memoryChunksDir("."))
	if len(entries2) != 1 {
		t.Fatalf("second export must not create extra files, got %d", len(entries2))
	}
}
