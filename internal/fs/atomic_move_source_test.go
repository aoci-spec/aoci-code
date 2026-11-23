package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A degraded publish that copies or links but cannot remove its source has
// not moved anything: AtomicMoveCAS reports it instead of returning success
// with the formal asset still in place (audit G1-11).
func TestAtomicMoveCASReportsASourceLeftBehind(t *testing.T) {
	dir := t.TempDir()
	source, recovery := filepath.Join(dir, "aoci.code.txt"), filepath.Join(dir, "recovered", "aoci.code.txt")
	data := []byte("formal bytes\n")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(recovery), 0o755); err != nil {
		t.Fatal(err)
	}
	original := publishAtomicCreatePlatform
	t.Cleanup(func() { publishAtomicCreatePlatform = original })
	publishAtomicCreatePlatform = func(from, to string) error {
		bytes, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		return os.WriteFile(to, bytes, 0o644) // copied; the source removal "failed"
	}
	digest := sha256.Sum256(data)
	err := AtomicMoveCAS(source, recovery, hex.EncodeToString(digest[:]))
	if !errors.Is(err, ErrAtomicCASRecovery) {
		t.Fatalf("a move that left its source reported %v", err)
	}
}
