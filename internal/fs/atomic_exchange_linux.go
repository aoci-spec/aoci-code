// Linux CAS requires native atomic exchange. Link/rename emulation can
// overwrite a non-cooperating writer's replacement and therefore fails closed.
// Old capture artifacts remain readable for recovery after an upgrade.
package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func exchangeAtomicPaths(first, second string) error {
	return renameat2Exchange(first, second)
}

// renameat2Exchange is the primitive behind exchangeAtomicPaths. It is a
// variable only so a test can make it answer the way DrvFs does (EINVAL) on a
// filesystem that honours the flag; production never reassigns it.
var renameat2Exchange = func(first, second string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_EXCHANGE,
	)
}

// atomicExchangeCapturePath is where the emulation parks first's inode while
// second is renamed over it. It is derived from second, which every caller
// passes as the swap path, so the name is the same in both exchange directions
// and recovery can find it without being told which direction crashed.
func atomicExchangeCapturePath(second string) string {
	return second + ".capture"
}

// normalizeAtomicExchangeArtifacts converges the two states a crash inside the
// link emulation can leave, so recoverAtomicCAS only ever judges the target and
// swap digests it already understands. It runs on every filesystem, because an
// index crashed on DrvFs may be recovered after a move to ext4.
//
//	(a) capture and swap both exist: rename(second, first) never happened, so
//	    the target still names the inode the capture links to and the capture
//	    is redundant; remove it. If a writer outside aoci replaced the target by
//	    rename in the meantime, the capture holds bytes that writer discarded,
//	    exactly as the primitive would have left them discarded.
//	(b) capture exists and swap is absent: rename(second, first) took effect and
//	    rename(capture, second) did not, so the target already holds the new
//	    inode and the old one lives only under the capture; finishing that
//	    rename puts it in the swap, where recovery reads it as the captured
//	    preimage and converges as it would after a completed exchange.
func normalizeAtomicExchangeArtifacts(swapPath string) error {
	capture := atomicExchangeCapturePath(swapPath)
	if _, err := os.Lstat(capture); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Lstat(swapPath); err == nil {
		return os.Remove(capture)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(capture, swapPath)
}
