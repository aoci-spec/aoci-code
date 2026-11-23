package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"testing"
)

// The intent is written before the swap (audit G1-01). A process stopped
// between the two leaves an intent over an untouched target and no swap, and
// the next CAS write clears it and proceeds; the reverse order left a swap
// with no intent, which recovery refuses forever.
func TestAtomicWriteCASStoppedBetweenIntentAndSwapRecovers(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/asset.txt"
	if err := os.WriteFile(target, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := func(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
	stop := errors.New("stopped")
	previous := afterAtomicCASIntent
	t.Cleanup(func() { afterAtomicCASIntent = previous })
	afterAtomicCASIntent = func(string) {
		if _, err := os.Lstat(target + ".aoci-cas.intent"); err != nil {
			t.Errorf("the intent must exist before the swap is written: %v", err)
		}
		if _, err := os.Lstat(target + ".aoci-cas.swap"); !os.IsNotExist(err) {
			t.Errorf("no swap may exist before the intent: %v", err)
		}
		panic(stop)
	}
	func() {
		defer func() {
			if r := recover(); r != stop {
				panic(r)
			}
		}()
		_ = AtomicWriteCAS(target, []byte("v2\n"), sum([]byte("v1\n")))
	}()
	afterAtomicCASIntent = previous
	if data, _ := os.ReadFile(target); string(data) != "v1\n" {
		t.Fatalf("the target must be untouched: %q", data)
	}
	if err := AtomicWriteCAS(target, []byte("v3\n"), sum([]byte("v1\n"))); err != nil {
		t.Fatalf("the next write must recover the stopped one and proceed: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "v3\n" {
		t.Fatalf("got %q", data)
	}
	for _, leftover := range []string{".aoci-cas.intent", ".aoci-cas.swap"} {
		if _, err := os.Lstat(target + leftover); !os.IsNotExist(err) {
			t.Fatalf("%s must be gone", leftover)
		}
	}
}

// A shared lock directory owned by someone else, or not a directory at all,
// moves this user's CAS locks to a directory of its own (audit G1-06).
func TestAtomicCASLockDirAvoidsAForeignSharedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR does not select the temporary directory on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if got := atomicCASLockDir(); got != tmp+"/aoci-cas-locks" {
		t.Fatalf("an absent shared directory keeps the shared name: %s", got)
	}
	if err := os.WriteFile(tmp+"/aoci-cas-locks", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := atomicCASLockDir(); got == tmp+"/aoci-cas-locks" {
		t.Fatal("a non-directory in the shared place must not be used")
	}
}
