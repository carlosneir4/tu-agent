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
