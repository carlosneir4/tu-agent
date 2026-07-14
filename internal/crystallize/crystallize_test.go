package crystallize

import (
	"strings"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/memory"
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
	// NEW cohesion behavior: notes cluster only when they share MORE than one
	// specific token. These three notes share two specific tokens (checkout,
	// order) across three types, so they form one cross-type cluster; the label
	// is the alphabetically-first most-common specific token. The logging note
	// shares nothing specific and stays out.
	in := []memory.Observation{
		obs("testing/checkout-order", "testing", "checkout order", "checkout order flow", 1),
		obs("gotcha/checkout-order", "gotcha", "checkout order", "checkout order flow", 2),
		obs("decision/checkout-order", "decision", "checkout order", "checkout order flow", 3),
		obs("reference/logging-setup", "reference", "logging", "configure the structured logger", 4),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "checkout" {
		t.Errorf("want label checkout (alphabetically-first shared token), got %q", got[0].Label)
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
	for i := range 4 {
		in = append(in, obs("testing/payment-"+string(rune('a'+i)), "testing", "payment", "payment gateway charge", i))
	}
	for i := range 3 {
		in = append(in, obs("testing/auth-"+string(rune('a'+i)), "testing", "auth", "auth login session", i))
	}
	got := Detect(in, 3)
	if len(got) != 2 {
		t.Fatalf("want 2 clusters, got %d: %v", len(got), labels(got))
	}
	// Larger cluster ranks first (size desc). Its label is the alphabetical
	// winner of the payment/gateway/charge tie (all df=4) → "charge", per the
	// retired slug-priority rule being replaced by alphabetical tie-break.
	if got[0].Label != "charge" || got[1].Label != "auth" {
		t.Errorf("want [charge auth] (larger first, alphabetical tie-break), got %v", labels(got))
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

func TestDetect_LabelBreaksDFTieAlphabetically(t *testing.T) {
	// NEW behavior: on a document-frequency tie the label is the
	// alphabetically-first specific token; the old slug-priority rule is gone.
	// All three notes share {zebra, alpha, shared}, each with DF=3, so the label
	// is "alpha" (a < s < z), not the slug token "zebra".
	in := []memory.Observation{
		obs("testing/zebra-one", "testing", "", "alpha shared", 1),
		obs("gotcha/zebra-two", "gotcha", "", "alpha shared", 2),
		obs("decision/zebra-three", "decision", "", "alpha shared", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "alpha" {
		t.Errorf("want alphabetically-first token alpha to win the DF tie, got %q", got[0].Label)
	}
}

func TestFormatWithStatus_ShowsMarkers(t *testing.T) {
	cs := []Cluster{
		{Label: "checkout", Size: 3, Members: []memory.Observation{{TopicKey: "testing/a"}}},
		{Label: "payment", Size: 4, Members: []memory.Observation{{TopicKey: "testing/b"}}},
	}
	st := map[string]SkillStatus{"checkout": StatusStale, "payment": StatusNone}
	out := FormatWithStatus(cs, st)
	if !strings.Contains(out, "[stale]") || !strings.Contains(out, "[none]") {
		t.Errorf("missing status markers:\n%s", out)
	}
}
