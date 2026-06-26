package tdd

import (
	"reflect"
	"testing"
)

const sampleFeature = `Feature: count command

  @s1
  Scenario: empty file prints 0
    Given an empty notes file
    When I run count
    Then it prints 0

  @s2 @wip
  Scenario: three notes prints 3
    Given three notes
    When I run count
    Then it prints 3
`

func TestScenarioTags(t *testing.T) {
	got := ScenarioTags(sampleFeature)
	want := []string{"@s1", "@s2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

func TestParseFeature(t *testing.T) {
	got := ParseFeature(sampleFeature)
	want := []Scenario{
		{Tag: "@s1", Title: "empty file prints 0"},
		{Tag: "@s2", Title: "three notes prints 3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scenarios = %v, want %v", got, want)
	}
}

func TestScenarioTagsNone(t *testing.T) {
	if got := ScenarioTags("Feature: x\n  Scenario: untagged\n"); len(got) != 0 {
		t.Fatalf("tags = %v, want empty", got)
	}
}
