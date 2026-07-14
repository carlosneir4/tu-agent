package codegen_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func jf(pkg, class, path string) codegen.SourceUnit {
	return codegen.SourceUnit{Package: pkg, FQN: pkg + "." + class, Path: path}
}

func TestBuildDomainMap_PartitionsByDepth(t *testing.T) {
	files := []codegen.SourceUnit{
		jf("acme.cms.feed", "FeedA", "src/acme/cms/feed/FeedA.java"),
		jf("acme.cms.feed.util", "FeedB", "src/acme/cms/feed/util/FeedB.java"),
		jf("acme.cms.video", "VideoA", "src/acme/cms/video/VideoA.java"),
		jf("acme.cms.video", "VideoB", "src/acme/cms/video/VideoB.java"),
	}
	got := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MinFiles: 1, MaxFiles: 100})

	names := make([]string, len(got))
	for i, d := range got {
		names[i] = d.Name
	}
	want := []string{"feed", "video"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("domain names = %v, want %v", names, want)
	}
	for _, d := range got {
		if d.Name == "feed" && len(d.Files) != 2 {
			t.Errorf("feed domain has %d files, want 2", len(d.Files))
		}
		if d.Name == "video" && len(d.Files) != 2 {
			t.Errorf("video domain has %d files, want 2", len(d.Files))
		}
	}
}

func TestBuildDomainMap_Deterministic(t *testing.T) {
	files := []codegen.SourceUnit{
		jf("acme.cms.video", "V", "src/acme/cms/video/V.java"),
		jf("acme.cms.feed", "F", "src/acme/cms/feed/F.java"),
	}
	opts := codegen.DomainMapOptions{Depth: 1, MinFiles: 1, MaxFiles: 100}
	a := codegen.BuildDomainMap(files, nil, opts)
	b := codegen.BuildDomainMap(files, nil, opts)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("BuildDomainMap not deterministic:\n%v\n%v", a, b)
	}
}

func TestBuildDomainMap_MergesTinyDomainIntoMostCoupled(t *testing.T) {
	files := []codegen.SourceUnit{
		jf("acme.cms.feed", "F1", "src/acme/cms/feed/F1.java"),
		jf("acme.cms.feed", "F2", "src/acme/cms/feed/F2.java"),
		jf("acme.cms.video", "V1", "src/acme/cms/video/V1.java"),
		jf("acme.cms.video", "V2", "src/acme/cms/video/V2.java"),
		jf("acme.cms.tiny", "T1", "src/acme/cms/tiny/T1.java"), // 1 file -> must merge
	}
	// tiny is coupled to video (two edges) and not to feed.
	edges := []codegen.Edge{
		{From: "src/acme/cms/tiny/T1.java", To: "src/acme/cms/video/V1.java"},
		{From: "src/acme/cms/video/V2.java", To: "src/acme/cms/tiny/T1.java"},
	}
	got := codegen.BuildDomainMap(files, edges, codegen.DomainMapOptions{Depth: 1, MinFiles: 2, MaxFiles: 100})

	for _, d := range got {
		if d.Name == "tiny" {
			t.Fatalf("tiny domain should have been merged away, got %+v", d)
		}
		if d.Name == "video" {
			if len(d.Files) != 3 {
				t.Errorf("video should absorb tiny (3 files), got %d", len(d.Files))
			}
		}
	}
}

