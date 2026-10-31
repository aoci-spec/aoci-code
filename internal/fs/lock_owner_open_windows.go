//go:build windows

package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openLockOwnerForObservation lets the owner delete the canonical lock while a waiter reads its identity.
// The Windows sharing semantics of Go's default os.Open cannot be assumed by this lock protocol; an explicit
// FILE_SHARE_DELETE keeps the observer's brief read-only handle from leaking into a condition that blocks Release.
func openLockOwnerForObservation(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// lockOwnerObservationTransient recognizes the short Windows window in which the name is deleted but the handle
// cleanup has not finished. It only invalidates the observation evidence and retries; a real Release error is never rewritten as success.
func lockOwnerObservationTransient(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_DELETE_PENDING)
}
