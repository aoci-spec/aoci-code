package fs

import (
	"os"
	"path/filepath"
	"testing"
)

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
