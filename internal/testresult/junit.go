package testresult

import (
	"encoding/xml"
	"fmt"
	"io"
)

// junitCase mirrors a <testcase> element; presence of a child marks its status.
type junitCase struct {
	Classname string    `xml:"classname,attr"`
	Name      string    `xml:"name,attr"`
	Failure   *struct{} `xml:"failure"`
	Error     *struct{} `xml:"error"`
	Skipped   *struct{} `xml:"skipped"`
}

// junitSuite mirrors a <testsuite>; <testsuites> nests suites.
type junitSuite struct {
	Cases  []junitCase  `xml:"testcase"`
	Suites []junitSuite `xml:"testsuite"`
}

// ParseJUnitXML parses one JUnit XML artifact (a <testsuite> or <testsuites>
// root) into a Report. A <testcase> with a <failure> child is Fail, <error> is
// Error, <skipped> is Skipped, otherwise Pass.
func ParseJUnitXML(r io.Reader) (Report, error) {
	// Decode against a permissive root that accepts either element name.
	var root struct {
		junitSuite
		Suites []junitSuite `xml:"testsuite"`
	}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	if err := dec.Decode(&root); err != nil {
		return Report{}, fmt.Errorf("testresult.ParseJUnitXML: %w", err)
	}
	var rep Report
	var walk func(s junitSuite)
	walk = func(s junitSuite) {
		for _, c := range s.Cases {
			rep.Cases = append(rep.Cases, Case{
				Class:  c.Classname,
				Name:   c.Name,
				Status: caseStatus(c),
			})
		}
		for _, sub := range s.Suites {
			walk(sub)
		}
	}
	// A <testsuite> root populates root.junitSuite; a <testsuites> root populates
	// root.Suites. Handle both.
	walk(root.junitSuite)
	for _, s := range root.Suites {
		walk(s)
	}
	return rep, nil
}

func caseStatus(c junitCase) Status {
	switch {
	case c.Failure != nil:
		return Fail
	case c.Error != nil:
		return Error
	case c.Skipped != nil:
		return Skipped
	default:
		return Pass
	}
}
