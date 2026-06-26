package coverage

import (
	"encoding/xml"
	"fmt"
	"io"
)

// ParseJaCoCo parses a jacoco.xml report. A line is covered when its covered
// instruction count (ci) is > 0. Keys are "package/sourcefile" (no src root);
// SymbolCoverage suffix-matches them to repo-relative node paths.
func ParseJaCoCo(r io.Reader) (Profile, error) {
	var rep struct {
		Packages []struct {
			Name        string `xml:"name,attr"`
			Sourcefiles []struct {
				Name  string `xml:"name,attr"`
				Lines []struct {
					Nr int `xml:"nr,attr"`
					Ci int `xml:"ci,attr"`
				} `xml:"line"`
			} `xml:"sourcefile"`
		} `xml:"package"`
	}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	if err := dec.Decode(&rep); err != nil {
		return nil, fmt.Errorf("coverage.ParseJaCoCo: %w", err)
	}
	p := Profile{}
	for _, pkg := range rep.Packages {
		for _, sf := range pkg.Sourcefiles {
			file := sf.Name
			if pkg.Name != "" {
				file = pkg.Name + "/" + sf.Name
			}
			for _, ln := range sf.Lines {
				p.add(file, ln.Nr, ln.Ci > 0)
			}
		}
	}
	return p, nil
}
