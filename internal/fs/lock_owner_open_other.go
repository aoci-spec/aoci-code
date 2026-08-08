//go:build !windows

package fs

import "os"

// openLockOwnerForObservation在POSIX沿用普通只读打开；unlink不受读句柄阻塞。
func openLockOwnerForObservation(path string) (*os.File, error) {
	return os.Open(path)
}

func lockOwnerObservationTransient(error) bool {
	return false
}
