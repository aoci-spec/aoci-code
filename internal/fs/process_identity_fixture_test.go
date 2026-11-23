//go:build !((aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux)

package fs

// comparableIdentity spells a fixture identity the way this platform's
// reader records one, so two of them are compared.
func comparableIdentity(identity string) string { return identity }
