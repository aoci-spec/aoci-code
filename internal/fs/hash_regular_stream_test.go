package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The digest is streamed: hashing a 64 MiB file allocates far less than the
// file (rv35: reading it whole ran a status out of memory).
func TestHashRegularFileInRootStreams(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := HashRegularFileInRoot(root, "big.bin"); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Fatalf("hashing a 64 MiB file allocated %d bytes", allocated)
	}
}
