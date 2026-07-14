package crystallize

// Tests for the `label-hygiene` feature: two orthogonal tokenization fixes in
// domainTokens/stopTokens that do NOT exist yet. @s1, @s2 and @s4 must go RED
// against current code; @s3 and @s5 are guards that the fixes do not over-reach
// and may pass today. Generic com.acme content-management/migration corpus only.

import (
	"regexp"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/memory"
)

var allDigits = regexp.MustCompile(`^[0-9]+$`)

// sameCluster reports whether both topic keys land in the same returned cluster.
func sameCluster(cs []Cluster, topicA, topicB string) bool {
	for _, c := range cs {
		var a, b bool
		for _, m := range c.Members {
			if m.TopicKey == topicA {
				a = true
			}
			if m.TopicKey == topicB {
				b = true
			}
		}
		if a && b {
			return true
		}
	}
	return false
}

// @s1 — an all-digit token never becomes a cluster label.
// Three cohesive notes share {acme, content, migration, 4821} (each df=3). Under
// today's alphabetical tie-break "4821" sorts before any letter and wins the
// label. After the all-digit drop the label must be a non-numeric domain token.
func TestLabelHygiene_AllDigitTokenNeverLabels_s1(t *testing.T) {
	in := []memory.Observation{
		obs("decision/acme-migration", "decision", "content migration", "content migration 4821", 1),
		obs("gotcha/acme-migration", "gotcha", "content migration", "content migration 4821", 2),
		obs("reference/acme-migration", "reference", "content migration", "content migration 4821", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("@s1 want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if allDigits.MatchString(got[0].Label) {
		t.Errorf("@s1 label must not be an all-digit token, got %q", got[0].Label)
	}
	for _, c := range got {
		if allDigits.MatchString(c.Label) {
			t.Errorf("@s1 no cluster may be labeled by an all-digit string, got %q", c.Label)
		}
	}
}

// @s2 — a shared all-digit token does not by itself create an edge.
// Two otherwise-unrelated notes share ONLY the bare number "4821"
// (sets {alpha,beta,4821} and {gamma,delta,4821}; jaccard 1/5 = 0.2 >= the 0.125
// threshold today, so an edge forms). After the all-digit drop nothing is shared
// and the edge must disappear, so they must not co-cluster.
func TestLabelHygiene_AllDigitTokenMakesNoEdge_s2(t *testing.T) {
	const topicA = "decision/alpha-beta"
	const topicB = "gotcha/gamma-delta"
	in := []memory.Observation{
		obs(topicA, "decision", "alpha beta", "alpha beta 4821", 1),
		obs(topicB, "gotcha", "gamma delta", "gamma delta 4821", 2),
	}
	got := Detect(in, 2)
	if sameCluster(got, topicA, topicB) {
		t.Errorf("@s2 notes sharing only a bare number must not co-cluster; got %v", labels(got))
	}
}

// @s3 — a token that merely CONTAINS digits is retained (guard against overreach).
// The distinctive shared token "oauth2" is the alphabetically-first of the tied
// {oauth2, session, token} set, so it drives the label. It is not all-digit, so
// the fix must keep it and the cluster label must stay "oauth2". Passes today.
func TestLabelHygiene_ContainsDigitTokenRetained_s3(t *testing.T) {
	in := []memory.Observation{
		obs("decision/oauth2-session", "decision", "oauth2 session", "oauth2 session token", 1),
		obs("gotcha/oauth2-session", "gotcha", "oauth2 session", "oauth2 session token", 2),
		obs("reference/oauth2-session", "reference", "oauth2 session", "oauth2 session token", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("@s3 want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "oauth2" {
		t.Errorf("@s3 want label oauth2 (digit-containing token kept), got %q", got[0].Label)
	}
}

// @s4 — a newly-added workflow stopword never labels a cluster.
// Three cohesive notes share {feature, migration, publish} (each df=3). Today
// "feature" is the alphabetically-first tied token and wins the label. After
// "feature" joins the workflow stopword band it is dropped and the label must be
// a real domain token ("migration", the alphabetical winner of what remains).
func TestLabelHygiene_WorkflowStopwordNeverLabels_s4(t *testing.T) {
	in := []memory.Observation{
		obs("decision/publish-feature", "decision", "publish feature", "publish feature migration", 1),
		obs("gotcha/publish-feature", "gotcha", "publish feature", "publish feature migration", 2),
		obs("reference/publish-feature", "reference", "publish feature", "publish feature migration", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("@s4 want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label == "feature" {
		t.Errorf("@s4 stopword 'feature' must not label a cluster, got %q", got[0].Label)
	}
	if got[0].Label != "migration" {
		t.Errorf("@s4 want real domain token 'migration' as label, got %q", got[0].Label)
	}
	for _, c := range got {
		if c.Label == "feature" {
			t.Errorf("@s4 no cluster may be labeled 'feature', got %q", c.Label)
		}
	}
}

// @s5 — an intentionally-excluded ambiguous term stays eligible (guard).
// In a content-management theme "content" is the dominant specific token
// ({content, publish, workflow}, alphabetically first) and must remain a valid
// label. We deliberately did NOT add "content" to the stopword band. Passes today.
func TestLabelHygiene_AmbiguousContentStaysEligible_s5(t *testing.T) {
	in := []memory.Observation{
		obs("decision/content-publish", "decision", "content publish", "content publish workflow", 1),
		obs("gotcha/content-publish", "gotcha", "content publish", "content publish workflow", 2),
		obs("reference/content-publish", "reference", "content publish", "content publish workflow", 3),
	}
	got := Detect(in, 3)
	if len(got) != 1 {
		t.Fatalf("@s5 want 1 cluster, got %d: %v", len(got), labels(got))
	}
	if got[0].Label != "content" {
		t.Errorf("@s5 want 'content' to remain an eligible label, got %q", got[0].Label)
	}
}
