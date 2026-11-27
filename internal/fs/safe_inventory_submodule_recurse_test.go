package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestSafeInventoryRecurseSubmodulesPrunesAWhollyExcludedSubmodule(t *testing.T) {
	root := submoduleFixture(t)
	asked := []string{}
	prune := func(rel string) (string, bool) {
		asked = append(asked, rel)
		return "managed_scope_rule:defer-svc", rel == "svc"
	}
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true, PruneSubmodule: prune})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "svc" {
		t.Fatalf("the pruner is asked once per initialized submodule: %v", asked)
	}
	for _, path := range append(append([]string{}, report.ManagedCandidates...), report.TrackedPaths...) {
		if path == "svc" || strings.HasPrefix(path, "svc/") {
			t.Fatalf("a pruned submodule lists nothing, not even its gitlink: %s", path)
		}
	}
	if exclusionSource(report, "svc/") != "managed_scope_rule:defer-svc" || exclusionCategory(report, "svc/") != SafetyConfigured {
		t.Fatalf("a pruned submodule is one configured exclusion line: %#v", report.Exclusions)
	}
	if report.Summary.ChildRepositories != 0 {
		t.Fatalf("a pruned submodule is not read as a child repository: %v", report.ChildRepositoryRoots)
	}
	plain, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Summary.RulesIdentity != report.Summary.RulesIdentity {
		t.Fatal("pruning follows policy bound elsewhere and never enters the rules identity")
	}
}

func TestSafeInventoryRecurseSubmodulesKeepsASubmoduleWithAnOptInBelowIt(t *testing.T) {
	root := submoduleFixture(t)
	prune := func(rel string) (string, bool) { return "managed_scope_rule:defer-svc", true }
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true, PruneSubmodule: prune, HighRiskOptIn: []string{"svc/.env"}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(report.ManagedCandidates, "svc/.env") || report.Summary.ChildRepositories != 1 {
		t.Fatalf("an exact opt-in below a submodule keeps it listed for approval accounting: %#v", report.ManagedCandidates)
	}
}

// Review of #215: `git status` compares a tracked file whose stat moved
// against the index by reading it, so an edited `.env` placeholder inside a
// submodule was opened although Safe Inventory excludes it. Discovery runs
// path-only queries; a git wrapper on PATH records every subcommand and the
// test refuses any that can read a tracked file's content.
func TestSafeInventoryRecurseSubmodulesRunsOnlyPathOnlyGitQueries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the PATH wrapper is a POSIX shell script")
	}
	root := submoduleFixture(t)
	gitCommand(t, filepath.Join(root, "svc"), "add", "-f", ".env")
	gitCommand(t, filepath.Join(root, "svc"), "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "placeholder")
	// Same size, new bytes and mtime: exactly the file git status re-reads.
	mustWrite(t, root, "svc/.env", "SECRET=2\n")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	wrapper := "#!/bin/sh\nfor arg in \"$@\"; do case \"$arg\" in status|diff|diff-files|diff-index|update-index) echo \"$arg\" >> '" + logPath + "';; esac; done\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true, IncludeIgnoredCandidates: true})
	if err != nil {
		t.Fatal(err)
	}
	if exclusionCategory(report, "svc/.env") != SafetySensitive {
		t.Fatalf("the placeholder stays a sensitive exclusion: %#v", report.Exclusions)
	}
	if data, _ := os.ReadFile(logPath); len(data) != 0 {
		t.Fatalf("discovery ran a git command that reads tracked content: %s", data)
	}
}

// Review of #215: with only the outer submodule initialized, a nested
// gitlink lost its boundary because every nested gitlink was dropped from the
// tracked list before recursion. Only expanded gitlinks are dropped now.
func TestSafeInventoryRecurseSubmodulesKeepsANestedUninitializedGitlinkABoundary(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "leaf")
	committedRepository(t, leaf, map[string]string{"leaf.go": "package leaf\n"})
	middle := filepath.Join(t.TempDir(), "middle")
	committedRepository(t, middle, map[string]string{"middle.go": "package middle\n"})
	addSubmodule(t, middle, leaf, "deps/leaf")
	root := filepath.Join(t.TempDir(), "super")
	committedRepository(t, root, map[string]string{"README.md": "# super\n"})
	addSubmodule(t, root, middle, "middle")
	// Only the outer submodule: middle/deps/leaf stays uninitialized.
	report, err := BuildSafeInventory(root, WalkOptions{RecurseSubmodules: true})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(report.ManagedCandidates, "middle/middle.go") {
		t.Fatalf("the outer submodule is read: %#v", report.ManagedCandidates)
	}
	if exclusionSource(report, "middle/deps/leaf") != "git_submodule" {
		t.Fatalf("a nested uninitialized gitlink stays a boundary, as at the top level: %#v", report.Exclusions)
	}
	if strings.Join(report.ChildRepositoryRoots, ",") != "middle" {
		t.Fatalf("only the outer submodule is a child repository: %v", report.ChildRepositoryRoots)
	}
}

// Review of #215: a submodule under an exclude_root_dirs directory still had
// its Git started, so stale metadata there failed the inventory. A hard
// excluded submodule is one exclusion line and its Git never runs.
func TestSafeInventoryRecurseSubmodulesNeverStartsGitInAHardExcludedSubmodule(t *testing.T) {
	source := filepath.Join(t.TempDir(), "svc")
	committedRepository(t, source, map[string]string{"app/main.go": "package main\n"})
	root := filepath.Join(t.TempDir(), "super")
	committedRepository(t, root, map[string]string{"README.md": "# super\n"})
	addSubmodule(t, root, source, "vendor/svc")
	// Stale metadata: a .git file naming a gitdir that no longer exists.
	mustWrite(t, root, "vendor/svc/.git", "gitdir: ../../.git/modules/gone\n")
	options := WalkOptions{RecurseSubmodules: true, ExcludeRootDirs: []string{"vendor"}}
	report, err := BuildSafeInventory(root, options)
	if err != nil {
		t.Fatalf("a hard-excluded submodule must not be queried: %v", err)
	}
	if exclusionSource(report, "vendor/svc/") != "exclude_root_dirs:vendor" || exclusionCategory(report, "vendor/svc/") != SafetyConfigured {
		t.Fatalf("a hard-excluded submodule is one configured exclusion line: %#v", report.Exclusions)
	}
	for _, path := range report.ManagedCandidates {
		if strings.HasPrefix(path, "vendor/") {
			t.Fatalf("nothing below a hard-excluded submodule is listed: %s", path)
		}
	}
}
