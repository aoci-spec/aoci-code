package fs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func gitCommand(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=AOCI Test", "GIT_AUTHOR_EMAIL=aoci@example.invalid",
		"GIT_COMMITTER_NAME=AOCI Test", "GIT_COMMITTER_EMAIL=aoci@example.invalid", "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(arguments, " "), err, output)
	}
}

func exclusionCategory(report *SafeInventory, path string) string {
	for _, exclusion := range report.Exclusions {
		if exclusion.PathSummary == path {
			return exclusion.Category
		}
	}
	return ""
}

func TestSafeInventoryGitAwareBoundaries(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".gitignore", ".env\n.runtime/\ndist/\n")
	mustWrite(t, root, "src/tracked.go", "package source\n")
	mustWrite(t, root, "tracked.pem", "must never be inventoried as content\n")
	mustWrite(t, root, "package-lock.json", "{}\n")
	gitCommand(t, root, "add", ".gitignore", "src/tracked.go", "tracked.pem", "package-lock.json")
	mustWrite(t, root, "src/new.go", "package source\r\n")
	mustWrite(t, root, ".env", "SECRET=redacted\n")
	mustWrite(t, root, ".runtime/mysql/data/file.ibd", "runtime\n")
	mustWrite(t, root, "dist/app.js", "generated\n")

	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	managed := map[string]bool{}
	for _, path := range report.ManagedCandidates {
		managed[path] = true
	}
	for _, path := range []string{".gitignore", "src/tracked.go", "src/new.go", "package-lock.json"} {
		if !managed[path] {
			t.Fatalf("expected managed path %s: %#v", path, report)
		}
	}
	for _, path := range []string{"tracked.pem", ".env", ".runtime/mysql/data/file.ibd", "dist/app.js"} {
		if managed[path] {
			t.Fatalf("unsafe path became managed: %s", path)
		}
	}
	if exclusionCategory(report, "tracked.pem") != SafetySensitive || report.Summary.RequiredHumanReview != 1 ||
		report.Summary.ReviewVisibleCount != 1 || report.Summary.AutoBlockerCount != 0 {
		t.Fatalf("tracked sensitive file did not fail closed visibly: %#v", report)
	}
	if report.Summary.Ignored != 3 || report.Summary.BuiltinSensitiveExcluded < 2 || report.Summary.RuntimeExcluded < 1 || report.Summary.GeneratedExcluded < 1 {
		t.Fatalf("ignored safety summary incomplete: %#v", report.Summary)
	}
	if report.Summary.NonignoredUntracked != 1 {
		t.Fatalf("new source must remain eligible: %#v", report.Summary)
	}
	optedIn, err := BuildSafeInventory(root, WalkOptions{HighRiskOptIn: []string{".env"}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(optedIn.ManagedCandidates, ".env") || optedIn.Summary.RequiredHumanReview != 2 ||
		optedIn.Summary.ReviewVisibleCount != 2 || optedIn.Summary.AutoBlockerCount != 1 {
		t.Fatalf("exact ignored sensitive opt-in was not visible and review-bound: %#v", optedIn)
	}
	if _, err := BuildSafeInventory(root, WalkOptions{HighRiskOptIn: []string{".runtime/mysql/data/file.ibd"}}); err == nil || err.Error() != "safe_inventory_high_risk_opt_in_forbidden" {
		t.Fatalf("runtime boundary was opt-in eligible: %v", err)
	}
}

func TestSameGitRootPathNormalizesWindowsGitOutput(t *testing.T) {
	if !sameGitRootPath(`D:/work/aoci`, `d:\work\aoci`, "windows") {
		t.Fatal("Windows Git and native path spellings must identify the same root")
	}
	if sameGitRootPath(`D:/work/foreign`, `D:\work\aoci`, "windows") {
		t.Fatal("a foreign Git root must not pass boundary validation")
	}
	root := t.TempDir()
	alias := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-alias")
	if err := os.Symlink(root, alias); err == nil {
		if !sameGitRootPath(alias, root, "windows") {
			t.Fatal("two Windows path aliases for one directory must pass file-identity validation")
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
}

func TestSafeInventoryRejectsUnverifiableGitBoundary(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".git/HEAD", "not a repository\n")
	mustWrite(t, root, "src/main.go", "package main\n")

	_, err := BuildSafeInventory(root, WalkOptions{})
	if err == nil || err.Error() != "safe_inventory_git_query_failed" {
		t.Fatalf("an unverifiable Git boundary must fail closed: %v", err)
	}
}

func containsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}

func TestSafeInventoryNonGitAndHighRiskOptIn(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "src/main.go", "package main\n")
	mustWrite(t, root, ".env", "SECRET=redacted\n")
	mustWrite(t, root, ".env.example", "SECRET=\n")
	mustWrite(t, root, ".runtime/cache/state.db", "runtime\n")
	mustWrite(t, root, "node_modules/pkg/index.js", "generated\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "src", "main.go"), filepath.Join(root, "source-link")); err != nil {
			t.Fatal(err)
		}
	}

	report, err := BuildSafeInventory(root, WalkOptions{HighRiskOptIn: []string{".env"}})
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.Join(report.ManagedCandidates, "\n")
	for _, path := range []string{"src/main.go", ".env", ".env.example"} {
		if !strings.Contains(managed, path) {
			t.Fatalf("expected managed path %s: %#v", path, report)
		}
	}
	if report.Summary.RequiredHumanReview != 1 || report.Summary.ReviewVisibleCount != 1 || report.Summary.AutoBlockerCount != 1 ||
		report.Summary.RuntimeExcluded != 1 || report.Summary.GeneratedExcluded != 1 {
		t.Fatalf("non-Git safety summary unexpected: %#v", report.Summary)
	}
	if runtime.GOOS != "windows" && exclusionCategory(report, "source-link") != SafetyUnsafe {
		t.Fatalf("symlink must not be followed: %#v", report.Exclusions)
	}
	if _, err := BuildSafeInventory(root, WalkOptions{HighRiskOptIn: []string{"*.pem"}}); err == nil || err.Error() != "safe_inventory_high_risk_opt_in_invalid" {
		t.Fatalf("glob opt-in must fail closed: %v", err)
	}
}

