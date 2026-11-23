//go:build !((aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux)

package fs

// identitiesComparable: /proc start ticks on Linux and creation times on
// Windows do not depend on the caller's environment, so any two compare.
func identitiesComparable(recorded, current string) bool { return true }
