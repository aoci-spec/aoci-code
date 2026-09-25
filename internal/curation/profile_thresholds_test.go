package curation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The published thresholds: a NUL byte inside the first 8000 bytes makes a
// file binary, one byte later does not; 1 MiB is still readable, one byte
// more is oversize; an empty file is empty.
func TestProfileThresholdsMatchThePublishedNumbers(t *testing.T) {
	if BinarySniffBytes != 8000 || OversizeBytes != 1<<20 {
		t.Fatalf("thresholds moved: sniff=%d oversize=%d", BinarySniffBytes, OversizeBytes)
	}
	root := t.TempDir()
	write := func(name string, size int, nulAt int) {
		body := []byte(strings.Repeat("a", size))
		if nulAt >= 0 && nulAt < size {
			body[nulAt] = 0
		}
		if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nul-inside.txt", 9000, 7999)
	write("nul-outside.txt", 9000, 8000)
	write("at-limit.txt", 1<<20, -1)
	write("over-limit.txt", 1<<20+1, -1)
	write("empty.txt", 0, -1)
	profiles := BuildProfiles(root, []string{"nul-inside.txt", "nul-outside.txt", "at-limit.txt", "over-limit.txt", "empty.txt"})
	want := map[string]string{"nul-inside.txt": ProfileReasonBinary, "nul-outside.txt": "", "at-limit.txt": "",
		"over-limit.txt": ProfileReasonOversize, "empty.txt": ProfileReasonEmpty}
	for name, reason := range want {
		if got := profiles[name].Reason; got != reason {
			t.Fatalf("%s: reason %q, want %q", name, got, reason)
		}
	}
}