func TestSafeInventoryNonGitRootIncludesNestedRepositories(t *testing.T) {
	root := t.TempDir()
	for _, service := range []string{"svc-a", "svc-b"} {
		serviceRoot := filepath.Join(root, service)
		if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		gitCommand(t, serviceRoot, "init", "-q")
		mustWrite(t, root, service+"/main.go", "package main\n")
	}

	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.GitRepository {
		t.Fatal("a non-Git container must keep the traversal inventory path")
	}
	for _, path := range []string{"svc-a/main.go", "svc-b/main.go"} {
		if !containsPath(report.ManagedCandidates, path) {
			t.Fatalf("nested repository source %s was not inventoried: %#v", path, report.ManagedCandidates)
		}
	}
	for _, path := range []string{"svc-a/.git", "svc-b/.git"} {
		if exclusionCategory(report, path) != SafetyGenerated {
			t.Fatalf("nested repository metadata %s did not keep its VCS exclusion: %#v", path, report.Exclusions)
		}
	}
	for _, path := range report.ManagedCandidates {
		if strings.Contains(path, "/.git/") || strings.HasSuffix(path, "/.git") {
			t.Fatalf("nested repository metadata became managed: %s", path)
		}
	}
}

func TestSafeInventoryNonGitRootReadsNoChildGitignore(t *testing.T) {
	root := t.TempDir()
	serviceRoot := filepath.Join(root, "svc-a")
	if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, serviceRoot, "init", "-q")
	// The probe name is one no built-in rule touches: a name such as build/
	// is pruned by the safety boundary and would look like an honored ignore.
	mustWrite(t, root, "svc-a/.gitignore", "local-notes.txt\n")
	mustWrite(t, root, "svc-a/local-notes.txt", "ignored by the child repository only\n")
	mustWrite(t, root, "svc-a/main.go", "package main\n")

	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"svc-a/main.go", "svc-a/local-notes.txt"} {
		if !containsPath(report.ManagedCandidates, path) {
			t.Fatalf("docs/managed-scope-and-budget.md says a child .gitignore has no authority under a non-Git root, but %s is missing: %#v", path, report.ManagedCandidates)
		}
	}

	// The remedy that document names has to work where the ignore file does not.
	report, err = BuildSafeInventory(root, WalkOptions{ExcludeFiles: []string{"local-notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(report.ManagedCandidates, "svc-a/local-notes.txt") || exclusionCategory(report, "svc-a/local-notes.txt") != SafetyConfigured {
		t.Fatalf("exclude_files did not keep the path out: %#v", report.Exclusions)
	}
	if !containsPath(report.ManagedCandidates, "svc-a/main.go") {
		t.Fatalf("exclude_files removed an unrelated source: %#v", report.ManagedCandidates)
	}
}

func TestSafeInventoryExcludeOnlyArtifactsRemainAutoEligible(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"logs/service.log", "cache/item.bin", "build/app.js", "dist/app.js", "coverage/index.html",
		"uploads/blob.bin", "node_modules/pkg/index.js", "vendor/pkg/file.go", "backup/state.json",
		"artifacts/release.bin", "third-party-dist/library.min.js", "storage/.gitkeep",
	} {
		body := "excluded artifact\n"
		if strings.HasSuffix(path, ".gitkeep") {
			body = ""
		}
		mustWrite(t, root, path, body)
	}
	mustWrite(t, root, "src/main.go", "package main\n")
	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.AutoBlockerCount != 0 || report.Summary.FinalManagedCandidates != 1 ||
		!containsPath(report.ManagedCandidates, "src/main.go") {
		t.Fatalf("exclude-only artifacts blocked Fresh Auto: %#v", report)
	}
}

