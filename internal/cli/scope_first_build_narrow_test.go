package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// Narrowing the scope before the first Entry is written loses no cognition:
// a fresh repository's sources are what the first build will write, so the
// narrower scope applies without asking anyone (scipy first build, B4). Once
// the index holds an Entry, the same narrowing waits for a person again.
func TestNarrowingBeforeTheFirstEntryAppliesWithoutApproval(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	for _, rel := range []string{"keep/a.go", "drop/b.go", "drop/c.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte("package p\n\nfunc F() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--locale", "en-US", "--hooks=false", "--here=false"}, {"scan"}} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI(append([]string{"--repo", root}, args...), &stdout, &stderr); code != ExitOK {
			t.Fatalf("%v: exit=%d %s %s", args, code, &stdout, &stderr)
		}
	}
	exclude := func(directory string) {
		if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
			policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "narrow-" + directory, Action: machinecontract.ScopeRoleExclude,
				Pattern: directory, PatternKind: machinecontract.ScopePatternDirectory, Reason: "focus the first build",
				DecisionBasis: machinecontract.ScopeDecisionUnspecified, Source: machinecontract.ScopeRuleUser,
				CreatedBy: "first-build-test", Order: len(policy.Rules) + 1, Enabled: true})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	exclude("drop")
	code, data := activateJSON(t, root)
	if code != ExitOK {
		t.Fatalf("narrowing a repository with no Entry asked for approval: code=%d %s", code, data)
	}
	var applied map[string]any
	if err := json.Unmarshal(data, &applied); err != nil {
		t.Fatal(err)
	}
	if applied["status"] != "applied" && applied["applied"] != true {
		t.Logf("activate answered: %s", data)
	}

	// One Entry written: narrowing now waits for a person again.
	if err := os.WriteFile(filepath.Join(root, "keep", "d.go"), []byte("package p\n\nfunc D() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "scope", "activate"}, &stdout, &stderr); code != ExitOK && code != 2 {
		t.Fatalf("activate: %d %s %s", code, &stdout, &stderr)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "keep", "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, fail := mcptools.ApplyUpdateEntriesAtomic(root, []mcptools.AtomicUpdateItem{{Path: "keep/a.go",
		NewEntry: "a.go[CG5T]: F:Provides the kept fixture function | R:- | A:- | S:-", SourceSHA256: fingerprint.SHA256}},
		"test", false); fail != nil {
		t.Fatalf("writing the first Entry: %s", fail.Msg)
	}
	exclude("keep")
	if code, data := activateJSON(t, root); code == ExitOK {
		t.Fatalf("narrowing after the first Entry applied without approval: %s", data)
	}
}
