//go:build !linux && !darwin && !windows

// Other platforms lack an atomic path-exchange primitive usable from the standard library; the safe direction
// is an explicit "unavailable", never a fall-back to a compare-then-rename pseudo CAS.
package fs

import "fmt"

func exchangeAtomicPaths(_, _ string) error {
	return fmt.Errorf("当前平台不支持原子路径交换")
}

func normalizeAtomicExchangeArtifacts(string) error { return nil }
