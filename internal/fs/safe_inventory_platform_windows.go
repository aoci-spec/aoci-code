//go:build windows

package fs

import "golang.org/x/sys/windows"

// Windows reparse points can appear as directories and must be rejected before
// traversal. System/device objects are also outside the ordinary source-file
// boundary. Hidden regular files remain eligible and are classified by name.
func unsafePlatformObject(path string) bool {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := windows.GetFileAttributes(pointer)
	if err != nil {
		return true
	}
	return attributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_SYSTEM|windows.FILE_ATTRIBUTE_DEVICE) != 0
}
