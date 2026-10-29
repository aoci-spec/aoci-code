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

// ProfilePath is pure and a Profiler touches the cache once per batch. The
// first draft opened and saved the whole cache inside ProfilePath, so a
// Legacy manifest over N files decoded and rewrote N entries N times; the
// migration benchmark at 100 objects went from 0.3 s to 3.7 s and the
// 1,000-object fixtures past the job's thirty minutes.
func TestProfilePathIsPureAndProfilerSavesOncePerBatch(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, ".aoci", "fingerprints.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, 40)
	old := time.Now().Add(-10 * time.Second)
	for index := 0; index < 40; index++ {
		rel := filepath.Join("src", "f"+string(rune('a'+index%26))+string(rune('0'+index/26))+".go")
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n// "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	for _, rel := range paths {
		if _, err := ProfilePath(root, rel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("ProfilePath touched the fingerprint cache: stat err=%v", err)
	}
	profiler := OpenProfiler(root)
	for _, rel := range paths {
		if _, err := profiler.Path(rel); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
			t.Fatalf("the profiler saved before Close, during %s", rel)
		}
	}
	profiler.Close()
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("Close did not save the cache: %v", err)
	}
	for _, rel := range paths {
		if !bytes.Contains(data, []byte(`"`+rel+`"`)) {
			t.Fatalf("saved cache lacks %s", rel)
		}
	}
	// A pure ProfilePath over an existing cache leaves its bytes alone.
	for _, rel := range paths {
		if _, err := ProfilePath(root, rel); err != nil {
			t.Fatal(err)
		}
	}
	if after, err := os.ReadFile(cachePath); err != nil || !bytes.Equal(after, data) {
		t.Fatalf("ProfilePath rewrote the cache (err=%v, changed=%v)", err, !bytes.Equal(after, data))
	}
}