func TestBuildDomainMap_SplitsHugeDomainBySubPackage(t *testing.T) {
	var files []codegen.SourceUnit
	// 4 files in acme.cms.content.article, 4 in acme.cms.content.video,
	// 1 file in acme.cms.core to ensure common prefix is acme.cms.
	// At depth=1, content.* files are in domain "content", core file is in "core".
	// But we set MaxFiles=5 so content domain (8 files) should split.
	for i := 0; i < 4; i++ {
		files = append(files, jf("acme.cms.content.article", "A", "src/acme/cms/content/article/A"+string(rune('0'+i))+".java"))
		files = append(files, jf("acme.cms.content.video", "V", "src/acme/cms/content/video/V"+string(rune('0'+i))+".java"))
	}
	files = append(files, jf("acme.cms.core", "C", "src/acme/cms/core/C.java"))
	got := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MinFiles: 1, MaxFiles: 5})

	names := map[string]int{}
	for _, d := range got {
		names[d.Name] = len(d.Files)
	}
	// "content" may exist as a parent marker (Files==nil → len==0) but must not
	// survive as a leaf domain — that would mean it did not split.
	if names["content"] != 0 {
		t.Fatalf("content (8 files > MaxFiles 5) should have split into children, got names %v", names)
	}
	if names["content-article"] != 4 || names["content-video"] != 4 {
		t.Errorf("expected content-article=4 and content-video=4, got %v", names)
	}
	if names["core"] != 1 {
		t.Errorf("expected core=1, got %v", names)
	}
}

// mkSized builds SourceUnits for a single package where each path maps to a byte size.
func mkSized(pkg string, sizes map[string]int) []codegen.SourceUnit {
	var out []codegen.SourceUnit
	for path, size := range sizes {
		class := strings.TrimSuffix(filepath.Base(path), ".java")
		out = append(out, codegen.SourceUnit{
			Path:    path,
			Package: pkg,
			FQN:     pkg + "." + class,
			Size:    size,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func TestSplitLeafDomainByBytes(t *testing.T) {
	files := mkSized("com.acme.widget", map[string]int{
		"w/A.java": 4000, "w/B.java": 4000, "w/C.java": 4000, "w/D.java": 4000,
	})
	domains := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 9000})
	// A split now also emits a parent marker (Files==nil); filter to leaf domains.
	var leaves []codegen.Domain
	for _, d := range domains {
		if d.Files != nil {
			leaves = append(leaves, d)
		}
	}
	if len(leaves) != 2 {
		t.Fatalf("leaf domains = %d (%+v), want 2 batches", len(leaves), domains)
	}
	if !strings.HasSuffix(leaves[0].Name, "-1") || !strings.HasSuffix(leaves[1].Name, "-2") {
		t.Errorf("batch names = %q, %q", leaves[0].Name, leaves[1].Name)
	}
	for _, d := range leaves {
		var sum int
		for range d.Files {
			sum += 4000
		}
		if sum > 9000 {
			t.Errorf("batch %s exceeds MaxBytes: %d", d.Name, sum)
		}
	}
}

func TestSplitPrefersSubpackagesOverBatches(t *testing.T) {
	// Files across two sub-packages: com.acme.api.view and com.acme.api.model.
	// At Depth=1 the common prefix is com.acme.api, so each sub-package becomes
	// its own domain (view / model) with 12000 bytes each — under MaxBytes=13000.
	// The code must NOT produce byte-batched names (e.g. "view-1", "model-1").
	files := append(
		mkSized("com.acme.api.view", map[string]int{"v/A.java": 6000, "v/B.java": 6000}),
		mkSized("com.acme.api.model", map[string]int{"m/C.java": 6000, "m/D.java": 6000})...,
	)
	domains := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 13000})
	for _, d := range domains {
		if strings.HasSuffix(d.Name, "-1") || strings.HasSuffix(d.Name, "-2") {
			t.Errorf("byte-batched domain name %q: sub-package split should have been preferred", d.Name)
		}
	}
	// Exactly 2 domains: view and model.
	if len(domains) != 2 {
		t.Errorf("expected 2 domains (view, model), got %+v", domains)
	}
}

func TestSingleOversizedFileGetsOwnBatch(t *testing.T) {
	files := mkSized("com.acme.widget", map[string]int{
		"w/Huge.java": 50000, "w/Tiny.java": 100,
	})
	domains := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 9000})
	var leaves []codegen.Domain
	for _, d := range domains {
		if d.Files != nil {
			leaves = append(leaves, d)
		}
	}
	if len(leaves) != 2 {
		t.Fatalf("leaf domains = %+v, want 2 (oversized file isolated)", domains)
	}
}

