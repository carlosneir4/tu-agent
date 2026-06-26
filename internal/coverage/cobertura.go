package coverage

import (
	"encoding/xml"
	"fmt"
	"io"
)

// ParseCobertura parses coverage.py's Cobertura XML (`coverage xml`). A line is
// covered when hits > 0. Class filenames are project-relative; SymbolCoverage
// suffix-matches them to node paths.
func ParseCobertura(r io.Reader) (Profile, error) {
	var cov struct {
		Packages []struct {
			Classes []struct {
				Filename string `xml:"filename,attr"`
				Lines    []struct {
					Number int `xml:"number,attr"`
					Hits   int `xml:"hits,attr"`
				} `xml:"lines>line"`
			} `xml:"classes>class"`
		} `xml:"packages>package"`
	}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	if err := dec.Decode(&cov); err != nil {
		return nil, fmt.Errorf("coverage.ParseCobertura: %w", err)
	}
	p := Profile{}
	for _, pkg := range cov.Packages {
		for _, cls := range pkg.Classes {
			for _, ln := range cls.Lines {
				p.add(cls.Filename, ln.Number, ln.Hits > 0)
			}
		}
	}
	return p, nil
}
