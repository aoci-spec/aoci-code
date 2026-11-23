//go:build !windows

package fs

import (
	"os"
	"strconv"
	"syscall"
)

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || int(stat.Uid) == os.Getuid()
}

func currentUserTag() string { return strconv.Itoa(os.Getuid()) }
