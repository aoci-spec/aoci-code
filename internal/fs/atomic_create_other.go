//go:build !linux && !darwin && !windows

package fs

import "fmt"

func publishAtomicCreate(_, _ string) error {
	return fmt.Errorf("current platform has no supported atomic no-replace primitive")
}
