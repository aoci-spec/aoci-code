//go:build !windows

package fs

import "os"

// openLockOwnerForObservation uses an ordinary read-only open on POSIX; unlink is not blocked by a read handle.
func openLockOwnerForObservation(path string) (*os.File, error) {
	return os.Open(path)
}

func lockOwnerObservationTransient(error) bool {
	return false
}
