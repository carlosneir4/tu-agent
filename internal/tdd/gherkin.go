package tdd

import (
	"bufio"
	"regexp"
	"strings"
)

// Scenario is one tagged Gherkin scenario: its @sN tag and title.
type Scenario struct {
	Tag   string
	Title string
}

var sceneTagRe = regexp.MustCompile(`@s\d+`)

// ScenarioTags returns the ordered @sN tags found in a .feature source.
func ScenarioTags(src string) []string {
	tags := make([]string, 0)
	for _, s := range ParseFeature(src) {
		tags = append(tags, s.Tag)
	}
	return tags
}

// ParseFeature pairs each @sN tag with the title of the following
// "Scenario:" line. Tags without a following scenario, and scenarios without
// an @sN tag, are skipped.
func ParseFeature(src string) []Scenario {
	out := make([]Scenario, 0)
	pending := ""
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := sceneTagRe.FindString(line); m != "" && strings.HasPrefix(line, "@") {
			pending = m
			continue
		}
		if strings.HasPrefix(line, "Scenario:") {
			if pending != "" {
				title := strings.TrimSpace(strings.TrimPrefix(line, "Scenario:"))
				out = append(out, Scenario{Tag: pending, Title: title})
				pending = ""
			}
		}
	}
	return out
}
