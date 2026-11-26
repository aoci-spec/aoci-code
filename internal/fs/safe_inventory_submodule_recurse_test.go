package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// committedRepository creates a Git repository at dir holding files and
// commits them.
func committedRepository(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "init", "-q")
	for rel, body := range files {
		mustWrite(t, dir, rel, body)
		gitCommand(t, dir, "add", "-f", rel)
	}
	gitCommand(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
}

func addSubmodule(t *testing.T, root, source, rel string) {
	t.Helper()
	gitCommand(t, root, "-c", "protocol.file.allow=always", "-c", "user.email=t@t", "-c", "user.name=t", "submodule", "add", "-q", source, rel)
	gitCommand(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "add "+rel)
}

func submoduleFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "svc")
	committedRepository(t, source, map[string]string{
		"app/main.go": "package main\n",
		".gitignore":  "dist/\n",
	})
	root := filepath.Join(t.TempDir(), "super")
	committedRepository(t, root, map[string]string{"README.md": "# super\n"})
	addSubmodule(t, root, source, "svc")
	// Untracked files inside the submodule: one ignored by the submodule's
	// own .gitignore, one sensitive by name, one ordinary.
	mustWrite(t, root, "svc/dist/bundle.js", "x\n")
	mustWrite(t, root, "svc/.env", "SECRET=1\n")
	mustWrite(t, root, "svc/app/new.go", "package main\n")
	return root
}

func TestSafeInventoryRecurseSubmodulesDefaultKeepsTheBoundary(t *testing.T) {
	root := submoduleFixture(t)
	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if exclusionSource(report, "svc") != "git_submodule" {
		t.Fatalf("without the switch the gitlink stays a boundary: %#v", report.Exclusions)
	}
	for _, path := range report.ManagedCandidates {
		if strings.HasPrefix(path, "svc/") {
			t.Fatalf("a submodule path became a candidate without the switch: %s", path)
		}
	}
}

func TestSafeInventoryRecurseSubmodulesReadsTheSubmoduleThroughItsOwnGit(t *testing.T) {
	root := submoduleFixture(t)
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"README.md", ".gitmodules", "svc/app/main.go", "svc/.gitignore", "svc/app/new.go"} {
		if !containsPath(report.ManagedCandidates, want) {
			t.Fatalf("%s must be a candidate: %#v", want, report.ManagedCandidates)
		}
	}
	if containsPath(report.ManagedCandidates, "svc") {
		t.Fatal("a recursed gitlink is a directory, never a candidate of its own")
	}
	if !containsPath(report.TrackedPaths, "svc/app/main.go") || containsPath(report.TrackedPaths, "svc/app/new.go") {
		t.Fatalf("tracked state comes from the submodule's own Git: %#v", report.TrackedPaths)
	}
	for _, path := range report.ManagedCandidates {
		if strings.HasPrefix(path, "svc/dist/") {
			t.Fatalf("the submodule's .gitignore must hold: %s", path)
		}
	}
	if containsPath(report.ManagedCandidates, "svc/.env") || exclusionCategory(report, "svc/.env") != SafetySensitive {
		t.Fatalf("a sensitive name inside a submodule stays a hard exclusion: %#v", report.Exclusions)
	}
	if exclusionCategory(report, "svc/.git") == "" {
		t.Fatalf("the submodule's .git file stays excluded: %#v", report.Exclusions)
	}
	if report.Summary.ChildRepositories != 1 || strings.Join(report.ChildRepositoryRoots, ",") != "svc" {
		t.Fatalf("the submodule is reported as a child repository: %v", report.ChildRepositoryRoots)
	}
}

func TestSafeInventoryRecurseSubmodulesLeavesAnUninitializedGitlinkABoundary(t *testing.T) {
	root := submoduleFixture(t)
	gitCommand(t, root, "submodule", "deinit", "-q", "-f", "svc")
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	if exclusionSource(report, "svc") != "git_submodule" {
		t.Fatalf("an uninitialized submodule stays a boundary: %#v", report.Exclusions)
	}
	if report.Summary.ChildRepositories != 0 {
		t.Fatalf("an uninitialized submodule is not read: %v", report.ChildRepositoryRoots)
	}
}

func TestSafeInventoryRecurseSubmodulesReadsNestedSubmodules(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "leaf")
	committedRepository(t, leaf, map[string]string{"leaf.go": "package leaf\n"})
	middle := filepath.Join(t.TempDir(), "middle")
	committedRepository(t, middle, map[string]string{"middle.go": "package middle\n"})
	addSubmodule(t, middle, leaf, "deps/leaf")
	root := filepath.Join(t.TempDir(), "super")
	committedRepository(t, root, map[string]string{"README.md": "# super\n"})
	addSubmodule(t, root, middle, "middle")
	gitCommand(t, root, "-c", "protocol.file.allow=always", "submodule", "update", "-q", "--init", "--recursive")

	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"middle/middle.go", "middle/.gitmodules", "middle/deps/leaf/leaf.go"} {
		if !containsPath(report.ManagedCandidates, want) {
			t.Fatalf("%s must be a candidate: %#v", want, report.ManagedCandidates)
		}
	}
	if containsPath(report.ManagedCandidates, "middle/deps/leaf") {
		t.Fatal("a nested recursed gitlink is never a candidate of its own")
	}
	if strings.Join(report.ChildRepositoryRoots, ",") != "middle,middle/deps/leaf" {
		t.Fatalf("both levels are child repositories: %v", report.ChildRepositoryRoots)
	}
}

func TestSafeInventoryRecurseSubmodulesNeverEntersALinkedGitlink(t *testing.T) {
	root := submoduleFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	committedRepository(t, outside, map[string]string{"stolen.go": "package stolen\n"})
	work := filepath.Join(root, "svc")
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, work); err != nil {
		t.Skip("symbolic links unavailable:", err)
	}
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range report.ManagedCandidates {
		if strings.HasPrefix(path, "svc/") {
			t.Fatalf("a gitlink replaced by a link was entered: %s", path)
		}
	}
}

func TestSafeInventoryRecurseSubmodulesEntersTheRulesIdentity(t *testing.T) {
	root := submoduleFixture(t)
	off, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	on, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	if off.Summary.RulesIdentity == on.Summary.RulesIdentity {
		t.Fatal("enabling the switch must change the rules identity")
	}
	if safeInventoryRulesIdentity(WalkOptions{}) != safeInventoryRulesIdentity(WalkOptions{RecurseSubmodules: false}) {
		t.Fatal("an absent switch keeps the pre-#107 identity")
	}
}
