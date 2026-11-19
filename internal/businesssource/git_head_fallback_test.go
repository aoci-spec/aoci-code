package businesssource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func TestManifestGitHeadUsesGitForUnsupportedLooseRef(t *testing.T) {
	root := testkit.GitRepo(t)
	testkit.WriteFile(t, root, "main.go", "package main\n")
	testkit.Commit(t, root, "packed")
	packed := testkit.Git(t, root, "rev-parse", "HEAD")
	testkit.Git(t, root, "pack-refs", "--all")
	testkit.Git(t, root, "commit", "--allow-empty", "-qm", "loose")
	current := testkit.Git(t, root, "rev-parse", "HEAD")
	if current == packed {
		t.Fatal("fixture: loose and packed refs must differ")
	}
	want, err := Build(root, "2026-11-19T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".git", "refs", "heads", "main")
	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("current", path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := testkit.Git(t, root, "rev-parse", "HEAD"); got != current {
		t.Fatalf("fixture: Git resolves %s, want %s", got, current)
	}
	got, err := Build(root, "2026-11-19T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got.GitHead != current || got.AggregateSHA256 != want.AggregateSHA256 {
		t.Fatalf("same files and HEAD must preserve manifest identity: got head=%s aggregate=%s; want head=%s aggregate=%s (stale packed=%s)", got.GitHead, got.AggregateSHA256, current, want.AggregateSHA256, packed)
	}
}
