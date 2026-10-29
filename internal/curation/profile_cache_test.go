package curation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A profile built from the fingerprint cache equals one built from the bytes
// for every class the profile can answer: empty, text with lines, binary, and
// oversize; the second batch over a settled tree reads nothing.
func TestProfilesAgreeAcrossTheFingerprintCache(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"empty.txt":  {},
		"text.go":    []byte("package main\n\nfunc main() {}\n"),
		"nolf.txt":   []byte("one\ntwo"),
		"binary.bin": append([]byte("GIF89a"), 0, 1, 2, 3),
		"big.bin":    bytes.Repeat([]byte("x"), OversizeBytes+1),
		"late.bin":   append(bytes.Repeat([]byte("y"), BinarySniffBytes+10), 0),
	}
	old := time.Now().Add(-10 * time.Second)
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"empty.txt", "text.go", "nolf.txt", "binary.bin", "big.bin", "late.bin"}
	cold := BuildProfiles(root, paths)
	warm := BuildProfiles(root, paths)
	want := map[string]string{"empty.txt": ProfileReasonEmpty, "text.go": "", "nolf.txt": "", "binary.bin": ProfileReasonBinary, "big.bin": ProfileReasonOversize, "late.bin": ""}
	for _, rel := range paths {
		if cold[rel] != warm[rel] {
			t.Fatalf("%s: cold %+v, cached %+v", rel, cold[rel], warm[rel])
		}
		if cold[rel].Reason != want[rel] {
			t.Fatalf("%s: reason %q, want %q", rel, cold[rel].Reason, want[rel])
		}
	}
	if cold["text.go"].Lines != 3 || cold["nolf.txt"].Lines != 2 || cold["late.bin"].Lines != 1 {
		t.Fatalf("line counts: text=%d nolf=%d late=%d", cold["text.go"].Lines, cold["nolf.txt"].Lines, cold["late.bin"].Lines)
	}
}
