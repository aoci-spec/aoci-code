package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cacheFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func ageFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// A hit returns byte for byte what a cold hash returns, and the second call
// over an unchanged, settled file is a hit.
func TestFingerprintCacheHitEqualsColdHash(t *testing.T) {
	root, path := cacheFixture(t)
	ageFile(t, path, 10*time.Second)
	cold, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cache := OpenFingerprintCache(root)
	first, err := cache.HashFile(path, "main.go", Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	cache.Save(true)
	reopened := OpenFingerprintCache(root)
	second, err := reopened.HashFile(path, "main.go", Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	if first != cold || second != cold {
		t.Fatalf("cache answers differ from the cold hash: cold=%+v first=%+v second=%+v", cold, first, second)
	}
	if cache.Misses != 1 || cache.Hits != 0 || reopened.Hits != 1 || reopened.Misses != 0 {
		t.Fatalf("hit accounting: first miss=%d hit=%d, reopened hit=%d miss=%d", cache.Misses, cache.Hits, reopened.Hits, reopened.Misses)
	}
	if first.FormatKind != "gofmt" || second.FormatSHA256 != first.FormatSHA256 {
		t.Fatalf("the gofmt digest did not survive the cache: %+v", second)
	}
}

// New content at the same size carries a newer mtime, so it is hashed again.
func TestFingerprintCacheRehashesChangedContentOfTheSameSize(t *testing.T) {
	root, path := cacheFixture(t)
	ageFile(t, path, 10*time.Second)
	cache := OpenFingerprintCache(root)
	before, err := cache.HashFile(path, "main.go", Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	cache.Save(true)
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n\n"[:len("package main\n\nfunc main() {}\n")]), 0o644); err != nil {
		t.Fatal(err)
	}
	// Same byte count, different bytes, mtime now.
	if err := os.WriteFile(path, []byte("package main\n\nfunc mainx(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reopened := OpenFingerprintCache(root)
	after, err := reopened.HashFile(path, "main.go", Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	if after.SHA256 == before.SHA256 || reopened.Hits != 0 {
		t.Fatalf("a changed file of the same size was served from the cache: before=%s after=%s hits=%d", before.SHA256[:12], after.SHA256[:12], reopened.Hits)
	}
}

// A file hashed within the racy window of its own mtime is not trusted on the
// next call: a write in the same seconds can leave size and mtime intact.
func TestFingerprintCacheRacyEntryIsHashedAgain(t *testing.T) {
	root, path := cacheFixture(t)
	cache := OpenFingerprintCache(root)
	if _, err := cache.HashFile(path, "main.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	cache.Save(true)
	reopened := OpenFingerprintCache(root)
	if _, err := reopened.HashFile(path, "main.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	if reopened.Hits != 0 || reopened.Misses != 1 {
		t.Fatalf("a racily clean entry was trusted: hits=%d misses=%d", reopened.Hits, reopened.Misses)
	}
}

// A corrupt or foreign cache file means every path is hashed, and Save
// replaces it; without an .aoci directory nothing is written at all.
func TestFingerprintCacheFailsOpenAndNeverCreatesAoci(t *testing.T) {
	root, path := cacheFixture(t)
	ageFile(t, path, 10*time.Second)
	cachePath := filepath.Join(root, ".aoci", fingerprintCacheFile)
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := OpenFingerprintCache(root)
	if _, err := cache.HashFile(path, "main.go", Fingerprint{}); err != nil || cache.Misses != 1 {
		t.Fatalf("corrupt cache did not fail open: err=%v misses=%d", err, cache.Misses)
	}
	cache.Save(true)
	var doc fingerprintCacheDocument
	data, err := os.ReadFile(cachePath)
	if err != nil || json.Unmarshal(data, &doc) != nil || doc.Version != fingerprintCacheVersion || len(doc.Entries) != 1 {
		t.Fatalf("Save did not replace the corrupt file: err=%v doc=%+v", err, doc)
	}
	bare := t.TempDir()
	file := filepath.Join(bare, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	none := OpenFingerprintCache(bare)
	if _, err := none.HashFile(file, "a.txt", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	none.Save(true)
	if _, err := os.Stat(filepath.Join(bare, ".aoci")); !os.IsNotExist(err) {
		t.Fatalf("a read created .aoci in a repository without AOCI assets: %v", err)
	}
}

// Pruning drops the entries a whole-tree walk did not ask for, and the
// environment switch turns the cache off for the process.
func TestFingerprintCachePrunesAndCanBeSwitchedOff(t *testing.T) {
	root, path := cacheFixture(t)
	ageFile(t, path, 10*time.Second)
	gone := filepath.Join(root, "gone.go")
	if err := os.WriteFile(gone, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageFile(t, gone, 10*time.Second)
	cache := OpenFingerprintCache(root)
	for rel, p := range map[string]string{"main.go": path, "gone.go": gone} {
		if _, err := cache.HashFile(p, rel, Fingerprint{}); err != nil {
			t.Fatal(err)
		}
	}
	cache.Save(true)
	second := OpenFingerprintCache(root)
	if _, err := second.HashFile(path, "main.go", Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	second.Save(true)
	third := OpenFingerprintCache(root)
	if _, ok := third.entries["gone.go"]; ok || len(third.entries) != 1 {
		t.Fatalf("an unseen entry survived a pruning save: %v", third.entries)
	}
	t.Setenv(FingerprintCacheEnv, "off")
	off := OpenFingerprintCache(root)
	for i := 0; i < 2; i++ {
		if _, err := off.HashFile(path, "main.go", Fingerprint{}); err != nil {
			t.Fatal(err)
		}
	}
	if off.Hits != 0 || off.Misses != 0 || !off.disabled {
		t.Fatalf("the switch did not disable the cache: hits=%d misses=%d", off.Hits, off.Misses)
	}
}