func TestMaxBytesZeroDisablesByteSplit(t *testing.T) {
	files := mkSized("com.acme.widget", map[string]int{"w/A.java": 90000, "w/B.java": 90000})
	domains := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 0})
	if len(domains) != 1 {
		t.Errorf("MaxBytes=0 must not split: %+v", domains)
	}
}

func TestBudgetSplitIsDeterministic(t *testing.T) {
	files := mkSized("com.acme.widget", map[string]int{
		"w/A.java": 3000, "w/B.java": 5000, "w/C.java": 2000, "w/D.java": 7000, "w/E.java": 1000,
	})
	a := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 8000})
	b := codegen.BuildDomainMap(files, nil, codegen.DomainMapOptions{Depth: 1, MaxBytes: 8000})
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic split:\n%+v\nvs\n%+v", a, b)
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"internal/memory/store_test.go", true},
		{"internal/memory/store.go", false},
		{"src/main/java/acme/InvoiceServiceTest.java", true},
		{"src/main/java/acme/InvoiceServiceTests.java", true},
		{"src/main/java/acme/Latest.java", false}, // suffix trap: lowercase "test.java"
		{"app/test_routes.py", true},
		{"app/routes_test.py", true},
		{"app/routes.py", false},
		{"web/cart.test.ts", true},
		{"web/cart.spec.ts", true},
		{"web/cart.ts", false},
	}
	for _, tt := range tests {
		if got := codegen.IsTestFile(tt.path); got != tt.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestMergeGuardrails(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "cmd/app/main.go", Package: "cmd/app", Size: 100},
		{Path: "cmd/app/root.go", Package: "cmd/app", Size: 100},
		{Path: "cmd/app/run.go", Package: "cmd/app", Size: 100},
		{Path: "cmd/app/serve.go", Package: "cmd/app", Size: 100},
		{Path: "cmd/app/init.go", Package: "cmd/app", Size: 100},
		{Path: "internal/memory/store.go", Package: "internal/memory", Size: 100},
		{Path: "internal/memory/store_test.go", Package: "internal/memory", Size: 100},
		{Path: "internal/skill/skill.go", Package: "internal/skill", Size: 100},
		{Path: "internal/skill/scanner.go", Package: "internal/skill", Size: 100},
		{Path: "internal/tool/tool.go", Package: "internal/tool", Size: 100},
		{Path: "internal/tool/memory.go", Package: "internal/tool", Size: 100},
		{Path: "internal/tool/bash.go", Package: "internal/tool", Size: 100},
		{Path: "internal/tool/grep.go", Package: "internal/tool", Size: 100},
		{Path: "internal/tool/find.go", Package: "internal/tool", Size: 100},
	}
	edges := []codegen.Edge{
		{From: "cmd/app/main.go", To: "internal/memory/store.go"},
		{From: "cmd/app/main.go", To: "internal/skill/skill.go"},
		{From: "cmd/app/main.go", To: "internal/tool/tool.go"},
		{From: "internal/tool/memory.go", To: "internal/memory/store.go"},
	}
	domains := codegen.BuildDomainMap(units, edges, codegen.DomainMapOptions{
		Depth: 2, MinFiles: 5, MinStandaloneFiles: 4,
	})

	byName := map[string]codegen.Domain{}
	for _, d := range domains {
		byName[d.Name] = d
	}
	cmdDomain, ok := byName["cmd-app"]
	if !ok {
		t.Fatalf("expected cmd-app domain, got %v", domainNames(domains))
	}
	for _, f := range cmdDomain.Files {
		if strings.HasPrefix(f, "internal/") {
			t.Errorf("cross-tree merge: %s landed in cmd/app", f)
		}
	}
	// internal/memory is tiny (1 non-test file) and coupled to internal/tool
	// inside the same subtree: merging there is allowed and expected.
	if _, exists := byName["internal-memory"]; exists {
		t.Errorf("internal/memory should have merged into its coupled sibling internal/tool")
	}
	tool := byName["internal-tool"]
	if !containsFile(tool.Files, "internal/memory/store.go") {
		t.Errorf("internal/memory files should be in internal/tool, got %v", tool.Files)
	}
}

