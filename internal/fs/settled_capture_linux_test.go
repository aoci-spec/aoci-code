//go:build linux

package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The read-only view of a cut exchange sees what settling will: on the link
// fallback the displaced bytes can sit only in the .capture file, and when
// they are third-party bytes they are what the target will hold (rv35).
func TestSettledViewReadsTheLinkFallbackCapture(t *testing.T) {
	sum := func(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
	pre, post, third := []byte("pre\n"), []byte("post\n"), []byte("third party\n")
	target := filepath.Join(t.TempDir(), "baseline.json")
	intent, _ := json.Marshal(atomicCASIntent{ExpectedSHA256: sum(pre), NewSHA256: sum(post)})
	for path, data := range map[string][]byte{target: post, atomicExchangeCapturePath(target + ".aoci-cas.swap"): third, target + ".aoci-cas.intent": intent} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, pending, err := SettledAtomicWriteCASSHA256(target)
	if err != nil || !pending || got != sum(third) {
		t.Fatalf("settled view = %s pending=%v err=%v, want the captured third-party bytes", got, pending, err)
	}
	if err := SettlePendingAtomicWriteCAS(target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); sum(data) != got {
		t.Fatalf("settling left %q, the view said %s", data, got)
	}
}
