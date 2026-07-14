//go:build unix

package extract

import "syscall"

// flockExclusive takes a blocking exclusive advisory lock on fd. It returns
// once the lock is held; the kernel releases it automatically if the holding
// process dies, so there is no timeout or staleness handling here (spec
// Decision 2).
func flockExclusive(fd uintptr) error {
	return syscall.Flock(int(fd), syscall.LOCK_EX)
}

// flockRelease releases a lock previously taken by flockExclusive.
func flockRelease(fd uintptr) error {
	return syscall.Flock(int(fd), syscall.LOCK_UN)
}
