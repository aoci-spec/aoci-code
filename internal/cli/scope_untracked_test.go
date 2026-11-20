package cli

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func untrackedScopeStatus(t *testing.T, root string, want ...string) scopePolicyStatus {
	t.Helper()
	output, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(output, &status); err != nil {
		t.Fatalf("decode %s: %v", output, err)
	}
	if !reflect.DeepEqual(status.GitUntrackedPaths, want) {
		t.Fatalf("untracked paths = %v, want %v", status.GitUntrackedPaths, want)
	}
	if len(want) == 0 && strings.Contains(string(output), `"git_untracked_paths"`) {
		t.Fatal("empty diagnostic must be omitted")
	}
	return status
}

func scopeStatusTree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[rel] = sha256.Sum256(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestScopeStatusUntrackedTwoCheckoutsIsAdvisoryAndReadOnly(t *testing.T) {
	root := scopeActivationFixtureWith(t, map[string]string{"main_test.go": "package main\n"})
	testkit.InitGit(t, root)
	testkit.Git(t, root, "config", "core.autocrlf", "false")
	testkit.WriteFile(t, root, ".aoci/ledger.jsonl", "{\"operation\":\"fixture\"}\n")
	testkit.Git(t, root, "add", "--", "aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/config.json", ".aoci/baseline.json")
	testkit.Git(t, root, "commit", "-qm", "share cognition without local sources")
	clone := filepath.Join(t.TempDir(), "clone")
	testkit.Git(t, root, "-c", "core.autocrlf=false", "clone", "-q", root, clone)
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
		if testkit.ReadFile(t, root, rel) != testkit.ReadFile(t, clone, rel) {
			t.Fatalf("shared asset changed on clone: %s", rel)
		}
	}
	before := scopeStatusTree(t, root)
	status := untrackedScopeStatus(t, root, "main.go", "main_test.go")
	if status.Stage != "aligned" || !status.PolicyIdentityAligned || status.AuthoringTargets != 0 {
		t.Fatalf("advisory changed governance: %+v", status)
	}
	text := mustCLI(t, root, "scope", "status")
	if !strings.Contains(text, "Git-untracked: main.go, main_test.go") {
		t.Fatalf("missing human warning: %s", text)
	}
	if after := scopeStatusTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatal("status changed repository files, Git metadata, or Ledger")
	}
	cloneBefore := scopeStatusTree(t, clone)
	cloned := untrackedScopeStatus(t, clone)
	if cloned.Drift == nil || !reflect.DeepEqual(cloned.Drift.Orphan, []string{"main.go"}) ||
		!reflect.DeepEqual(cloned.Drift.ObservedRemoved, []string{"main_test.go"}) {
		t.Fatalf("clone must distinguish missing sources from untracked ones: %+v", cloned.Drift)
	}
	if !reflect.DeepEqual(scopeStatusTree(t, clone), cloneBefore) {
		t.Fatal("clone status wrote repository files")
	}
	// Git's index is the tracking authority: a staged source need not have a
	// commit yet, and must no longer be called untracked.
	testkit.Git(t, root, "add", "--", "main.go", "main_test.go")
	untrackedScopeStatus(t, root)
}

func TestScopeStatusUntrackedReferencesAndEarlyReturns(t *testing.T) {
	root := buildScopeCLIRepo(t)
	testkit.WriteFile(t, root, "not_referenced.go", "package sample\n")
	active, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	delete(active.Files, "main.go") // Index-only reference; Observe is Baseline-only.
	if err := baseline.Save(root, active); err != nil {
		t.Fatal(err)
	}
	untrackedScopeStatus(t, root, "main.go", "main_test.go")
	testkit.WriteFile(t, root, ".gitignore", "main_test.go\n")
	untrackedScopeStatus(t, root, "main.go")
	if err := os.Remove(filepath.Join(root, "main_test.go")); err != nil {
		t.Fatal(err)
	}
	untrackedScopeStatus(t, root, "main.go")
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.ObserveChangePolicy = "informational"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if status := untrackedScopeStatus(t, root, "main.go"); status.Stage != "scope_change_required" {
		t.Fatalf("policy mismatch was hidden: %s", status.Stage)
	}
	if err := os.Remove(filepath.Join(root, ".aoci", "baseline.json")); err != nil {
		t.Fatal(err)
	}
	untrackedScopeStatus(t, root, "main.go")
}

func TestScopeStatusUntrackedChildGitAndOutsideGit(t *testing.T) {
	root := scopeActivationFixture(t)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.InitGit(t, child)
	testkit.WriteFile(t, child, "local.go", "package child\n")
	testkit.WriteFile(t, child, "local_test.go", "package child\n")
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	active.Files = files
	if err := baseline.Save(root, active); err != nil {
		t.Fatal(err)
	}
	untrackedScopeStatus(t, root, "child/local.go", "child/local_test.go")
	testkit.Git(t, child, "add", "--", "local.go", "local_test.go")
	untrackedScopeStatus(t, root)
}

func TestScopeStatusUntrackedWarningLocalized(t *testing.T) {
	root := buildScopeCLIRepo(t)
	for locale, warning := range map[string]string{"en-US": "Warning:", "zh-CN": "警告："} {
		t.Run(locale, func(t *testing.T) {
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Locale = locale
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			text := mustCLI(t, root, "scope", "status")
			if !strings.Contains(text, warning) || !strings.Contains(text, "main.go, main_test.go") {
				t.Fatalf("missing localized warning: %s", text)
			}
		})
	}
}
