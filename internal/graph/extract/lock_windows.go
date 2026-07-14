//go:build windows

package extract

// flockExclusive is a documented no-op on Windows: single-flight advisory
// locking for BuildScoped is only implemented for darwin/linux (spec
// Design). The Windows build still compiles and runs BuildScoped
// unserialized rather than degrading to an error.
func flockExclusive(fd uintptr) error {
	return nil
}

// flockRelease is the matching no-op release for flockExclusive.
func flockRelease(fd uintptr) error {
	return nil
}
