//go:build linux

package fs

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// publishAtomicCreate publishes a staged file into a name that must not already
// exist, refusing rather than overwriting.
//
// renameat2 with RENAME_NOREPLACE is the primitive for that, but the flag is a
// filesystem capability, not a kernel one: WSL's DrvFs (9p) answers EINVAL, so
// every `aoci init` under /mnt/c failed with init_volume_create_failed while the
// identical repository initialized on ext4. The capability was assumed rather
// than probed, and the file's own siblings already show the alternative — the
// exchange path has a declared "no safe primitive here" answer for platforms
// that lack one, and this path did not.
func publishAtomicCreate(source, target string) error {
	err := renameat2NoReplace(source, target)
	if err == nil || !renameat2FlagUnsupported(err) {
		return err
	}
	if fallbackErr := publishAtomicCreateByLink(source, target); fallbackErr != nil {
		// Name both, or a future reader sees only the second failure and cannot
		// tell that the primitive was refused before the fallback was tried.
		return fmt.Errorf("renameat2 refused the no-replace flag (%w) and the link fallback failed: %w", err, fallbackErr)
	}
	return nil
}

// renameat2NoReplace is the primitive behind publishAtomicCreate. It is a
// variable only so a test can make it answer the way DrvFs does (EINVAL) on a
// filesystem that honours the flag; production never reassigns it.
var renameat2NoReplace = func(source, target string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
}

// publishAtomicCreateByLink is the same guarantee built from two older calls.
// link refuses an occupied name with EEXIST and never replaces it, so the
// caller's contract — publish, or refuse without touching what is there — holds
// exactly as it does under RENAME_NOREPLACE. The staging file is unlinked only
// after the target exists, so a crash between the two leaves the published
// bytes plus a recoverable staging file rather than nothing.
func publishAtomicCreateByLink(source, target string) error {
	if err := unix.Link(source, target); err != nil {
		return &linkError{op: "link", err: err}
	}
	// As on the darwin copy path, a failed unlink is not a failed publish:
	// the target holds the bytes, and an error here read as the name being
	// owned by somebody else (audit G1-11). A move checks its source is gone.
	_ = unix.Unlink(source)
	return nil
}

type linkError struct {
	op  string
	err error
}

func (e *linkError) Error() string { return e.op + ": " + e.err.Error() }
func (e *linkError) Unwrap() error { return e.err }

// renameat2FlagUnsupported reports the answers a filesystem gives when it cannot
// honour RENAME_NOREPLACE at all, as opposed to answering the question that was
// asked. EEXIST in particular is the caller's requested outcome and must never
// route to the fallback.
//
// Being slightly over-broad here is safe by construction: the fallback carries
// the identical no-replace guarantee, so an unnecessary fallback costs one extra
// syscall and can never weaken the refusal. Being under-broad is not safe — it
// is what left DrvFs failing.
func renameat2FlagUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.EOPNOTSUPP)
}
