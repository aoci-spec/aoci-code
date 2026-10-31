// Darwin atomic path exchange: renamex_np(RENAME_SWAP) captures the real preimage at the instant of the swap.
package fs

import "golang.org/x/sys/unix"

func exchangeAtomicPaths(first, second string) error {
	return unix.RenamexNp(first, second, unix.RENAME_SWAP)
}

func normalizeAtomicExchangeArtifacts(string) error { return nil }
