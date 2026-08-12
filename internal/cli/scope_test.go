package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

func buildScopeCLIRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixtures", "case.txt"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexText := "# test index\n===" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[A.IX.9.T]: F:index | R:- | A:- | S:-\n" +
		"main.go[C.RT.9.T]: F:production | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
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
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	return root
}

func runScopeCLI(t *testing.T, root string, args ...string) ([]byte, error) {
	t.Helper()
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, true
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	command := newScopeCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.Bytes(), err
}

func TestScopeStatusObservesTestsWithoutChangingWholeIndex(t *testing.T) {
	root := buildScopeCLIRepo(t)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(output, &status); err != nil {
		t.Fatalf("decode %s: %v", output, err)
	}
	if status.Stage != "observed_evidence_review_required" || status.ObservedPendingReview != 1 ||
		status.Drift == nil || len(status.Drift.ObservedChanged) != 1 || status.Drift.ObservedChanged[0] != "main_test.go" {
		t.Fatalf("test change was not observe-only drift: %+v", status)
	}
	indexAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("scope status changed Whole-Index")
	}
}

func TestScopeStatusUsesCodeVolumeAndExcludesFormalVolumeDrift(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootPreimage := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n" +
		"#Project: Scope status Volumes fixture\n#Global-Invariants: deterministic fixture bytes\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"
	write("aoci.txt", rootPreimage+"#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")
	write("aoci.database.txt", cognition.DatabaseMarker+"\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
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
	// Simulate a completed pre-fix Database Bootstrap: the live Root and formal
	// Volumes are current, while only the Root Baseline binding is historical.
	files["aoci.txt"] = baseline.HashBytes("aoci.txt", []byte(rootPreimage))
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: cfg.CognitionBudget}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}

	output, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatalf("Volumes Scope status failed: %v: %s", err, output)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(output, &status); err != nil {
		t.Fatalf("decode %s: %v", output, err)
	}
	if status.Stage != "aligned" || status.Drift == nil || len(status.Drift.Missing) != 0 ||
		len(status.Drift.Orphan) != 0 || len(status.Drift.Stale) != 0 || len(status.Drift.Unbaselined) != 0 {
		t.Fatalf("formal Volumes were misclassified as Code source drift: %+v", status)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(cognition.CodeVolumeMarker+"\n# unrelated drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildScopePolicyStatus(root, true); err == nil || err.Error() != "managed_scope_formal_volume_baseline_drift: aoci.code.txt" {
		t.Fatalf("Scope status hid unproven formal Volume drift: %v", err)
	}
}

func TestScopeAcknowledgeAdvancesOnlyReviewedObserveFingerprint(t *testing.T) {
	root := buildScopeCLIRepo(t)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// reviewed change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := runScopeCLI(t, root, "acknowledge", "--reviewed-by=model-reviewer"); err != nil {
		t.Fatalf("acknowledge failed: %v: %s", err, output)
	}
	statusOutput, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		t.Fatal(err)
	}
	if status.Stage != "aligned" || status.ObservedPendingReview != 0 {
		t.Fatalf("reviewed observe change remained pending: %+v", status)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("observe acknowledgement changed Whole-Index")
	}
}

func TestScopeAcknowledgePreservesIndexAuthoringDebt(t *testing.T) {
	root := buildScopeCLIRepo(t)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineBefore, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline before acknowledgement: exists=%t err=%v", exists, err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package sample\n// semantic change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// reviewed change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := runScopeCLI(t, root, "acknowledge", "--reviewed-by=model-reviewer"); err != nil {
		t.Fatalf("acknowledge with Index debt failed: %v: %s", err, output)
	}
	baselineAfter, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline after acknowledgement: exists=%t err=%v", exists, err)
	}
	if baselineAfter.Files["main.go"].SHA256 != baselineBefore.Files["main.go"].SHA256 {
		t.Fatal("observe acknowledgement advanced a stale Index fingerprint")
	}
	if _, exists := baselineAfter.Files["new.go"]; exists {
		t.Fatal("observe acknowledgement baselined a new Index authoring target")
	}
	if baselineAfter.Files["main_test.go"].SHA256 == baselineBefore.Files["main_test.go"].SHA256 {
		t.Fatal("observe acknowledgement did not advance the reviewed Observe fingerprint")
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(after, indexBefore) {
		t.Fatal("observe acknowledgement changed Whole-Index")
	}
	statusOutput, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		t.Fatal(err)
	}
	if status.Stage != "authoring_required" || status.ObservedPendingReview != 0 ||
		status.Drift == nil || len(status.Drift.Stale) == 0 || len(status.Drift.Missing) == 0 {
		t.Fatalf("Index authoring debt did not remain visible: %+v", status)
	}
}

