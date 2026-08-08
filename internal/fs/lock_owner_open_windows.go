//go:build windows

package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openLockOwnerForObservation允许owner在等待者读取身份期间删除canonical锁。
// Go默认os.Open的Windows共享语义不能作为本锁协议假设；显式FILE_SHARE_DELETE
// 避免观察者把自己的短时只读句柄泄漏为Release阻断条件。
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

// lockOwnerObservationTransient识别Windows名称已删除但句柄清理尚未完成的
// 短暂窗口。这里只令观察证据失效并重试，不把真实Release错误改写为成功。
func lockOwnerObservationTransient(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_DELETE_PENDING)
}
