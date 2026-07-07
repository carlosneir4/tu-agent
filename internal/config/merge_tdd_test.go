package config

import "testing"

func TestMergeInto_ConceptRoots(t *testing.T) {
	dst := Config{}
	src := Config{}
	src.Learn.ConceptRoots = []string{"packages", "rigs"}
	mergeInto(&dst, src)
	if len(dst.Learn.ConceptRoots) != 2 || dst.Learn.ConceptRoots[0] != "packages" {
		t.Fatalf("concept_roots not merged: %+v", dst.Learn.ConceptRoots)
	}
}

func TestMergeIntoTdd(t *testing.T) {
	dst := defaultConfig()
	tr := true
	mergeInto(&dst, Config{Tdd: TddConfig{
		TestCommand:       "./gradlew test",
		Mutation:          true,
		MutationThreshold: 0.8,
		Archive:           &tr,
	}})
	if dst.Tdd.TestCommand != "./gradlew test" {
		t.Fatalf("test_command not merged: %q", dst.Tdd.TestCommand)
	}
	if !dst.Tdd.Mutation {
		t.Fatalf("mutation not merged")
	}
	if dst.Tdd.MutationThreshold != 0.8 {
		t.Fatalf("threshold not merged: %v", dst.Tdd.MutationThreshold)
	}
	if dst.Tdd.Archive == nil || *dst.Tdd.Archive != true {
		t.Fatalf("archive not merged")
	}
}

func TestMergeIntoTddStrict(t *testing.T) {
	base := defaultConfig()
	proj := Config{Tdd: TddConfig{Strict: true}}
	mergeInto(&base, proj)
	if !base.Tdd.Strict {
		t.Fatal("Tdd.Strict dropped by mergeInto")
	}
}

func TestMergeIntoRoutingDisabled(t *testing.T) {
	base := defaultConfig()
	mergeInto(&base, Config{Routing: RoutingConfig{Disabled: true}})
	if !base.Routing.Disabled {
		t.Fatal("Routing.Disabled not merged")
	}
}

func TestMergeIntoRoutingDisabled_Sticky(t *testing.T) {
	base := defaultConfig()
	mergeInto(&base, Config{Routing: RoutingConfig{Disabled: true}})
	// A later layer without the field must not un-set it.
	mergeInto(&base, Config{Routing: RoutingConfig{Default: "local"}})
	if !base.Routing.Disabled {
		t.Fatal("Routing.Disabled was reset by a later layer without the field — kill-switch must be sticky")
	}
}
