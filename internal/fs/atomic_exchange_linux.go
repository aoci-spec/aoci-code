// Linux atomic path exchange: CAS must capture the real preimage at the instant of the swap.
//
// renameat2 with RENAME_EXCHANGE is the primitive, but like RENAME_NOREPLACE
// next door it is a filesystem capability, not a kernel one: WSL's DrvFs (9p)
// answers EINVAL, so every governed CAS write under /mnt/c failed with
// "原子交换目标失败: invalid argument" while `aoci init` on the same path had
// worked since the no-replace path learned to probe (#116). The flag is now
// probed, not assumed. When it is refused the exchange is emulated with a hard
// link and two renames, which DrvFs honours, and the emulation ends in the
// exact post-state the primitive produces: first holds second's old inode and
// second holds first's old inode, so AtomicWriteCAS's preimage check and
// rollbackAtomicCAS's swap-back run unchanged on top of either.
//
// What the emulation cannot give is a zero-width replacement window. The
// primitive swaps both names in one step; the emulation holds the target's
// inode under a capture link, renames second over first, then renames the
// capture into second. A non-cooperating writer that replaces the target by
// rename between the link and the first rename is overwritten rather than
// detected, because the exchange never sees its inode. A writer that modifies
// the target in place during that window is still detected, because the
// capture link shares the inode and the modified bytes arrive in the swap as
// the captured preimage. Every cooperating aoci writer is serialized by the
// cross-process CAS lock AtomicWriteCAS takes on the target before any of
// this runs, so the window is only ever visible to writers outside aoci.
//
// A crash inside the emulation leaves the capture link beside the swap;
// normalizeAtomicExchangeArtifacts converges both states before recovery
// judges digests, so the next call finds the same two assets it would have
// found after a crashed primitive exchange.
package fs

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func exchangeAtomicPaths(first, second string) error {
	err := renameat2Exchange(first, second)
	if err == nil || !renameat2FlagUnsupported(err) {
		return err
	}
	if fallbackErr := exchangeAtomicPathsByLink(first, second); fallbackErr != nil {
		// Name both, or a future reader sees only the second failure and cannot
		// tell that the primitive was refused before the fallback was tried.
		return fmt.Errorf("renameat2 refused the exchange flag (%w) and the link fallback failed: %w", err, fallbackErr)
	}
	return nil
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

// exchangeAtomicPathsByLink is the exchange built from link and rename.
//
//	link(first, capture)      capture and first now name the same inode
//	rename(second, first)     first names second's inode; the old inode lives on in capture
//	rename(capture, second)   second names the old inode; capture is gone
//
// A failure before the second rename takes effect is undone by unlinking the
// capture, which is redundant with first. A failure after it leaves first
// already replaced and the old inode only under the capture; that is state (b)
// of normalizeAtomicExchangeArtifacts and is left for it rather than reversed
// here, since the reverse would be two more renames that can fail in turn.
func exchangeAtomicPathsByLink(first, second string) error {
	capture := atomicExchangeCapturePath(second)
	if err := unix.Link(first, capture); err != nil {
		return &linkError{op: "link", err: err}
	}
	if err := unix.Rename(second, first); err != nil {
		_ = unix.Unlink(capture)
		return &linkError{op: "rename second over first", err: err}
	}
	if err := unix.Rename(capture, second); err != nil {
		return &linkError{op: "rename capture to second", err: err}
	}
	return nil
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
