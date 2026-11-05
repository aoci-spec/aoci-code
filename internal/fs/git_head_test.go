package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output))
}

// The fast reader must answer exactly what git answers for every shape it
// claims to understand: a loose branch, packed refs, a detached HEAD, and a
// linked worktree; and decline the shapes it does not.
func TestGitHeadFastMatchesGitAcrossRefShapes(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q", "-b", "main")
	if head, ok := GitHeadFast(root); ok {
		t.Fatalf("an unborn branch has no HEAD to answer: %s", head)
	}
	mustWrite(t, root, "a.txt", "a\n")
	gitCommand(t, root, "add", "a.txt")
	gitCommand(t, root, "commit", "-qm", "first")
	want := gitOutput(t, root, "rev-parse", "HEAD")
	if got, ok := GitHeadFast(root); !ok || got != want {
		t.Fatalf("loose ref: got %q ok=%v want %q", got, ok, want)
	}
	gitCommand(t, root, "pack-refs", "--all")
	if _, err := os.Lstat(filepath.Join(root, ".git", "refs", "heads", "main")); err == nil {
		t.Fatal("fixture: the branch should be packed now")
	}
	if got, ok := GitHeadFast(root); !ok || got != want {
		t.Fatalf("packed ref: got %q ok=%v want %q", got, ok, want)
	}
	mustWrite(t, root, "b.txt", "b\n")
	gitCommand(t, root, "add", "b.txt")
	gitCommand(t, root, "commit", "-qm", "second")
	want = gitOutput(t, root, "rev-parse", "HEAD")
	if got, ok := GitHeadFast(root); !ok || got != want {
		t.Fatalf("loose ref beside packed-refs: got %q ok=%v want %q", got, ok, want)
	}
	gitCommand(t, root, "checkout", "-q", "--detach", "HEAD~1")
	want = gitOutput(t, root, "rev-parse", "HEAD")
	if got, ok := GitHeadFast(root); !ok || got != want {
		t.Fatalf("detached HEAD: got %q ok=%v want %q", got, ok, want)
	}
	gitCommand(t, root, "checkout", "-q", "main")
	worktree := filepath.Join(t.TempDir(), "wt")
	gitCommand(t, root, "worktree", "add", "-q", "-b", "feature", worktree, "main")
	mustWrite(t, worktree, "c.txt", "c\n")
	gitCommand(t, worktree, "add", "c.txt")
	gitCommand(t, worktree, "commit", "-qm", "feature")
	want = gitOutput(t, worktree, "rev-parse", "HEAD")
	if got, ok := GitHeadFast(worktree); !ok || got != want {
		t.Fatalf("linked worktree: got %q ok=%v want %q", got, ok, want)
	}
	if mainWant := gitOutput(t, root, "rev-parse", "HEAD"); mainWant == want {
		t.Fatal("fixture: the worktree should be ahead of main")
	}
}

func TestGitHeadFastDeclinesWhatItDoesNotUnderstand(t *testing.T) {
	if head, ok := GitHeadFast(t.TempDir()); ok {
		t.Fatalf("no repository: %s", head)
	}
	root := t.TempDir()
	gitCommand(t, root, "init", "-q", "-b", "main")
	mustWrite(t, root, "a.txt", "a\n")
	gitCommand(t, root, "add", "a.txt")
	gitCommand(t, root, "commit", "-qm", "first")
	mustWrite(t, root, ".git/HEAD", "ref: refs/heads/../../etc/passwd\n")
	if head, ok := GitHeadFast(root); ok {
		t.Fatalf("a traversing ref must be declined: %s", head)
	}
	mustWrite(t, root, ".git/HEAD", "not a ref at all\n")
	if head, ok := GitHeadFast(root); ok {
		t.Fatalf("a malformed HEAD must be declined: %s", head)
	}
	linked := t.TempDir()
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(linked, ".git")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if head, ok := GitHeadFast(linked); ok {
		t.Fatalf("a symlinked .git must be declined: %s", head)
	}
}
