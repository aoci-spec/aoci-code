// Windows atomic path exchange: ReplaceFileW keeps canonical pointing at a complete file and saves the
// replaced version as a recovery copy; the recovery copy is then normalized to the path the shared swap state machine uses.
package fs

import (
	"fmt"
	"os"
	"strings"
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
		// ReFS refuses ReplaceFileW with ERROR_ACCESS_DENIED where NTFS
		// performs it (#210). Name the volume instead of leaving a failure
		// that reads like a lock or permission problem.
		if name := volumeFileSystemName(first); name != "" && !strings.EqualFold(name, "NTFS") {
			return fmt.Errorf("%s[%s]: this volume does not support the atomic file replacement AOCI's governed writes need; keep the repository on an NTFS volume (ReplaceFileW: %w)",
				AtomicExchangeUnsupportedFilesystem, name, callErr)
		}
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

// volumeFileSystemName reports the file system of the volume holding path
// ("NTFS", "ReFS", "FAT32", ...), or "" when it cannot be determined.
func volumeFileSystemName(path string) string {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	volume := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(pathPtr, &volume[0], uint32(len(volume))); err != nil {
		return ""
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(&volume[0], nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return ""
	}
	return windows.UTF16ToString(name)
}

// effectiveAtomicExchangeSwap names, without renaming anything, the file
// normalizeAtomicExchangeArtifacts would leave as the swap.
func effectiveAtomicExchangeSwap(swapPath string) (string, error) {
	backup := swapPath + ".exchange-backup"
	if _, err := os.Lstat(backup); os.IsNotExist(err) {
		return swapPath, nil
	} else if err != nil {
		return "", err
	}
	if _, err := os.Lstat(swapPath); err == nil {
		return "", fmt.Errorf("交换副本与swap同时存在: %s", swapPath)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return backup, nil
}