func TestSafeInventoryTrackedGitkeepIsReviewVisibleButNotAutoBlocking(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, "src/main.go", "package main\n")
	mustWrite(t, root, "storage/.gitkeep", "")
	gitCommand(t, root, "add", "src/main.go", "storage/.gitkeep")

	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(report.ManagedCandidates, "storage/.gitkeep") ||
		exclusionCategory(report, "storage/.gitkeep") != SafetyGenerated ||
		report.Summary.ReviewVisibleCount != 1 || report.Summary.RequiredHumanReview != 1 ||
		report.Summary.AutoBlockerCount != 0 {
		t.Fatalf("tracked repository placeholder did not remain audit-only: %#v", report)
	}
}

func TestSafeInventoryCaseFoldConflictAndStableRuntimeIdentity(t *testing.T) {
	caseRoot := t.TempDir()
	mustWrite(t, caseRoot, "Code/A.go", "package code\n")
	mustWrite(t, caseRoot, "code/a.go", "package code\n")
	upperInfo, upperErr := os.Lstat(filepath.Join(caseRoot, "Code", "A.go"))
	lowerInfo, lowerErr := os.Lstat(filepath.Join(caseRoot, "code", "a.go"))
	caseSensitiveFixture := upperErr == nil && lowerErr == nil && !os.SameFile(upperInfo, lowerInfo)
	if !caseSensitiveFixture {
		seen := map[string]string{}
		if err := recordCasefoldPath(seen, "Code/A.go"); err != nil {
			t.Fatal(err)
		}
		if err := recordCasefoldPath(seen, "code/a.go"); err == nil || err.Error() != "safe_inventory_casefold_conflict" {
			t.Fatalf("case-fold collision must fail closed: %v", err)
		}
	} else {
		if _, err := BuildSafeInventory(caseRoot, WalkOptions{}); err == nil || err.Error() != "safe_inventory_casefold_conflict" {
			t.Fatalf("case-fold collision must fail closed: %v", err)
		}
	}

	root := t.TempDir()
	mustWrite(t, root, "src/main.go", "package main\n")
	first, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root, ".aoci/ledger.jsonl", "audit\n")
	second, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.InclusionExclusionIdentity != second.Summary.InclusionExclusionIdentity {
		t.Fatal("runtime audit creation changed managed content identity")
	}
}

func TestSafeInventoryRuntimeAndSecretMatrix(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"service.log", "service.pid", "service.sock", "dump.rdb", "appendonly.aof",
		"mysql-data/table.ibd", "pgdata/PG_VERSION", ".pm2/dump.pm2", "cache/result.bin",
		"credentials.json", "server.key", ".env.production", ".aoci/evidence/cache.json",
	} {
		mustWrite(t, root, path, "excluded body must not be inventoried\n")
	}
	mustWrite(t, root, "package-lock.json", "{}\n")
	mustWrite(t, root, ".env.template", "TOKEN=\n")
	mustWrite(t, root, ".env.production.example", "TOKEN=\n")
	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.Join(report.ManagedCandidates, "\n")
	if !strings.Contains(managed, "package-lock.json") || !strings.Contains(managed, ".env.template") || !strings.Contains(managed, ".env.production.example") {
		t.Fatalf("policy-controlled lockfile or safe template was hard excluded: %#v", report)
	}
	for _, forbidden := range []string{"service.log", "service.pid", "service.sock", "dump.rdb", "appendonly.aof", "mysql-data/table.ibd", "pgdata/PG_VERSION", "credentials.json", "server.key", ".env.production", ".aoci/evidence/cache.json"} {
		if containsPath(report.ManagedCandidates, forbidden) {
			t.Fatalf("runtime/secret candidate leaked into managed set: %s", forbidden)
		}
	}
	if report.Summary.BuiltinSensitiveExcluded < 3 || report.Summary.RuntimeExcluded < 7 || report.Summary.GeneratedExcluded < 1 {
		t.Fatalf("safety categories not fully counted: %#v", report.Summary)
	}
}

