package codegen_test

import (
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/codegen"
)

func domainContextFixture() (codegen.Domain, []codegen.SourceUnit, []codegen.Edge) {
	d := codegen.Domain{
		Name:    "feed",
		Package: "acme.feed",
		Files:   []string{"src/feed/FeedService.java", "src/feed/FeedParser.java"},
	}
	files := []codegen.SourceUnit{
		{Path: "src/feed/FeedService.java", Package: "acme.feed", FQN: "acme.feed.FeedService"},
		{Path: "src/feed/FeedParser.java", Package: "acme.feed", FQN: "acme.feed.FeedParser"},
		{Path: "src/gw/GatewayController.java", Package: "acme.gw", FQN: "acme.gw.GatewayController"},
		{Path: "src/billing/BillingClient.java", Package: "acme.billing", FQN: "acme.billing.BillingClient"},
	}
	edges := []codegen.Edge{
		{From: "src/gw/GatewayController.java", To: "src/feed/FeedService.java"},  // inbound
		{From: "src/feed/FeedService.java", To: "src/billing/BillingClient.java"}, // outbound
		{From: "src/feed/FeedService.java", To: "src/feed/FeedParser.java"},       // internal: excluded
	}
	return d, files, edges
}

func TestBuildDomainContext_ClassifiesEdges(t *testing.T) {
	d, files, edges := domainContextFixture()
	got := codegen.BuildDomainContext(d, files, edges)

	if !strings.Contains(got, "acme.gw.GatewayController -> acme.feed.FeedService") {
		t.Errorf("missing inbound edge, got:\n%s", got)
	}
	if !strings.Contains(got, "acme.billing.BillingClient") {
		t.Errorf("missing outbound dependency, got:\n%s", got)
	}
	// The internal edge must not appear as inbound or outbound.
	if strings.Contains(got, "acme.feed.FeedService -> acme.feed.FeedParser") {
		t.Errorf("internal edge leaked into structural context:\n%s", got)
	}
	// File list carries packages.
	if !strings.Contains(got, "src/feed/FeedParser.java (acme.feed)") {
		t.Errorf("missing file-with-package entry, got:\n%s", got)
	}
}

func TestBuildDomainContext_Deterministic(t *testing.T) {
	d, files, edges := domainContextFixture()
	a := codegen.BuildDomainContext(d, files, edges)
	b := codegen.BuildDomainContext(d, files, edges)
	if a != b {
		t.Error("BuildDomainContext is not deterministic for identical input")
	}
}

func TestBuildDomainContext_EmptyEdges(t *testing.T) {
	d, files, _ := domainContextFixture()
	got := codegen.BuildDomainContext(d, files, nil)
	if !strings.Contains(got, "(none)") {
		t.Errorf("empty edge sets should render '(none)', got:\n%s", got)
	}
}
