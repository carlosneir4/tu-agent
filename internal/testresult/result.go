package testresult

// Status is the outcome of one test case.
type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"    // assertion failure
	Error   Status = "error"   // uncaught error/exception
	Skipped Status = "skipped" // ignored/skipped
)

// Case is one test case result.
type Case struct {
	Class  string // e.g. "com.acme.FooTest"
	Name   string // e.g. "resolvesProfile"
	Status Status
}

// Report is the parsed set of test cases from one or more runner artifacts.
type Report struct {
	Cases []Case
}

// Merge appends another report's cases into r.
func (r *Report) Merge(other Report) {
	r.Cases = append(r.Cases, other.Cases...)
}
