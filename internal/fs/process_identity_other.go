//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package fs

// processIdentity在未知平台无法可靠探测时返回未知，禁止自动抢占。
func processIdentity(int) (string, bool, bool) {
	return "", false, false
}
