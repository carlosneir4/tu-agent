package coverage

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseGoProfile parses `go test -coverprofile` output. Each block line is
// `file:sLine.sCol,eLine.eCol numStmt count`; lines sLine..eLine become known,
// and covered when count > 0. modulePath (from go.mod) is stripped so keys are
// repo-relative.
func ParseGoProfile(r io.Reader, modulePath string) (Profile, error) {
	p := Profile{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		file := strings.TrimPrefix(line[:colon], modulePath+"/")
		var sLine, sCol, eLine, eCol, numStmt, count int
		if _, err := fmt.Sscanf(line[colon+1:], "%d.%d,%d.%d %d %d",
			&sLine, &sCol, &eLine, &eCol, &numStmt, &count); err != nil {
			continue // tolerate malformed lines
		}
		for ln := sLine; ln <= eLine; ln++ {
			p.add(file, ln, count > 0)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("coverage.ParseGoProfile: %w", err)
	}
	return p, nil
}
