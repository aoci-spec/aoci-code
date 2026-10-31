//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package fs

// processIdentity returns unknown on a platform it cannot probe reliably, which forbids automatic takeover.
func processIdentity(int) (string, bool, bool) {
	return "", false, false
}
