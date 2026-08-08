//go:build !windows

package fs

func unsafePlatformObject(string) bool { return false }
