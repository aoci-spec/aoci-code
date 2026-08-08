// Linux原子路径交换 —— CAS必须捕获交换瞬间的真实preimage。
package fs

import "golang.org/x/sys/unix"

func exchangeAtomicPaths(first, second string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_EXCHANGE,
	)
}

func normalizeAtomicExchangeArtifacts(string) error { return nil }
