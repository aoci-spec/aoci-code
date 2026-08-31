//go:build darwin

package fs

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// publishAtomicCreate publishes a staged file into a name that must not already
// exist, refusing rather than overwriting.
//
// renamex_np with RENAME_EXCL is the primitive for that, but the flag is a
// filesystem capability, not a kernel one. Measured on both macOS runners:
// exFAT answers ENOTSUP, so a repository on an external drive — exFAT is what
// macOS and Windows share — cannot be initialized at all. This is the same
// defect DrvFs produced on Linux, found here by probing instead of by a user.
//
// The Linux repair does not transfer. There the fallback is link+unlink, and
// the filesystems that refuse RENAME_EXCL are exactly the msdos family, which
// has no hard links; trying link first would only add a syscall that always
// fails wherever this path is reached.
func publishAtomicCreate(source, target string) error {
	err := unix.RenamexNp(source, target, unix.RENAME_EXCL)
	if err == nil || !renameExclUnsupported(err) {
		return err
	}
	if fallbackErr := publishAtomicCreateByCopy(source, target); fallbackErr != nil {
		// Name both, or a future reader sees only the second failure and cannot
		// tell that the primitive was refused before the fallback was tried.
		return fmt.Errorf("renamex_np refused the exclusive flag (%w) and the copy fallback failed: %w", err, fallbackErr)
	}
	return nil
}

// publishAtomicCreateByCopy reserves the name with O_EXCL and then fills it.
//
// O_EXCL is what carries the guarantee: the kernel either creates the name or
// answers EEXIST, so an occupied target is refused untouched exactly as
// RENAME_EXCL would refuse it, and two racing publishers cannot both win.
//
// What it does not carry is atomic *content* publication. Between the create
// and the final write the target exists and is short, where a rename would have
// exposed the complete file or nothing. That window is why this is a fallback
// and not the primary path, and it is bounded on both sides: every failure
// after the create removes the target, so a partial file never survives to look
// published, and both callers in atomic.go verify the published digest and
// report ErrAtomicCASRecovery rather than trusting the write.
func publishAtomicCreateByCopy(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staged source is not a regular file")
	}

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err // EEXIST here is the caller's requested refusal, not a fault
	}
	// From here the target is ours, so unwinding it is safe: nothing else can
	// have created it, and leaving a short file behind would both masquerade as
	// published and block every later attempt on that name.
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(target)
		return err
	}

	// Best effort, and deliberately not an error: the bytes are published, and
	// atomic.go answers a publish error by stat-ing the target and reporting
	// ErrAtomicCreateConflict when it exists. Returning a failure here would
	// therefore describe a successful publish as somebody else owning the name.
	// A surviving staging file is recoverable; that misreport is not.
	_ = os.Remove(source)
	return nil
}

// renameExclUnsupported reports the answers a filesystem gives when it cannot
// honour RENAME_EXCL at all, as opposed to answering the question that was
// asked. EEXIST in particular is the caller's requested outcome and must never
// route to the fallback.
//
// Being slightly over-broad is safe by construction: the fallback carries the
// same refusal guarantee, so an unnecessary fallback costs atomicity of content
// on a filesystem that was about to fail outright. EPERM is deliberately absent
// because it is a genuine answer here, not a capability report.
func renameExclUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.ENOSYS)
}
