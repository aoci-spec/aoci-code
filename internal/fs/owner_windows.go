//go:build windows

package fs

import "os"

// The temporary directory is per user on Windows.
func ownedByCurrentUser(os.FileInfo) bool { return true }

func currentUserTag() string { return "user" }