// #97: git used to enumerate every file under an ignored directory on every
// scan, and a pnpm workspace whose node_modules links form a cycle never
// finished on Windows. A git-ignored directory whose every file is a hard
// exclusion anyway (built-in name such as node_modules, or an exclude_dirs
// component) is now one exclusion line; growing it changes nothing else.
func TestSafeInventoryCollapsesHardExcludedIgnoredDirectories(t *testing.T) {
	for _, includeIgnored := range []bool{false, true} {
		root := t.TempDir()
		gitCommand(t, root, "init", "-q")
		mustWrite(t, root, ".gitignore", "node_modules/\n.venv/\n*.local.txt\n")
		mustWrite(t, root, "src/main.go", "package main\n")
		gitCommand(t, root, "add", ".gitignore", "src/main.go")
		mustWrite(t, root, "node_modules/a/b/x.js", "x\n")
		mustWrite(t, root, ".venv/lib/site.py", "x = 1\n")
		mustWrite(t, root, "notes.local.txt", "local notes\n")
		options := WalkOptions{IncludeIgnoredCandidates: includeIgnored, ExcludeDirs: []string{".venv"}}
		before, err := BuildSafeInventory(root, options)
		if err != nil {
			t.Fatal(err)
		}
		if exclusionCategory(before, "node_modules/") != SafetyGenerated || exclusionCategory(before, ".venv/") != SafetyConfigured {
			t.Fatalf("includeIgnored=%v: hard-excluded ignored directories must be one line each with their category: %#v", includeIgnored, before.Exclusions)
		}
		for _, path := range []string{"node_modules/a/b/x.js", ".venv/lib/site.py"} {
			if containsPath(before.ManagedCandidates, path) || containsPath(before.IgnoredPaths, path) || hasExclusion(before, path) {
				t.Fatalf("includeIgnored=%v: %s sits under a collapsed directory and must not be enumerated: %#v", includeIgnored, path, before)
			}
		}
		if includeIgnored != containsPath(before.ManagedCandidates, "notes.local.txt") {
			t.Fatalf("includeIgnored=%v: a loose ignored file keeps its rc17 treatment: %#v", includeIgnored, before)
		}
		for i := 0; i < 300; i++ {
			mustWrite(t, root, fmt.Sprintf("node_modules/pkg%03d/index.js", i), "module.exports = 1\n")
		}
		after, err := BuildSafeInventory(root, options)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after.ManagedCandidates, before.ManagedCandidates) || !reflect.DeepEqual(after.Exclusions, before.Exclusions) ||
			after.Summary.InclusionExclusionIdentity != before.Summary.InclusionExclusionIdentity || after.Summary.Ignored != before.Summary.Ignored {
			t.Fatalf("includeIgnored=%v: growing a collapsed directory changed the inventory: before=%#v after=%#v", includeIgnored, before.Summary, after.Summary)
		}
	}
}