func TestScopeAcknowledgePreservesExistingOnboardingAuthoringDebt(t *testing.T) {
	root := buildScopeCLIRepo(t)
	skeleton := []byte("# onboarding skeleton\n")
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), skeleton, 0o644); err != nil {
		t.Fatal(err)
	}
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
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: cfg.CognitionBudget}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// reviewed before authoring\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runScopeCLI(t, root, "acknowledge", "--reviewed-by=model-reviewer")
	if err != nil {
		t.Fatalf("acknowledge failed on onboarding skeleton: %v: %s", err, output)
	}
	if after, readErr := os.ReadFile(filepath.Join(root, "aoci.txt")); readErr != nil || !bytes.Equal(after, skeleton) {
		t.Fatalf("observe acknowledgement changed onboarding skeleton: %v", readErr)
	}
	statusOutput, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		t.Fatal(err)
	}
	if status.ObservedPendingReview != 0 || len(status.Drift.Missing) == 0 {
		t.Fatalf("observe review or existing authoring debt was lost: %+v", status)
	}
}

func TestScopeExcludedFixtureProducesNoObservedState(t *testing.T) {
	root := buildScopeCLIRepo(t)
	if err := os.WriteFile(filepath.Join(root, "fixtures", "case.txt"), []byte("changed fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(output, &status); err != nil {
		t.Fatal(err)
	}
	if status.ObservedPendingReview != 0 || status.Stage != "aligned" {
		t.Fatalf("excluded fixture created governance drift: %+v", status)
	}
}

func TestScopeRuleMutationRequiresRefreshWithoutIndexOrBaselineWrite(t *testing.T) {
	root := buildScopeCLIRepo(t)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineBefore, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	output, err := runScopeCLI(t, root, "rule", "add", "user-index-tests", "--action=index", "--pattern=**/*_test.go",
		"--pattern-kind=glob", "--reason=project requires test Entries", "--created-by=test", "--order=10", "--enabled=true")
	if err != nil {
		t.Fatalf("add failed: %v: %s", err, output)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(indexBefore, after) {
		t.Fatal("rule edit changed index")
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json")); !bytes.Equal(baselineBefore, after) {
		t.Fatal("rule edit changed Baseline")
	}
	statusOutput, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		t.Fatal(err)
	}
	if status.Stage != "scope_change_required" || status.PolicyIdentityAligned || status.ObserveCount != 0 {
		t.Fatalf("direct policy edit was not held for explicit Scope Change: %+v", status)
	}
}

func TestScopeBudgetDirectEditRequiresRefreshWithoutFormalWrites(t *testing.T) {
	root := buildScopeCLIRepo(t)
	indexBefore, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineBefore, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	output, err := runScopeCLI(t, root, "budget", "set", "--target-tokens=100", "--warning-tokens=200", "--max-tokens=300")
	if err != nil {
		t.Fatalf("budget set failed: %v: %s", err, output)
	}
	statusOutput, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var status scopePolicyStatus
	if err := json.Unmarshal(statusOutput, &status); err != nil {
		t.Fatal(err)
	}
	if status.Stage != "scope_change_required" || status.BudgetIdentityAligned {
		t.Fatalf("direct budget edit was not held for explicit Apply: %+v", status)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); !bytes.Equal(indexBefore, after) {
		t.Fatal("budget edit changed Whole-Index")
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json")); !bytes.Equal(baselineBefore, after) {
		t.Fatal("budget edit changed Baseline")
	}
}

func TestScopeApprovalModeDirectEditRequiresRefreshWithoutBaselineWrite(t *testing.T) {
	root := buildScopeCLIRepo(t)
	baselineBefore, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	output, err := runScopeCLI(t, root, "approval-mode", "auto")
	if err != nil {
		t.Fatalf("approval mode failed: %v: %s", err, output)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveManagedScope().ApprovalMode != machinecontract.ScopeApprovalModeAuto {
		t.Fatal("Scope approval mode was not persisted")
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json")); !bytes.Equal(after, baselineBefore) {
		t.Fatal("approval mode edit advanced Baseline")
	}
}

func TestScopeAuthorizeAndApplyAutoReceiptWithoutTTY(t *testing.T) {
	root := buildScopeCLIRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: []string{"main_test.go"},
			ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: "model-reviewer"}}
	preview, err := scopechange.Build(root, "2026-07-31T03:30:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	previewBytes, err := scopechange.Encode(preview)
	if err != nil {
		t.Fatal(err)
	}
	previewPath := filepath.Join(t.TempDir(), "preview.json")
	if err := os.WriteFile(previewPath, previewBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	authorizationBytes, err := runScopeCLI(t, root, "authorize", "--preview-file", previewPath)
	if err != nil {
		t.Fatalf("auto authorize failed: %v: %s", err, authorizationBytes)
	}
	receipt, err := scopechange.DecodePolicyBoundApproval(authorizationBytes)
	if err != nil || receipt.Mechanism != machinecontract.ApprovalMechanismPolicyBoundAuto {
		t.Fatalf("invalid CLI auto Receipt: %+v err=%v", receipt, err)
	}
	authorizationPath := filepath.Join(t.TempDir(), "authorization.json")
	if err := os.WriteFile(authorizationPath, authorizationBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	resultBytes, err := runScopeCLI(t, root, "apply", "--preview-file", previewPath, "--authorization-file", authorizationPath)
	if err != nil {
		t.Fatalf("auto Apply failed: %v: %s", err, resultBytes)
	}
	var result scopechange.Result
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto ||
		result.ApprovalDigest != receipt.ApprovalDigest {
		t.Fatalf("CLI Apply lost auto authorization: %+v", result)
	}
}

func TestVerifyKeepsActiveManagedCountDuringDirectPolicyEdit(t *testing.T) {
	root := buildScopeCLIRepo(t)
	value, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load active Baseline: exists=%v err=%v", exists, err)
	}
	activeCount := len(value.Files)
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "user-index-tests", Action: machinecontract.ScopeRoleIndex,
			Pattern: "**/*_test.go", PatternKind: machinecontract.ScopePatternGlob, Reason: "project requires test Entries",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "verify"}, &stdout, &stderr)
	if code != ExitDrift {
		t.Fatalf("direct policy edit verify code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report verifyReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode verify output %s: %v", stdout.String(), err)
	}
	if !report.ManagedScope.ScopeChangeRequired || report.DiskFiles != activeCount {
		t.Fatalf("active governed count was hidden during direct edit: active=%d report=%+v", activeCount, report)
	}
}

