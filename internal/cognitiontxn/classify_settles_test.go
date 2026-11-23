package cognitiontxn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeCutExchange leaves what a process stopped between an AtomicWriteCAS
// exchange and its cleanup leaves: the target holds target, the swap holds
// swap, and the intent names expected -> written.
func writeCutExchange(t *testing.T, path string, target, swap, expected, written []byte) {
	t.Helper()
	intent, err := json.Marshal(map[string]string{"expected_sha256": SHA256(expected), "new_sha256": SHA256(written)})
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string][]byte{path: target, path + ".aoci-cas.swap": swap, path + ".aoci-cas.intent": intent} {
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Classify settles a cut exchange before judging the bytes: a completed
// exchange reads as the postimage with nothing left beside it, and one that
// captured third-party bytes restores them instead of reading as done
// (audit G1-02).
func TestClassifySettlesACutExchangeFirst(t *testing.T) {
	pre, post, third := []byte("pre\n"), []byte("post\n"), []byte("third party\n")

	completed := filepath.Join(t.TempDir(), "aoci.code.txt")
	writeCutExchange(t, completed, post, pre, pre, post)
	state, _, err := Classify(completed, SHA256(pre), SHA256(post), false)
	if err != nil || state != StatePostimage {
		t.Fatalf("completed exchange: state=%s err=%v", state, err)
	}
	for _, suffix := range []string{".aoci-cas.swap", ".aoci-cas.intent"} {
		if _, err := os.Lstat(completed + suffix); !os.IsNotExist(err) {
			t.Fatalf("%s left beside a settled target: %v", suffix, err)
		}
	}

	conflicted := filepath.Join(t.TempDir(), "aoci.code.txt")
	writeCutExchange(t, conflicted, post, third, pre, post)
	state, _, err = Classify(conflicted, SHA256(pre), SHA256(post), false)
	if err != nil || state != StateUnknown {
		t.Fatalf("third-party exchange read as %s (err=%v), want unknown", state, err)
	}
	if data, _ := os.ReadFile(conflicted); string(data) != string(third) {
		t.Fatalf("third-party bytes were not restored: %q", data)
	}
}
