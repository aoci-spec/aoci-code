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
)

// renamex_np with RENAME_EXCL is the single-syscall no-replace primitive on
// darwin, and its manual page says it answers ENOTSUP when "the file system does
// not support that operation". That is the same shape as the Linux defect fixed
// in a08d25a: RENAME_NOREPLACE was assumed to be a kernel capability, WSL's
// DrvFs answered EINVAL, and `aoci init` failed on every path under /mnt/c.
//
// No macOS filesystem has been observed refusing RENAME_EXCL, so this test does
// not presume one does. It manufactures the filesystems a repository can
// actually live on besides the runner's APFS boot volume — an exFAT or FAT32
// external drive, an older HFS+ volume — and asserts the contract on each. If
// one of them refuses the flag, or worse honours the call but not the guarantee,
// this fails and names the filesystem.
//
// The Linux answer does not transfer, which is the reason to measure before
// changing anything: the link fallback there depends on hard links, and FAT has
// none. A darwin fallback would have to be a different construction with a
// different crash profile, so it must not be written before the evidence says
// it is needed.
func TestNoReplaceGuaranteeHoldsOnEveryReachableFilesystem(t *testing.T) {
	probed := []string{}
	assertNoReplaceContract(t, "APFS (runner boot volume)", t.TempDir())
	probed = append(probed, "APFS=honoured")

	for _, image := range []struct{ label, hdiutilFS string }{
		{"exFAT", "exFAT"},
		{"FAT32", "MS-DOS FAT32"},
		{"HFS+", "HFS+"},
	} {
		mount, detach, err := attachScratchImage(t, image.hdiutilFS)
		if err != nil {
			// Never a failure: a runner that cannot make disk images has simply
			// not answered the question, and saying so is the honest result.
			probed = append(probed, image.label+"=unavailable")
			t.Logf("%s not probed: %v", image.label, err)
			continue
		}
		t.Cleanup(detach)
		assertNoReplaceContract(t, image.label, mount)
		probed = append(probed, image.label+"=honoured")
	}

	// go test hides t.Log without -v, and native-lifecycle runs without it. One
	// stderr line keeps the capability matrix readable in the job log, which is
	// the whole point of a probe whose expected outcome is green.
	fmt.Fprintf(os.Stderr, "AOCI RENAME_EXCL probe: %s\n", strings.Join(probed, " "))
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
		t.Fatalf("%s refused RENAME_EXCL for a free name (%v); aoci init cannot create "+
			"a volume on this filesystem and needs a fallback that does not assume hard links", label, err)
	}
	if body, _ := os.ReadFile(target); string(body) != "first\n" {
		t.Fatalf("%s: target content = %q", label, body)
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
