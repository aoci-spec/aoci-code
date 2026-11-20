package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitHeadWithStalePackedRef(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	gitCommand(t, root, "init", "-q", "-b", "main")
	gitCommand(t, root, "commit", "--allow-empty", "-qm", "packed")
	packed := gitOutput(t, root, "rev-parse", "HEAD")
	gitCommand(t, root, "pack-refs", "--all")
	gitCommand(t, root, "commit", "--allow-empty", "-qm", "loose")
	if current := gitOutput(t, root, "rev-parse", "HEAD"); current == packed {
		t.Fatal("fixture: loose and packed refs must differ")
	}
	return root, packed
}

func TestGitHeadFastDoesNotFallBackPastExistingLooseRef(t *testing.T) {
	for _, shape := range []string{"symlink", "directory", "oversized"} {
		t.Run(shape, func(t *testing.T) {
			root, packed := gitHeadWithStalePackedRef(t)
			path := filepath.Join(root, ".git", "refs", "heads", "main")
			current := gitOutput(t, root, "rev-parse", "HEAD")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "symlink":
				mustWrite(t, root, ".git/refs/heads/current", current+"\n")
				if err := os.Symlink("current", path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if got := gitOutput(t, root, "rev-parse", "HEAD"); got != current {
					t.Fatalf("fixture: Git resolves %s, want current %s", got, current)
				}
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				mustWrite(t, root, ".git/refs/heads/main", current+strings.Repeat("\n", gitSmallFileLimit))
			}
			if got, ok := GitHeadFast(root); ok || got != "" {
				t.Fatalf("unsupported loose ref must defer to Git, not stale packed ref %s: got %q ok=%v", packed, got, ok)
			}
		})
	}
}

func TestGitHeadFastDoesNotIgnoreUnsupportedCommonDir(t *testing.T) {
	for _, shape := range []string{"symlink", "directory", "oversized"} {
		t.Run(shape, func(t *testing.T) {
			root, _ := gitHeadWithStalePackedRef(t)
			path := filepath.Join(root, ".git", "commondir")
			switch shape {
			case "symlink":
				mustWrite(t, root, ".git/common-target", ".\n")
				if err := os.Symlink("common-target", path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				mustWrite(t, root, ".git/commondir", strings.Repeat("x", gitSmallFileLimit+1))
			}
			if got, ok := GitHeadFast(root); ok || got != "" {
				t.Fatalf("unsupported commondir must defer to Git: got %q ok=%v", got, ok)
			}
		})
	}
}
