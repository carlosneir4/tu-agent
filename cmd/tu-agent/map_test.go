package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func progressChdir(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func writeMapFixture(t *testing.T) {
	t.Helper()
	files := map[string]string{
		"src/com/acme/billing/InvoiceService.java": "package com.acme.billing;\nimport com.acme.core.BaseService;\npublic class InvoiceService extends BaseService {}\n",
		"src/com/acme/billing/CreditNote.java":     "package com.acme.billing;\npublic class CreditNote {}\n",
		"src/com/acme/core/BaseService.java":       "package com.acme.core;\npublic class BaseService {}\n",
		"src/com/acme/core/Helper.java":            "package com.acme.core;\npublic class Helper {}\n",
	}
	for rel, content := range files {
		if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rel, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunMapJSON(t *testing.T) {
	progressChdir(t)
	writeMapFixture(t)

	out, err := runMap("src", 1, 2, 0, 0, 0, "", true)
	if err != nil {
		t.Fatalf("runMap: %v", err)
	}
	var got mapOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Domains) != 2 {
		t.Fatalf("domains = %d, want 2 (billing, core): %+v", len(got.Domains), got.Domains)
	}
	byName := map[string]mapDomain{}
	for _, d := range got.Domains {
		byName[d.Name] = d
	}
	billing, ok := byName["billing"]
	if !ok {
		t.Fatalf("billing domain missing: %+v", got.Domains)
	}
	if len(billing.Files) != 2 {
		t.Errorf("billing files = %v, want 2 entries", billing.Files)
	}
	if billing.Package != "com.acme.billing" {
		t.Errorf("billing package = %q", billing.Package)
	}
	// billing imports core, so its structural context must not be empty.
	if strings.TrimSpace(billing.Context) == "" {
		t.Errorf("billing structural context is empty")
	}
	// core is imported by billing; BuildDomainContext is called for every domain.
	core, ok := byName["core"]
	if !ok {
		t.Fatalf("core domain missing: %+v", got.Domains)
	}
	if strings.TrimSpace(core.Context) == "" {
		t.Errorf("core structural context is empty")
	}
}

func TestRunMapHumanReadable(t *testing.T) {
	progressChdir(t)
	writeMapFixture(t)

	out, err := runMap("src", 1, 2, 0, 0, 0, "", false)
	if err != nil {
		t.Fatalf("runMap: %v", err)
	}
	for _, want := range []string{"billing", "core", "(2 files)"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
}

func TestRunMapNoSourcesErrors(t *testing.T) {
	progressChdir(t)
	if _, err := runMap("", 1, 2, 0, 0, 0, "", true); err == nil {
		t.Fatal("expected error when no source files exist")
	}
}

func TestRunMapAbsolutePath(t *testing.T) {
	progressChdir(t)
	writeMapFixture(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absPath := filepath.Join(cwd, "src")

	out, err := runMap(absPath, 1, 2, 0, 0, 0, "", true)
	if err != nil {
		t.Fatalf("runMap with absolute path: %v", err)
	}
	var got mapOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Domains) != 2 {
		t.Errorf("domains = %d, want 2 (billing, core)", len(got.Domains))
	}
}

func TestMapPlan(t *testing.T) {
	progressChdir(t)
	for i := 0; i < 3; i++ {
		p := filepath.Join("svc", fmt.Sprintf("f%d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package svc\n\nfunc F() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runMapPlan("", 1, 5, 0, 0, 0, "")
	if err != nil {
		t.Fatalf("runMapPlan: %v", err)
	}
	for _, want := range []string{"top-level domains:", "LLM calls:", "estimated input tokens:"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q:\n%s", want, out)
		}
	}
}

func TestMapJSONIncludesParent(t *testing.T) {
	progressChdir(t)
	writeJava := func(rel, pkg, class string) {
		t.Helper()
		p := filepath.Join(rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		src := fmt.Sprintf("package %s;\n\npublic class %s {}\n", pkg, class)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		writeJava(fmt.Sprintf("svc/billing/invoice/Inv%d.java", i), "svc.billing.invoice", fmt.Sprintf("Inv%d", i))
		writeJava(fmt.Sprintf("svc/billing/tax/Tax%d.java", i), "svc.billing.tax", fmt.Sprintf("Tax%d", i))
	}
	writeJava("svc/auth/A.java", "svc.auth", "A")
	writeJava("svc/auth/B.java", "svc.auth", "B")

	// depth=1, minFiles=5, maxFiles=8 forces billing (12 files) to split
	out, err := runMap("", 1, 5, 8, 0, 0, "", true)
	if err != nil {
		t.Fatalf("runMap: %v", err)
	}
	var doc struct {
		Domains []struct {
			Name   string   `json:"name"`
			Parent string   `json:"parent"`
			Files  []string `json:"files"`
		} `json:"domains"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var parents, children int
	for _, d := range doc.Domains {
		if d.Parent != "" {
			children++
		}
		if d.Files == nil && d.Parent == "" {
			parents++
		}
	}
	if parents == 0 || children == 0 {
		t.Errorf("expected hierarchy in JSON output, got parents=%d children=%d:\n%s", parents, children, out)
	}
}
