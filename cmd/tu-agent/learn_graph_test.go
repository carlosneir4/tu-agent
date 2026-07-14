package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func writeJavaFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"src/com/acme/core/BaseService.java":       "package com.acme.core;\npublic class BaseService {}\n",
		"src/com/acme/billing/InvoiceService.java": "package com.acme.billing;\nimport com.acme.core.BaseService;\npublic class InvoiceService extends BaseService {}\n",
		"src/com/acme/billing/Ledger.java":         "package com.acme.billing;\npublic class Ledger {}\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreFedDomains(t *testing.T) {
	root := t.TempDir()
	writeJavaFixture(t, root)
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := runGraphBuild(""); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}
	s, err := openGraphStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	units, edges, weighted, err := loadSourceUnits(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 3 {
		t.Fatalf("units = %d, want 3", len(units))
	}
	var pairWeight int
	for _, e := range weighted {
		if e.From == "src/com/acme/billing/InvoiceService.java" && e.To == "src/com/acme/core/BaseService.java" {
			pairWeight = e.Weight
		}
	}
	if pairWeight < 2 {
		t.Errorf("InvoiceService->BaseService weight = %d, want >= 2 (imports + extends)", pairWeight)
	}
	domains := codegen.BuildDomainMap(units, edges, codegen.DomainMapOptions{Depth: 1, MinFiles: 1})
	if len(domains) != 2 {
		t.Errorf("domains = %d, want 2 (com.acme.core, com.acme.billing)", len(domains))
	}
}

func TestBuildDomainsSelectsStrategy(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "a.java", Package: "com.acme.alpha"},
		{Path: "b.java", Package: "com.acme.beta"},
	}
	opts := codegen.DomainMapOptions{Depth: 1, MinFiles: 1}

	for _, mode := range []string{"", "leiden", "heuristic"} {
		if _, err := buildDomains(units, nil, nil, opts, mode); err != nil {
			t.Errorf("mode %q: unexpected error %v", mode, err)
		}
	}
	if _, err := buildDomains(units, nil, nil, opts, "bogus"); err == nil {
		t.Error("mode bogus: expected error, got nil")
	}
}
