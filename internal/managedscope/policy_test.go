package managedscope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func writeScopeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitScopeFixture(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func evaluationForPath(result *Evaluation, rel string) (PathEvaluation, bool) {
	for _, group := range [][]PathEvaluation{result.Index, result.Observe, result.Exclude} {
		for _, item := range group {
			if item.Path == rel {
				return item, true
			}
		}
	}
	return PathEvaluation{}, false
}

func TestProductionProfileAndUserPrecedence(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	writeScopeFixture(t, root, "src/main_test.go", "package src\n")
	writeScopeFixture(t, root, "src/testdata/case.txt", "fixture\n")
	writeScopeFixture(t, root, ".env", "SECRET=not-read-by-scope\n")
	gitScopeFixture(t, root, "add", "src/main.go", "src/main_test.go", "src/testdata/case.txt", ".env")

	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	policy.Rules = append(policy.Rules, Rule{RuleID: "user-index-one-test", Action: machinecontract.ScopeRoleIndex,
		Pattern: "src/main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "project exception",
		Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
	result, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for path, role := range map[string]string{"src/main.go": machinecontract.ScopeRoleIndex,
		"src/main_test.go": machinecontract.ScopeRoleIndex, "src/testdata/case.txt": machinecontract.ScopeRoleExclude,
		".env": machinecontract.ScopeRoleExclude} {
		item, ok := evaluationForPath(result, path)
		if !ok || item.Role != role {
			t.Fatalf("%s role=%q found=%v; evaluation=%+v", path, item.Role, ok, result)
		}
	}
	secret, _ := evaluationForPath(result, ".env")
	if secret.ReadsContent || secret.RuleSource != machinecontract.ScopeRuleSafety {
		t.Fatalf("hard safety must win before content access: %+v", secret)
	}
	production, _ := evaluationForPath(result, "src/main.go")
	if production.GitStatus != "tracked" {
		t.Fatalf("tracked source fact missing from explanation: %+v", production)
	}
}

func TestLastMatchingUserRuleWinsAndDirectorySlashNormalizes(t *testing.T) {
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	policy.Rules = []Rule{
		{RuleID: "user-exclude-src", Action: machinecontract.ScopeRoleExclude, Pattern: "src/", PatternKind: machinecontract.ScopePatternDirectory,
			Reason: "first", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true},
		{RuleID: "user-observe-src", Action: machinecontract.ScopeRoleObserve, Pattern: "src/**", PatternKind: machinecontract.ScopePatternGlob,
			Reason: "last", Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 2, Enabled: true},
	}
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	got := EvaluatePath(normalized, "src/main.go", false, false)
	if got.Role != machinecontract.ScopeRoleObserve || got.MatchedRule == nil || got.MatchedRule.RuleID != "user-observe-src" {
		t.Fatalf("last matching user rule did not win: %+v", got)
	}
}

func TestPolicyRejectsEscapeAbsoluteAndInvalidGlob(t *testing.T) {
	for _, pattern := range []string{"../secret", "/absolute", `C:\\secret`, "src/[ab].go"} {
		policy := DefaultPolicy(machinecontract.ScopeProfileCustom)
		policy.Rules = []Rule{{RuleID: "bad-pattern", Action: machinecontract.ScopeRoleIndex, Pattern: pattern,
			PatternKind: machinecontract.ScopePatternGlob, Reason: "bad", Source: machinecontract.ScopeRuleUser,
			CreatedBy: "test", Order: 1, Enabled: true}}
		if _, err := Normalize(policy); err == nil {
			t.Fatalf("unsafe or unsupported pattern accepted: %q", pattern)
		}
	}
}

func TestPolicyDefaultsAndValidatesApprovalThresholds(t *testing.T) {
	policy := Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: machinecontract.ScopeProfileProduction,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, Rules: []Rule{}}
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.ApprovalThresholds != DefaultApprovalThresholds() {
		t.Fatalf("missing legacy thresholds did not receive stable defaults: %+v", normalized.ApprovalThresholds)
	}
	if normalized.ApprovalMode != machinecontract.ScopeApprovalModeInherit {
		t.Fatalf("missing approval mode did not inherit automation: %q", normalized.ApprovalMode)
	}
	policy.ApprovalThresholds = ApprovalThresholds{EntryRemovalCount: 10, EntryRemovalPercent: 101}
	if _, err := Normalize(policy); err == nil {
		t.Fatal("invalid removal percentage accepted")
	}
	policy.ApprovalThresholds = DefaultApprovalThresholds()
	policy.ApprovalMode = "locale_says_auto"
	if _, err := Normalize(policy); err == nil {
		t.Fatal("presentation text was accepted as an approval mode")
	}
}

func TestSnapshotNeverHashesExcludedContent(t *testing.T) {
	root := t.TempDir()
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	writeScopeFixture(t, root, "fixtures/case.txt", "fixture\n")
	result, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Snapshot(root, result)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot["fixtures/case.txt"]; ok {
		t.Fatal("excluded fixture entered fingerprint snapshot")
	}
	if snapshot["src/main.go"].Role != machinecontract.ScopeRoleIndex {
		t.Fatalf("index fingerprint role missing: %+v", snapshot)
	}
}

func TestBuildHonorsConfiguredSafeInventoryExclusions(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	writeScopeFixture(t, root, "experiments/prototype.go", "package experiments\n")
	writeScopeFixture(t, root, "generated.txt", "generated body\n")
	gitScopeFixture(t, root, "add", "src/main.go", "experiments/prototype.go", "generated.txt")

	result, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{WalkOptions: fs.WalkOptions{
		ExcludeDirs: []string{"experiments"}, ExcludeFiles: []string{"generated.txt"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"experiments/prototype.go", "generated.txt"} {
		item, ok := evaluationForPath(result, rel)
		if !ok || item.Role != machinecontract.ScopeRoleExclude || item.ReadsContent || item.RuleSource != machinecontract.ScopeRuleSafety {
			t.Fatalf("configured Safe Inventory exclusion was not preserved for %s: %+v", rel, item)
		}
	}
	if item, _ := evaluationForPath(result, "src/main.go"); item.Role != machinecontract.ScopeRoleIndex {
		t.Fatalf("unrelated production source was not indexed: %+v", item)
	}
}

func TestHighRiskExactOptInRequiresApprovalBeforeContentRead(t *testing.T) {
	root := t.TempDir()
	writeScopeFixture(t, root, ".env", "SECRET=must-not-be-read-before-approval\n")
	policy := DefaultPolicy(machinecontract.ScopeProfileFull)
	result, err := Build(root, policy, BuildOptions{HighRiskOptIn: []string{".env"}})
	if err != nil {
		t.Fatal(err)
	}
	item, found := evaluationForPath(result, ".env")
	if !found || item.SafetyStatus != "high_risk_exact_opt_in" {
		t.Fatalf("high-risk opt-in was not visible: %+v", result)
	}
	if _, err := Snapshot(root, result); err == nil {
		t.Fatal("high-risk content was readable before approval")
	}
	snapshot, err := Snapshot(root, result, SnapshotOptions{HighRiskContentApproved: true})
	if err != nil || snapshot[".env"].SHA256 == "" {
		t.Fatalf("approved exact opt-in was not fingerprinted: %+v err=%v", snapshot, err)
	}
}

func TestCaseInsensitiveRuleMatchingIsDeterministic(t *testing.T) {
	policy := DefaultPolicy(machinecontract.ScopeProfileCustom)
	policy.Rules = []Rule{{RuleID: "unicode-path", Action: machinecontract.ScopeRoleObserve,
		Pattern: "Src/Tests/**", PatternKind: machinecontract.ScopePatternGlob, Reason: "platform test",
		Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 1, Enabled: true}}
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	insensitive := EvaluatePathWithCase(normalized, "src/tests/CASE.go", false, false, false, false)
	if insensitive.Role != machinecontract.ScopeRoleObserve || insensitive.MatchedRule == nil {
		t.Fatalf("case-insensitive filesystem did not match rule: %+v", insensitive)
	}
	sensitive := EvaluatePathWithCase(normalized, "src/tests/CASE.go", false, false, false, true)
	if sensitive.Role != machinecontract.ScopeRoleExclude || sensitive.MatchedRule != nil {
		t.Fatalf("case-sensitive filesystem unexpectedly matched rule: %+v", sensitive)
	}
}

func TestProductionFullAndCustomProfilesAcrossProjectShapes(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"cmd/server/main.go":         "package main\n",
		"tests/test_api.py":          "def test_api(): pass\n",
		"web/app.test.ts":            "export {}\n",
		"scripts/release_test.sh":    "#!/bin/sh\n",
		"nested/testdata/input.json": "{}\n",
		"nested/fixtures/case.txt":   "fixture\n",
		"docs/architecture.md":       "# Architecture\n",
		"config/.env.example":        "TOKEN=\n",
	} {
		writeScopeFixture(t, root, rel, body)
	}
	production, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"cmd/server/main.go": machinecontract.ScopeRoleIndex, "tests/test_api.py": machinecontract.ScopeRoleObserve,
		"web/app.test.ts": machinecontract.ScopeRoleObserve, "scripts/release_test.sh": machinecontract.ScopeRoleObserve,
		"nested/testdata/input.json": machinecontract.ScopeRoleExclude, "nested/fixtures/case.txt": machinecontract.ScopeRoleExclude,
		"docs/architecture.md": machinecontract.ScopeRoleIndex, "config/.env.example": machinecontract.ScopeRoleIndex,
	} {
		item, ok := evaluationForPath(production, rel)
		if !ok || item.Role != want {
			t.Fatalf("production %s role=%q want=%q found=%v", rel, item.Role, want, ok)
		}
	}
	full, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileFull), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"tests/test_api.py", "web/app.test.ts", "nested/fixtures/case.txt"} {
		item, ok := evaluationForPath(full, rel)
		if !ok || item.Role != machinecontract.ScopeRoleIndex {
			t.Fatalf("full profile did not index safe %s: %+v", rel, item)
		}
	}
	custom := DefaultPolicy(machinecontract.ScopeProfileCustom)
	custom.Rules = []Rule{{RuleID: "custom-docs", Action: machinecontract.ScopeRoleIndex, Pattern: "docs/",
		PatternKind: machinecontract.ScopePatternDirectory, Reason: "project contract", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "test", Order: 1, Enabled: true}}
	customResult, err := Build(root, custom, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if item, _ := evaluationForPath(customResult, "docs/architecture.md"); item.Role != machinecontract.ScopeRoleIndex {
		t.Fatalf("custom exact directory rule not applied: %+v", item)
	}
	if item, _ := evaluationForPath(customResult, "cmd/server/main.go"); item.Role != machinecontract.ScopeRoleExclude {
		t.Fatalf("custom unmatched source not excluded: %+v", item)
	}
}
