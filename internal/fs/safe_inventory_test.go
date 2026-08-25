package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestSafeInventoryExcludesOnlyRootCodeTargetPlan(t *testing.T) {
	for _, gitRepository := range []bool{false, true} {
		name := "non-git"
		if gitRepository {
			name = "git"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if gitRepository {
				gitCommand(t, root, "init", "-q")
			}
			mustWrite(t, root, "aoci.code.target.txt", "#AOCI-CODE-VOLUME: 1\n")
			mustWrite(t, root, "plans/aoci.code.target.txt", "business-owned nested file\n")
			mustWrite(t, root, "src/main.go", "package main\n")

			report, err := BuildSafeInventory(root, WalkOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if containsPath(report.ManagedCandidates, "aoci.code.target.txt") ||
				exclusionCategory(report, "aoci.code.target.txt") != SafetyGenerated {
				t.Fatalf("root Code target plan must be a generated planning artifact: %#v", report)
			}
			for _, path := range []string{"plans/aoci.code.target.txt", "src/main.go"} {
				if !containsPath(report.ManagedCandidates, path) {
					t.Fatalf("root-only exclusion hid %s: %#v", path, report)
				}
			}
			if gitRepository && (report.Summary.Ignored != 0 || report.Summary.NonignoredUntracked != 3) {
				t.Fatalf("Git fixture must prove the target was unignored: %#v", report.Summary)
			}
		})
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
