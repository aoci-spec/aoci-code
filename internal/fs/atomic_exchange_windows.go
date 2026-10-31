// Windows atomic path exchange: ReplaceFileW keeps canonical pointing at a complete file and saves the
// replaced version as a recovery copy; the recovery copy is then normalized to the path the shared swap state machine uses.
package fs

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func exchangeAtomicPaths(first, second string) error {
	backup := second + ".exchange-backup"
	if err := normalizeAtomicExchangeArtifacts(second); err != nil {
		return err
	}
	firstPtr, err := windows.UTF16PtrFromString(first)
	if err != nil {
		return err
	}
	secondPtr, err := windows.UTF16PtrFromString(second)
	if err != nil {
		return err
	}
	backupPtr, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	r1, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(firstPtr)),
		uintptr(unsafe.Pointer(secondPtr)),
		uintptr(unsafe.Pointer(backupPtr)),
		0,
		0,
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("ReplaceFileW失败: %w", callErr)
	}
	return normalizeAtomicExchangeArtifacts(second)
}

func normalizeAtomicExchangeArtifacts(swapPath string) error {
	backup := swapPath + ".exchange-backup"
	if _, err := os.Lstat(backup); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Lstat(swapPath); err == nil {
		return fmt.Errorf("交换副本与swap同时存在: %s", swapPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(backup, swapPath)
}