// The collapse may only hide paths that were hard exclusions in rc17. Over a
// tree with every ignored shape, the collapsed inventory and the full rc17
// listing must agree on every candidate, tracked path, ignored candidate and
// selection identity; the exclusion lists may differ only by collapsed lines
// standing in for the per-file lines beneath them.
func TestSafeInventoryCollapseKeepsTheRc17CandidateSet(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".gitignore", "node_modules/\ngen/\nout/\nsecrets/\n*.log\n.env\npublic/\n")
	mustWrite(t, root, "src/main.go", "package main\n")
	mustWrite(t, root, "mixed/keep.txt", "tracked\n")
	gitCommand(t, root, "add", ".gitignore", "src/main.go", "mixed/keep.txt")
	for _, rel := range []string{
		"node_modules/a/b/x.js", "node_modules/c.js", "app/node_modules/d.js", "gen/api.go", "gen/sub/model.go",
		"out/x.go", "secrets/prod.pem", "app/.env", "app/src/x.log", "src/newpkg/y.log", "mixed/b.log",
		"public/app.js.map", "public/fonts/a.woff", "dist/bundle.js",
	} {
		mustWrite(t, root, rel, "content\n")
	}
	for _, options := range []WalkOptions{
		{},
		{IncludeIgnoredCandidates: true},
		{IncludeIgnoredCandidates: true, HighRiskOptIn: []string{"secrets/prod.pem", "app/.env"}},
	} {
		collapseIgnoredDirectories = false
		full, err := BuildSafeInventory(root, options)
		collapseIgnoredDirectories = true
		if err != nil {
			t.Fatal(err)
		}
		collapsed, err := BuildSafeInventory(root, options)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(collapsed.ManagedCandidates, full.ManagedCandidates) ||
			!reflect.DeepEqual(collapsed.TrackedPaths, full.TrackedPaths) ||
			!reflect.DeepEqual(collapsed.IgnoredPaths, full.IgnoredPaths) ||
			collapsed.Summary.InclusionExclusionIdentity != full.Summary.InclusionExclusionIdentity ||
			collapsed.Summary.RulesIdentity != full.Summary.RulesIdentity ||
			collapsed.Summary.AutoBlockerCount != full.Summary.AutoBlockerCount {
			t.Fatalf("options=%#v: collapsing changed the candidate set:\nfull=%#v\ncollapsed=%#v", options, full, collapsed)
		}
		collapsedDirs := []string{}
		for _, exclusion := range collapsed.Exclusions {
			if strings.HasSuffix(exclusion.PathSummary, "/") {
				collapsedDirs = append(collapsedDirs, exclusion.PathSummary)
			}
		}
		if !reflect.DeepEqual(collapsedDirs, []string{"app/node_modules/", "node_modules/"}) {
			t.Fatalf("only the built-in ignored directories may collapse: %v", collapsedDirs)
		}
		for _, exclusion := range full.Exclusions {
			under := false
			for _, dir := range collapsedDirs {
				under = under || strings.HasPrefix(exclusion.PathSummary, dir)
			}
			if !under && !hasExclusion(collapsed, exclusion.PathSummary) {
				t.Fatalf("an exclusion outside the collapsed directories disappeared: %#v", exclusion)
			}
			if under && exclusion.Category != SafetyGenerated {
				t.Fatalf("a path under a collapsed directory was not a hard exclusion in the full listing: %#v", exclusion)
			}
		}
		if options.HighRiskOptIn != nil && (!containsPath(collapsed.ManagedCandidates, "secrets/prod.pem") || !containsPath(collapsed.ManagedCandidates, "app/.env")) {
			t.Fatalf("an opt-in inside an ignored, non-built-in directory must still work: %#v", collapsed.ManagedCandidates)
		}
	}
}

// Ignored files inside untracked, non-ignored directories are listed one by
// one, as before; `--directory --no-empty-directory` had hidden them.
func TestSafeInventoryListsIgnoredFilesInsideUntrackedDirectories(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".gitignore", "*.log\n")
	gitCommand(t, root, "add", ".gitignore")
	mustWrite(t, root, "app/src/x.log", "log\n")
	mustWrite(t, root, "src/newpkg/y.log", "log\n")
	report, err := BuildSafeInventory(root, WalkOptions{IncludeIgnoredCandidates: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"app/src/x.log", "src/newpkg/y.log"} {
		if !hasExclusion(report, path) {
			t.Fatalf("ignored file %s inside an untracked directory must stay listed: %#v", path, report.Exclusions)
		}
	}
}

// A directory that matches an ignore pattern but still holds tracked files is
// not a hard exclusion unless its name is built in: its loose ignored files
// are listed as before.
func TestSafeInventoryKeepsIgnoredFilesBesideTrackedOnes(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".gitignore", "output/\n")
	mustWrite(t, root, "output/keep.txt", "tracked on purpose\n")
	gitCommand(t, root, "add", "-f", ".gitignore", "output/keep.txt")
	mustWrite(t, root, "output/out.js", "generated\n")

	report, err := BuildSafeInventory(root, WalkOptions{IncludeIgnoredCandidates: true})
	if err != nil {
		t.Fatal(err)
	}
	if hasExclusion(report, "output/") || !containsPath(report.ManagedCandidates, "output/keep.txt") {
		t.Fatalf("a non-built-in directory must not be collapsed: %#v", report)
	}
	if !containsPath(report.IgnoredPaths, "output/out.js") {
		t.Fatalf("the loose ignored file beside a tracked one must still be listed: %#v", report)
	}
}

