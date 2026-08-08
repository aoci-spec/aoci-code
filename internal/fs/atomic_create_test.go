package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAtomicCreateCASOnlyOneConcurrentPublisherSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created.txt")
	var successes atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 24; index++ {
		wait.Add(1)
		go func(value byte) {
			defer wait.Done()
			err := AtomicCreateCAS(path, []byte{value})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrAtomicCreateConflict):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected AtomicCreateCAS result: %v", err)
			}
		}(byte(index))
	}
	wait.Wait()
	if successes.Load() != 1 || conflicts.Load() != 23 {
		t.Fatalf("success=%d conflict=%d", successes.Load(), conflicts.Load())
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 1 {
		t.Fatalf("published file is incomplete: %q err=%v", data, err)
	}
}

func TestAtomicCreateCASRejectsBoundaryReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	previous := beforeAtomicCreatePublish
	beforeAtomicCreatePublish = func(target string) {
		if err := os.WriteFile(target, []byte("third-party\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeAtomicCreatePublish = previous })
	err := AtomicCreateCAS(path, []byte("ours\n"))
	if !errors.Is(err, ErrAtomicCreateConflict) {
		t.Fatalf("boundary replacement did not conflict: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "third-party\n" {
		t.Fatalf("third-party bytes were overwritten: %q", data)
	}
}

func TestAtomicMoveCASCapturesExpectedPostimage(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	recoveryDir := filepath.Join(root, "recovery")
	if err := os.Mkdir(recoveryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("postimage\n")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	recovery := filepath.Join(recoveryDir, "captured.txt")
	if err := AtomicMoveCAS(source, recovery, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	got, _ := os.ReadFile(recovery)
	if string(got) != string(data) {
		t.Fatalf("captured bytes changed: %q", got)
	}
}

func TestAtomicCreateCASRejectsUnsafeTypesAndParentSymlink(t *testing.T) {
	root := t.TempDir()
	directoryTarget := filepath.Join(root, "target")
	if err := os.Mkdir(directoryTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicCreateCAS(directoryTarget, []byte("x")); !errors.Is(err, ErrAtomicCreateConflict) {
		t.Fatalf("directory target was accepted: %v", err)
	}
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := AtomicCreateCAS(filepath.Join(linkedParent, "file.txt"), []byte("x")); err == nil {
		t.Fatal("parent symlink was accepted")
	}
}

func TestAtomicCreateCASLongSameDirectoryPath(t *testing.T) {
	root := t.TempDir()
	current := root
	for index := 0; index < 8; index++ {
		current = filepath.Join(current, "segment-0123456789abcdef")
		if err := os.Mkdir(current, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(current, "target.txt")
	if err := AtomicCreateCAS(path, []byte("complete\n")); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicCreateCASNoFallbackWhenPrimitiveUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	previous := publishAtomicCreatePlatform
	publishAtomicCreatePlatform = func(_, _ string) error { return os.ErrInvalid }
	t.Cleanup(func() { publishAtomicCreatePlatform = previous })
	err := AtomicCreateCAS(path, []byte("must-not-publish\n"))
	if !errors.Is(err, ErrAtomicCreateUnavailable) {
		t.Fatalf("unavailable primitive did not fail closed: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("target appeared through a fallback: %v", err)
	}
}
