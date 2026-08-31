//go:build darwin

package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// renamex_np with RENAME_EXCL is the single-syscall no-replace primitive on
// darwin, and the flag is a filesystem capability rather than a kernel one.
// This probe measured that on both macOS runners: exFAT answers ENOTSUP, so
// before the copy fallback existed a repository on an external drive could not
// be initialized at all. exFAT is precisely what macOS and Windows share, so
// that is an ordinary place to keep a repository, not an exotic one.
//
// Each filesystem is a subtest so one refusal reports itself without hiding the
// answers from the rest — the first version of this test used t.Fatalf, stopped
// at exFAT, and left FAT32 and HFS+ unmeasured.
//
// A filesystem that cannot be attached fails the run rather than being skipped.
// The meaning has to be carried by the pass, because nothing else can carry it:
// native-lifecycle runs go test in package-list mode, where the output of a
// passing test — t.Log and a direct os.Stderr write alike — is discarded. A
// coverage line is therefore visible only on failure, which is exactly when it
// is not needed. Green must mean "all of these were probed", or green means
// "APFS, and possibly nothing else" with no way to tell the two apart.
func TestNoReplaceGuaranteeHoldsOnEveryReachableFilesystem(t *testing.T) {
	t.Run("APFS", func(t *testing.T) { assertNoReplaceContract(t, "APFS", t.TempDir()) })

	optional := os.Getenv("AOCI_ATOMIC_PROBE_OPTIONAL") != ""
	for _, image := range []struct{ label, hdiutilFS string }{
		{"exFAT", "exFAT"},
		{"FAT32", "MS-DOS FAT32"},
		{"HFS+", "HFS+"},
	} {
		t.Run(image.label, func(t *testing.T) {
			mount, detach, err := attachScratchImage(t, image.hdiutilFS)
			if err != nil {
				if optional {
					t.Skipf("not probed: %v", err)
				}
				t.Fatalf("could not build a %s volume to probe (%v). exFAT is known to refuse "+
					"RENAME_EXCL, so an unprobed run proves nothing about the fallback; set "+
					"AOCI_ATOMIC_PROBE_OPTIONAL=1 where disk images are unavailable", image.label, err)
			}
			t.Cleanup(detach)
			assertNoReplaceContract(t, image.label, mount)
		})
	}
}

// assertNoReplaceContract exercises the exact promise publishAtomicCreate makes:
// publish into a free name, and refuse an occupied one without touching what is
// already there.
func assertNoReplaceContract(t *testing.T, label, dir string) {
	t.Helper()
	source := filepath.Join(dir, "aoci-staging-1")
	target := filepath.Join(dir, "aoci-target")
	if err := os.WriteFile(source, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	t.Cleanup(func() { os.Remove(source); os.Remove(target) })

	if err := publishAtomicCreate(source, target); err != nil {
		t.Fatalf("%s could not publish into a free name: %v", label, err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("%s: target content = %q", label, body)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s: the staging file survived a successful publish", label)
	}

	second := filepath.Join(dir, "aoci-staging-2")
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	t.Cleanup(func() { os.Remove(second) })

	err := publishAtomicCreate(second, target)
	if err == nil {
		t.Fatalf("%s published over an occupied name: the no-replace guarantee is absent "+
			"even though the call succeeded, which is worse than a refusal", label)
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("%s: an occupied name must report EEXIST, got %v", label, err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("%s: a refused publish changed the target: %q", label, body)
	}
	if _, err := os.Lstat(second); err != nil {
		t.Fatalf("%s: a refused publish consumed its staging file: %v", label, err)
	}
}

// The copy fallback must carry the refusal on its own, on any filesystem,
// because that is the whole reason it may stand in for the primitive.
func TestCopyFallbackKeepsTheNoReplaceGuarantee(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "staging")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishAtomicCreateByCopy(source, target); err != nil {
		t.Fatalf("publishing into a free name failed: %v", err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("target content = %q", body)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the copy did not carry the staged mode: %v %v", info, err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the staging file survived a successful publish")
	}

	second := filepath.Join(dir, "staging2")
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishAtomicCreateByCopy(second, target); !errors.Is(err, os.ErrExist) {
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
	for _, unsupported := range []error{unix.ENOTSUP, unix.EOPNOTSUPP, unix.EINVAL, unix.ENOSYS} {
		if !renameExclUnsupported(unsupported) {
			t.Fatalf("%v must select the fallback; exFAT answers ENOTSUP and would stay broken", unsupported)
		}
	}
	for _, real := range []error{unix.EEXIST, unix.EACCES, unix.EPERM, unix.EROFS, unix.ENOENT, unix.EXDEV} {
		if renameExclUnsupported(real) {
			t.Fatalf("%v is a real answer and must be propagated, not retried through the fallback", real)
		}
	}
}

// attachScratchImage builds a small disk image with the requested filesystem and
// mounts it, returning the mount point and a detach function. Every failure is
// reported to the caller rather than failing the test: a sandbox that forbids
// hdiutil leaves the question unanswered, and pretending otherwise would make a
// green run mean less than it appears to.
func attachScratchImage(t *testing.T, filesystem string) (string, func(), error) {
	t.Helper()
	dmg := filepath.Join(t.TempDir(), "probe.dmg")
	if out, err := hdiutil(t, "create", "-size", "16m", "-fs", filesystem,
		"-volname", "AOCIProbe", "-quiet", dmg); err != nil {
		return "", nil, fmt.Errorf("create: %w (%s)", err, out)
	}
	mount := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return "", nil, err
	}
	if out, err := hdiutil(t, "attach", dmg, "-nobrowse", "-quiet", "-mountpoint", mount); err != nil {
		return "", nil, fmt.Errorf("attach: %w (%s)", err, out)
	}
	return mount, func() { hdiutil(t, "detach", mount, "-quiet", "-force") }, nil
}

func hdiutil(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "hdiutil", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