// git before 2.16 has no --ignored=matching; the inventory then falls back to
// the rc17 listing instead of failing.
func TestGitIgnoredPathsFallsBackWhenStatusMatchingIsUnavailable(t *testing.T) {
	calls := [][]string{}
	run := func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "status" {
			return nil, &GitQueryError{Code: "safe_inventory_git_query_failed", Stderr: "error: unknown option `ignored=matching'"}
		}
		return []byte("gen/api.go\x00node_modules/x.js\x00"), nil
	}
	files, collapsed, err := gitIgnoredPaths(run, func(string) bool { return true })
	if err != nil || len(collapsed) != 0 || !reflect.DeepEqual(files, []string{"gen/api.go", "node_modules/x.js"}) {
		t.Fatalf("fallback must return the full listing: files=%v collapsed=%v err=%v calls=%v", files, collapsed, err, calls)
	}
	if len(calls) != 2 || calls[1][0] != "ls-files" {
		t.Fatalf("fallback must run the rc17 ls-files listing: %v", calls)
	}
}

// Expansion pathspecs are literal: a directory named with glob characters
// means itself.
func TestLiteralPathspecBatchesAreLiteralAndBounded(t *testing.T) {
	batches := literalPathspecBatches([]string{"gen", "a[b]*"})
	if !reflect.DeepEqual(batches, [][]string{{":(literal)gen/", ":(literal)a[b]*/"}}) {
		t.Fatalf("unexpected pathspecs: %v", batches)
	}
	many := []string{}
	for i := 0; i < 3000; i++ {
		many = append(many, fmt.Sprintf("directory-with-a-long-name-%04d", i))
	}
	total := 0
	for _, batch := range literalPathspecBatches(many) {
		size := 0
		for _, spec := range batch {
			size += len(spec) + 1
		}
		if size > 16<<10+64 {
			t.Fatalf("a batch exceeds the command-line bound: %d bytes", size)
		}
		total += len(batch)
	}
	if total != len(many) {
		t.Fatalf("batches lost directories: %d of %d", total, len(many))
	}
}

func TestTailWriterKeepsABoundedRuneSafeTail(t *testing.T) {
	w := &tailWriter{limit: 8}
	for i := 0; i < 1000; i++ {
		_, _ = w.Write([]byte("warning: Filename too long\n"))
	}
	if len(w.data) > 8 {
		t.Fatalf("tail grew past its limit: %d", len(w.data))
	}
	w = &tailWriter{limit: 5}
	_, _ = w.Write([]byte("ab错误"))
	if tail := gitStderrTail(w); !utf8.ValidString(tail) || tail != "误" {
		t.Fatalf("tail must start on a rune boundary: %q", tail)
	}
}

// The ls-files enumeration is where #97 failed; its stderr must reach the
// error, not only the rev-parse probe's.
func TestSafeInventoryListingFailureCarriesGitStderr(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, "src/main.go", "package main\n")
	gitCommand(t, root, "add", "src/main.go")
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("DIRC garbage that is not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := BuildSafeInventory(root, WalkOptions{})
	var query *GitQueryError
	if !errors.As(err, &query) || query.Error() != "safe_inventory_git_query_failed" || !strings.Contains(strings.ToLower(query.Stderr), "index") {
		t.Fatalf("a failed listing must carry git's stderr: %v", err)
	}
}

func TestSafeInventoryGitFailureCarriesGitStderr(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".git/HEAD", "not a repository\n")
	mustWrite(t, root, "src/main.go", "package main\n")

	_, err := BuildSafeInventory(root, WalkOptions{})
	var query *GitQueryError
	if !errors.As(err, &query) || query.Error() != "safe_inventory_git_query_failed" {
		t.Fatalf("a failed git query must be a GitQueryError with the stable code: %v", err)
	}
	if strings.TrimSpace(query.Stderr) == "" {
		t.Fatalf("the failure must carry git's own stderr so an operator can tell why: %#v", query)
	}
}

func hasExclusion(report *SafeInventory, path string) bool {
	for _, exclusion := range report.Exclusions {
		if exclusion.PathSummary == path {
			return true
		}
	}
	return false
}
