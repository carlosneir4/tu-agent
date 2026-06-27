package crystallize

import (
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/memory"
)

func obs(topic, typ, title, content string, daysAgo int) memory.Observation {
	return memory.Observation{
		TopicKey:  topic,
		Type:      typ,
		Title:     title,
		Content:   content,
		UpdatedAt: time.Now().AddDate(0, 0, -daysAgo),
	}
}

func labels(cs []Cluster) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Label)
	}
	return out
}

func TestDetect_GroupsAcrossTypesByDomainToken(t *testing.T) {
	in := []memory.Observation{
		obs("testing/checkout-flow-pattern", "testing", "checkout flow", "cover the checkout order total branches", 1),
		obs("gotcha/checkout-null-cart", "gotcha", "checkout null cart", "checkout panics when cart is empty", 2),
		obs("decision/checkout-tax-rules", "decision", "checkout tax", "apply tax during checkout per region", 3),
		obs("reference/logging-setup", "reference", "logging", "configure the structured logger", 4),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "checkout" {
		t.Errorf("want label checkout, got %q", got[0].Label)
	}
	if got[0].Size != 3 {
		t.Errorf("want size 3, got %d", got[0].Size)
	}
	// crosses types
	types := map[string]bool{}
	for _, m := range got[0].Members {
		types[m.Type] = true
	}
	for _, want := range []string{"testing", "gotcha", "decision"} {
		if !types[want] {
			t.Errorf("cluster missing a %s member", want)
		}
	}
}

func TestDetect_BelowThresholdNotReturned(t *testing.T) {
	in := []memory.Observation{
		obs("testing/checkout-a", "testing", "checkout a", "checkout", 1),
		obs("gotcha/checkout-b", "gotcha", "checkout b", "checkout", 2),
	}
	if got := Detect(in, 3); len(got) != 0 {
		t.Errorf("want 0 clusters below threshold, got %v", labels(got))
	}
}

func TestDetect_RanksLargerFirstThenLabel(t *testing.T) {
	var in []memory.Observation
	for i := 0; i < 4; i++ {
		in = append(in, obs("testing/payment-"+string(rune('a'+i)), "testing", "payment", "payment gateway charge", i))
	}
	for i := 0; i < 3; i++ {
		in = append(in, obs("testing/auth-"+string(rune('a'+i)), "testing", "auth", "auth login session", i))
	}
	got := Detect(in, 3)
	if len(got) != 2 {
		t.Fatalf("want 2 clusters, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "payment" || got[1].Label != "auth" {
		t.Errorf("want [payment auth] (larger first), got %v", labels(got))
	}
}

func TestFormat_ListsClustersWithMembers(t *testing.T) {
	cs := []Cluster{{Label: "checkout", Size: 2, Members: []memory.Observation{
		{TopicKey: "testing/checkout-flow"},
		{TopicKey: "gotcha/checkout-null"},
	}}}
	out := Format(cs)
	for _, want := range []string{"checkout", "testing/checkout-flow", "gotcha/checkout-null", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("Format output missing %q:\n%s", want, out)
		}
	}
	if empty := Format(nil); !strings.Contains(empty, "no crystallizable clusters") {
		t.Errorf("empty Format should explain none found, got %q", empty)
	}
}

func TestDetect_SlugTokenWinsOnDFTie(t *testing.T) {
	// All three notes share slug token "zebra" and content tokens "alpha" and
	// "shared" — every shared token has DF=3. The slug-derived token must win
	// the tie (token order is slug -> title -> content), so the cluster label
	// is the slug token, never an incidental content word.
	in := []memory.Observation{
		obs("testing/zebra-one", "testing", "", "alpha shared", 1),
		obs("gotcha/zebra-two", "gotcha", "", "alpha shared", 2),
		obs("decision/zebra-three", "decision", "", "alpha shared", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "zebra" {
		t.Errorf("want slug token zebra to win the DF tie, got %q", got[0].Label)
	}
}
