//go:build linux

package fs

import "golang.org/x/sys/unix"

func publishAtomicCreate(source, target string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		source,
		unix.AT_FDCWD,
		target,
		unix.RENAME_NOREPLACE,
	)
}
