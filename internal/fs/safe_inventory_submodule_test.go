package fs

import (
	"testing"
)

// #107: a git submodule is its own repository. The superproject tracks only
// its commit (a gitlink), so nothing under it is a candidate, and the gitlink
// itself is reported as a submodule boundary rather than an unsafe object.
func TestSafeInventoryGitSubmoduleIsABoundary(t *testing.T) {
	sub := t.TempDir()
	gitCommand(t, sub, "init", "-q")
	mustWrite(t, sub, "lib.go", "package lib\n")
	gitCommand(t, sub, "add", "lib.go")
	gitCommand(t, sub, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "sub")

	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, "main.go", "package main\n")
	gitCommand(t, root, "add", "main.go")
	gitCommand(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "super")
	gitCommand(t, root, "-c", "protocol.file.allow=always", "-c", "user.email=t@t", "-c", "user.name=t", "submodule", "add", "-q", sub, "vendor_sub")

	report, err := BuildSafeInventory(root, WalkOptions{IncludeIgnoredCandidates: true})
	if err != nil {
		t.Fatal(err)
	}
	if exclusionSource(report, "vendor_sub") != "git_submodule" || exclusionCategory(report, "vendor_sub") != SafetyUnsafe {
		t.Fatalf("the gitlink must be reported as a submodule boundary: %#v", report.Exclusions)
	}
	for _, path := range report.ManagedCandidates {
		if path == "vendor_sub" || len(path) > 11 && path[:11] == "vendor_sub/" {
			t.Fatalf("a submodule path became a candidate: %s", path)
		}
	}
	if !containsPath(report.ManagedCandidates, ".gitmodules") || !containsPath(report.ManagedCandidates, "main.go") {
		t.Fatalf("the superproject's own files stay candidates: %#v", report.ManagedCandidates)
	}
}
