//go:build windows

package fs

import "golang.org/x/sys/windows"

func publishAtomicCreate(source, target string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	// No MOVEFILE_REPLACE_EXISTING flag: an existing file, directory, junction,
	// or reparse point is a hard conflict. WRITE_THROUGH requests durable move
	// completion after the source file has already been flushed.
	return windows.MoveFileEx(sourcePtr, targetPtr, windows.MOVEFILE_WRITE_THROUGH)
}