func TestMinStandaloneBlocksMerge(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "internal/auth/a.go", Package: "internal/auth", Size: 1},
		{Path: "internal/auth/b.go", Package: "internal/auth", Size: 1},
		{Path: "internal/auth/c.go", Package: "internal/auth", Size: 1},
		{Path: "internal/auth/d.go", Package: "internal/auth", Size: 1},
		{Path: "internal/billing/a.go", Package: "internal/billing", Size: 1},
		{Path: "internal/billing/b.go", Package: "internal/billing", Size: 1},
		{Path: "internal/billing/c.go", Package: "internal/billing", Size: 1},
		{Path: "internal/billing/d.go", Package: "internal/billing", Size: 1},
		{Path: "internal/billing/e.go", Package: "internal/billing", Size: 1},
	}
	edges := []codegen.Edge{{From: "internal/billing/a.go", To: "internal/auth/a.go"}}
	domains := codegen.BuildDomainMap(units, edges, codegen.DomainMapOptions{
		Depth: 2, MinFiles: 5, MinStandaloneFiles: 4,
	})
	if len(domains) != 2 {
		t.Fatalf("want 2 standalone domains, got %v", domainNames(domains))
	}
}

// The non-clustered BuildDomainMap must NOT drop a tiny domain just because it
// lacks import coupling: it only ever sees import edges, so "uncoupled" is not a
// safe signal for "noise". A genuinely-disconnected stray file is dropped only
// in the clustered path, which has full-degree (call/extends) information.
func TestBuildDomainMap_DoesNotDropUncoupledDomain(t *testing.T) {
	files := []codegen.SourceUnit{
		{Path: "internal/a/a1.go", Package: "internal/a", Size: 1},
		{Path: "internal/a/a2.go", Package: "internal/a", Size: 1},
		{Path: "internal/b/b1.go", Package: "internal/b", Size: 1},
		{Path: "internal/b/b2.go", Package: "internal/b", Size: 1},
		// A single file with no import edge — looks "uncoupled" but must be kept
		// (it may well be call/extends-related, which BuildDomainMap can't see).
		{Path: "lone/Lone.java", Package: "lone", Size: 1},
	}
	edges := []codegen.Edge{
		{From: "internal/a/a1.go", To: "internal/b/b1.go"},
	}
	got := codegen.BuildDomainMap(files, edges, codegen.DomainMapOptions{Depth: 1, MinFiles: 2, MaxFiles: 100})

	if !containsFile(domainFiles(got), "lone/Lone.java") {
		t.Errorf("uncoupled single-file domain must be kept by BuildDomainMap; domains: %v", domainNames(got))
	}
}

// domainFiles flattens every domain's files into one slice.
func domainFiles(ds []codegen.Domain) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Files...)
	}
	return out
}

// Regression: a small repo whose tiny domains have no IMPORT edges between them
// (e.g. same-/sibling-package Java related by call/extends, which produce no
// import edges) must NOT be dropped to nothing. The non-clustered BuildDomainMap
// only receives import edges, so it must never drop a domain merely for lacking
// import coupling — doing so emptied the whole concept index on import-light repos.
func TestBuildDomainMap_KeepsUncoupledSmallDomains(t *testing.T) {
	files := []codegen.SourceUnit{
		{Path: "src/com/acme/orders/OrderService.java", Package: "com.acme.orders", Size: 1},
		{Path: "src/com/acme/orders/BaseService.java", Package: "com.acme.orders", Size: 1},
		{Path: "src/com/acme/catalog/CatalogRepository.java", Package: "com.acme.catalog", Size: 1},
	}
	var edges []codegen.Edge // no import edges between the domains
	got := codegen.BuildDomainMap(files, edges, codegen.DomainMapOptions{
		Depth: 1, MinFiles: 5, MinStandaloneFiles: 4, MaxFiles: 100,
	})

	if len(got) == 0 {
		t.Fatalf("uncoupled small domains must not all be dropped; got 0 domains")
	}
	total := 0
	for _, d := range got {
		total += len(d.Files)
	}
	if total != 3 {
		t.Errorf("all 3 files must remain represented across domains; got %d (%v)", total, domainNames(got))
	}
}

