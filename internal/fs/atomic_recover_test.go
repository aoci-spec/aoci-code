package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverAtomicWriteCASIsQuietWithoutAnExchange(t *testing.T) {
	target := filepath.Join(t.TempDir(), "asset.txt")
	if err := os.WriteFile(target, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAtomicWriteCAS(target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "kept\n" {
		t.Fatalf("nothing may be written without a pending exchange: %q", data)
	}
	// An exchange cut after the swap is settled by the recovery alone.
	sum := sha256.Sum256([]byte("kept\n"))
	if err := AtomicWriteCAS(target, []byte("next\n"), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAtomicWriteCAS(target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "next\n" {
		t.Fatalf("a completed exchange must stay: %q", data)
	}
}
