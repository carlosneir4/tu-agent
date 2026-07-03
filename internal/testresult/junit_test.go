package testresult

import (
	"strings"
	"testing"
)

const sampleJUnit = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="com.acme.FooTest" tests="3">
  <testcase classname="com.acme.FooTest" name="passes"/>
  <testcase classname="com.acme.FooTest" name="fails">
    <failure message="boom">expected true</failure>
  </testcase>
  <testcase classname="com.acme.FooTest" name="errors">
    <error message="npe">NullPointerException</error>
  </testcase>
</testsuite>`

func TestParseJUnitXML(t *testing.T) {
	rep, err := ParseJUnitXML(strings.NewReader(sampleJUnit))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rep.Cases) != 3 {
		t.Fatalf("cases = %d, want 3", len(rep.Cases))
	}
	want := map[string]Status{"passes": Pass, "fails": Fail, "errors": Error}
	for _, c := range rep.Cases {
		if c.Class != "com.acme.FooTest" {
			t.Errorf("class = %q", c.Class)
		}
		if want[c.Name] != c.Status {
			t.Errorf("%s status = %q, want %q", c.Name, c.Status, want[c.Name])
		}
	}
}

const sampleJUnitNestedSuites = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="com.acme.FooTest" tests="2">
    <testcase classname="com.acme.FooTest" name="testFooPass"/>
    <testcase classname="com.acme.FooTest" name="testFooFail">
      <failure message="assertion failed">expected 1, got 0</failure>
    </testcase>
  </testsuite>
  <testsuite name="com.acme.BarTest" tests="2">
    <testcase classname="com.acme.BarTest" name="testBarPass"/>
    <testcase classname="com.acme.BarTest" name="testBarError">
      <error message="npe">NullPointerException at line 42</error>
    </testcase>
  </testsuite>
</testsuites>`

func TestParseJUnitXMLNestedSuites(t *testing.T) {
	rep, err := ParseJUnitXML(strings.NewReader(sampleJUnitNestedSuites))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rep.Cases) != 4 {
		t.Fatalf("cases = %d, want 4", len(rep.Cases))
	}
	want := map[string]map[string]Status{
		"com.acme.FooTest": {"testFooPass": Pass, "testFooFail": Fail},
		"com.acme.BarTest": {"testBarPass": Pass, "testBarError": Error},
	}
	for _, c := range rep.Cases {
		if _, ok := want[c.Class]; !ok {
			t.Errorf("unexpected class = %q", c.Class)
			continue
		}
		if wantStatus, ok := want[c.Class][c.Name]; !ok {
			t.Errorf("unexpected case %s.%s", c.Class, c.Name)
		} else if wantStatus != c.Status {
			t.Errorf("%s.%s status = %q, want %q", c.Class, c.Name, c.Status, wantStatus)
		}
	}
}

func TestReportMerge(t *testing.T) {
	a := Report{Cases: []Case{{Class: "A", Name: "x", Status: Pass}}}
	a.Merge(Report{Cases: []Case{{Class: "B", Name: "y", Status: Fail}}})
	if len(a.Cases) != 2 {
		t.Fatalf("merged cases = %d, want 2", len(a.Cases))
	}
}