// A tiny domain that IS coupled but cannot merge (its only coupling is to a
// different package subtree) is connected, not noise, and must be kept.
func TestBuildDomainMap_KeepsCoupledTinyDomain(t *testing.T) {
	files := []codegen.SourceUnit{
		{Path: "com/a/a1.go", Package: "com/a", Size: 1},
		{Path: "com/a/a2.go", Package: "com/a", Size: 1},
		// 1 file, tiny; coupled to com/a but in a different top-level subtree.
		{Path: "org/b/b1.go", Package: "org/b", Size: 1},
	}
	edges := []codegen.Edge{
		{From: "org/b/b1.go", To: "com/a/a1.go"},
	}
	got := codegen.BuildDomainMap(files, edges, codegen.DomainMapOptions{Depth: 1, MinFiles: 2, MaxFiles: 100})

	var foundB bool
	for _, d := range got {
		if containsFile(d.Files, "org/b/b1.go") {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("coupled tiny domain org/b should be kept, not dropped; domains: %v", domainNames(got))
	}
}

func domainNames(ds []codegen.Domain) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

func containsFile(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

func TestSplitProducesHierarchy(t *testing.T) {
	var units []codegen.SourceUnit
	for i := 0; i < 6; i++ {
		units = append(units, codegen.SourceUnit{
			Path:    fmt.Sprintf("svc/billing/invoice/f%d.java", i),
			Package: "svc.billing.invoice", Size: 10,
		})
	}
	for i := 0; i < 6; i++ {
		units = append(units, codegen.SourceUnit{
			Path:    fmt.Sprintf("svc/billing/tax/f%d.java", i),
			Package: "svc.billing.tax", Size: 10,
		})
	}
	units = append(units,
		codegen.SourceUnit{Path: "svc/auth/A.java", Package: "svc.auth", Size: 10},
		codegen.SourceUnit{Path: "svc/auth/B.java", Package: "svc.auth", Size: 10},
	)
	domains := codegen.BuildDomainMap(units, nil, codegen.DomainMapOptions{
		Depth: 1, MaxFiles: 8, // 12 files in "billing" force a split
	})

	var parent *codegen.Domain
	var children []codegen.Domain
	for i := range domains {
		switch {
		case domains[i].Name == "billing" && domains[i].Files == nil:
			parent = &domains[i]
		case domains[i].Parent == "billing":
			children = append(children, domains[i])
		}
	}
	if parent == nil {
		t.Fatalf("expected parent marker domain 'billing' with nil Files, got %v", domainNames(domains))
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children of billing, got %v", domainNames(domains))
	}
	for _, c := range children {
		if len(c.Files) == 0 {
			t.Errorf("child %s has no files", c.Name)
		}
	}
}

func TestNoSplitNoParent(t *testing.T) {
	units := []codegen.SourceUnit{
		{Path: "svc/auth/a.java", Package: "svc.auth", Size: 10},
		{Path: "svc/auth/b.java", Package: "svc.auth", Size: 10},
	}
	domains := codegen.BuildDomainMap(units, nil, codegen.DomainMapOptions{Depth: 1, MaxFiles: 40})
	for _, d := range domains {
		if d.Parent != "" || d.Files == nil {
			t.Errorf("unexpected hierarchy fields on %s: parent=%q filesNil=%v", d.Name, d.Parent, d.Files == nil)
		}
	}
}
