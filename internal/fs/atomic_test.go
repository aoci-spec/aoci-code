// Regression coverage for the AtomicWrite Windows fallback rollback semantics.
package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A missing temp file reliably makes the retry fail after the target is backed up.
func TestWindowsRenameFallbackRestoresTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	tmp := filepath.Join(root, ".aoci-tmp-missing")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := windowsRenameFallback(tmp, target); err == nil {
		t.Fatal("fallback should fail when the temp file is missing")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old" {
		t.Fatalf("old target not restored after retry failure: content=%q err=%v", got, err)
	}
}

func TestWindowsRenameFallbackRetainsRecoveryFilesWhenRestoreFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	tmp := filepath.Join(root, ".aoci-tmp-prepared")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	retryErr := errors.New("retry failed")
	restoreErr := errors.New("restore failed")
	renameCalls := 0
	rename := func(oldPath, newPath string) error {
		renameCalls++
		switch renameCalls {
		case 1:
			return os.Rename(oldPath, newPath)
		case 2:
			return retryErr
		default:
			return restoreErr
		}
	}

	if err := windowsRenameFallbackWithRename(tmp, target, rename); !errors.Is(err, restoreErr) {
		t.Fatalf("fallback error = %v, want restore error", err)
	}
	if renameCalls != 3 {
		t.Fatalf("rename calls = %d, want 3", renameCalls)
	}
	for path, want := range map[string]string{
		tmp:          "new",
		tmp + ".bak": "old",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("recovery file %s: content=%q err=%v, want %q", path, got, err, want)
		}
	}
}
