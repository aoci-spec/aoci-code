// Darwin原子路径交换 —— renamex_np(RENAME_SWAP)捕获交换瞬间的真实preimage。
package fs

import "golang.org/x/sys/unix"

func exchangeAtomicPaths(first, second string) error {
	return unix.RenamexNp(first, second, unix.RENAME_SWAP)
}

func normalizeAtomicExchangeArtifacts(string) error { return nil }
