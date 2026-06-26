package tdd

import "testing"

func TestPlanFeatures(t *testing.T) {
	// Explicit list wins.
	c := Contract{Features: []FeaturePlan{{Name: "a", Scenarios: []string{"@s1"}}, {Name: "b"}}}
	got := planFeatures(c)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("explicit features = %+v", got)
	}
	// Legacy handoff is synthesized into a single plan.
	c2 := Contract{Handoff: "count", Scenarios: []string{"@s1", "@s2"}}
	got2 := planFeatures(c2)
	if len(got2) != 1 || got2[0].Name != "count" || len(got2[0].Scenarios) != 2 {
		t.Fatalf("legacy handoff = %+v", got2)
	}
	// Nothing to do.
	if got3 := planFeatures(Contract{}); len(got3) != 0 {
		t.Fatalf("empty = %+v", got3)
	}
}

func TestParseContract(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantStatus string
		wantErr    bool
	}{
		{
			name: "well-formed block",
			in: "Here is my work.\n\n```json\n" +
				`{"stage":"architect","status":"pass","complexity":"standard","handoff":"see spec"}` +
				"\n```\n",
			wantStatus: "pass",
		},
		{
			name: "last block wins",
			in: "```json\n{\"stage\":\"a\",\"status\":\"fail\"}\n```\n" +
				"```json\n{\"stage\":\"b\",\"status\":\"pass\"}\n```",
			wantStatus: "pass",
		},
		{name: "no block", in: "just prose, no contract", wantErr: true},
		{name: "malformed json", in: "```json\n{not json}\n```", wantErr: true},
		{name: "missing status", in: "```json\n{\"stage\":\"x\"}\n```", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseContract(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && got.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}
