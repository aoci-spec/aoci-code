package baseline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// KnownLines answers only from a trusted entry and never reads: a fresh file
// is unknown, an aged and counted one is known, a rewritten one is unknown
// again, and a path never hashed is unknown.
func TestKnownLinesAnswersOnlyTrustedEntriesWithoutReading(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenFingerprintCache(root)
	if _, err := cache.HashFile(path, "a.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.FileLines(path, "a.go"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.KnownLines(path, "a.go"); ok {
		t.Fatal("a file inside the racy window is not trusted")
	}
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	aged := OpenFingerprintCache(root)
	if _, err := aged.HashFile(path, "a.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	if _, err := aged.FileLines(path, "a.go"); err != nil {
		t.Fatal(err)
	}
	if lines, ok := aged.KnownLines(path, "a.go"); !ok || lines != 3 {
		t.Fatalf("an aged, counted entry is known: %d %v", lines, ok)
	}
	if _, ok := aged.KnownLines(filepath.Join(root, "b.go"), "b.go"); ok {
		t.Fatal("a path never hashed is unknown")
	}
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := aged.KnownLines(path, "a.go"); ok {
		t.Fatal("a rewritten file is unknown until hashed again")
	}
	var none *FingerprintCache
	if _, ok := none.KnownLines(path, "a.go"); ok {
		t.Fatal("a nil cache knows nothing")
	}
}