func TestScopeExplainFutureAndSafetyPaths(t *testing.T) {
	root := buildScopeCLIRepo(t)
	output, err := runScopeCLI(t, root, "explain", "future/new_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var future managedscope.PathEvaluation
	if err := json.Unmarshal(output, &future); err != nil {
		t.Fatal(err)
	}
	if future.Role != machinecontract.ScopeRoleObserve || future.GitStatus != "future_or_absent" || future.MatchedRule == nil {
		t.Fatalf("future pattern was not explainable: %+v", future)
	}
	secretOutput, err := runScopeCLI(t, root, "explain", ".env.production")
	if err != nil {
		t.Fatal(err)
	}
	var secret managedscope.PathEvaluation
	if err := json.Unmarshal(secretOutput, &secret); err != nil {
		t.Fatal(err)
	}
	if secret.Role != machinecontract.ScopeRoleExclude || secret.ReadsContent || secret.RuleSource != machinecontract.ScopeRuleSafety {
		t.Fatalf("safety explanation incomplete: %+v", secret)
	}
}

func TestFirstManagedScanPersistsRolesAndForceCannotWashReceipt(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"main.go":           "package sample\n",
		"main_test.go":      "package sample\n",
		"fixtures/case.txt": "fixture\n",
		"aoci.txt":          "# index\n===" + filepath.ToSlash(root) + "/===\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("first managed scan failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists || value.ManagedScope == nil || value.ManagedScope.PolicyIdentity == "" ||
		value.ManagedScope.BudgetPolicyIdentity == "" {
		t.Fatalf("managed receipt missing: exists=%v err=%v baseline=%+v", exists, err, value)
	}
	if baseline.EffectiveRole(value.Files["main.go"]) != machinecontract.ScopeRoleIndex ||
		baseline.EffectiveRole(value.Files["main_test.go"]) != machinecontract.ScopeRoleObserve {
		t.Fatalf("initial roles not persisted: %+v", value.Files)
	}
	if _, exists := value.Files["fixtures/case.txt"]; exists {
		t.Fatal("excluded fixture entered managed Baseline")
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan", "--force"}, &stdout, &stderr); code != ExitConfig {
		t.Fatalf("managed scan --force washed governance: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
