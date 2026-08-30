//go:build linux

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// renameat2 with RENAME_NOREPLACE is the single-syscall no-replace primitive,
// but a filesystem may refuse the flag outright. WSL's DrvFs (9p) answers
// EINVAL, which made `aoci init` fail with init_volume_create_failed on any
// path under /mnt/c while the identical repository initialized on ext4 — the
// primitive was assumed rather than probed.
func TestLinkFallbackKeepsTheNoReplaceGuarantee(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "staging")
	target := filepath.Join(dir, "target")

	if err := os.WriteFile(source, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishAtomicCreateByLink(source, target); err != nil {
		t.Fatalf("publishing into a free name failed: %v", err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("target content = %q", body)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the staging file survived a successful publish")
	}

	// The guarantee: an occupied name is refused, and its bytes are untouched.
	second := filepath.Join(dir, "staging2")
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := publishAtomicCreateByLink(second, target)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("publishing onto an occupied name must report EEXIST, got %v", err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("a refused publish changed the target: %q", body)
	}
	if _, err := os.Lstat(second); err != nil {
		t.Fatalf("a refused publish consumed its staging file: %v", err)
	}
}

// A filesystem that cannot honour the flag and a target that is genuinely
// occupied must never be confused: the first falls back, the second is the
// answer the caller asked for.
func TestUnsupportedClassificationNeverSwallowsARealRefusal(t *testing.T) {
	for _, unsupported := range []error{unix.EINVAL, unix.ENOSYS, unix.EOPNOTSUPP} {
		if !renameat2FlagUnsupported(unsupported) {
			t.Fatalf("%v must select the fallback; DrvFs answers EINVAL and would stay broken", unsupported)
		}
	}
	for _, real := range []error{unix.EEXIST, unix.EACCES, unix.EROFS, unix.ENOENT, unix.EXDEV} {
		if renameat2FlagUnsupported(real) {
			t.Fatalf("%v is a real answer and must be propagated, not retried through the fallback", real)
		}
	}
}

// Opt-in: point this at a directory on the filesystem under suspicion and it
// exercises the complete public path. AOCI_ATOMIC_PROBE_DIR=/mnt/c/... proves
// the DrvFs case that no CI runner can reach.
func TestAtomicCreateCASOnAProbedFilesystem(t *testing.T) {
	dir := os.Getenv("AOCI_ATOMIC_PROBE_DIR")
	if dir == "" {
		t.Skip("set AOCI_ATOMIC_PROBE_DIR to exercise a specific filesystem")
	}
	target := filepath.Join(dir, "aoci-atomic-probe.txt")
	os.Remove(target)
	t.Cleanup(func() { os.Remove(target) })

	if err := AtomicCreateCASMode(target, []byte("created\n"), 0o644); err != nil {
		t.Fatalf("AtomicCreateCASMode failed on %s: %v", dir, err)
	}
	if body, _ := os.ReadFile(target); string(body) != "created\n" {
		t.Fatalf("target content = %q", body)
	}
	if err := AtomicCreateCASMode(target, []byte("second\n"), 0o644); err == nil {
		t.Fatal("creating over an existing target succeeded; the no-replace guarantee is gone")
	}
	if body, _ := os.ReadFile(target); string(body) != "created\n" {
		t.Fatalf("a refused create changed the target: %q", body)
	}
}
