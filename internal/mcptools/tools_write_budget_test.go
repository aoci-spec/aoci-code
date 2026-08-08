package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func activateManagedWritePolicy(t *testing.T, root string, scope managedscope.Policy, budget cognitionbudget.Policy) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	scope, err = managedscope.Normalize(scope)
	if err != nil {
		t.Fatal(err)
	}
	budget, err = cognitionbudget.Normalize(budget)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope, cfg.CognitionBudget = &scope, &budget
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, scope, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	indexFingerprint, err := baseline.HashFile(filepath.Join(root, ".aoci", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	indexFingerprint.Role = machinecontract.ScopeRoleIndex
	snapshot[".aoci/index.txt"] = indexFingerprint
	value := baseline.NewBaseline(snapshot)
	budgetIdentity, err := cognitionbudget.Identity(budget)
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: scope.ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
}

func permissiveFieldBands() []cognitionbudget.FieldBand {
	return []cognitionbudget.FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 1000, MaxTokens: 2000}}
}

func TestMCPEntryFieldBudgetExceededIsRepairRequiredAndZeroWrite(t *testing.T) {
	root := buildRepo(t)
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	budget.WholeIndex = cognitionbudget.WholeIndexPolicy{TargetTokens: 10000, WarningTokens: 20000, MaxTokens: 30000}
	budget.R = []cognitionbudget.FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 1, MaxTokens: 2}}
	budget.S = permissiveFieldBands()
	activateManagedWritePolicy(t, root, managedscope.LegacyPolicy(), budget)
	before := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "src/a.go", SourceSHA256: sourceSHA256(t, root, "src/a.go"),
		NewEntry: "a.go[X.Y.5.T]: F:budget gate | R:" + strings.Repeat("src/related.go ", 20) + " | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("R overage did not request model repair: %+v", result)
	}
	if readBatchIndex(t, root) != before {
		t.Fatal("entry_field_budget_exceeded changed formal index")
	}
}

func TestMCPProjectedWholeIndexBudgetExceededRejectsWholeBatch(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	current := cognitionbudget.EstimateTokens([]byte(readBatchIndex(t, root)))
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	budget.WholeIndex = cognitionbudget.WholeIndexPolicy{TargetTokens: current, WarningTokens: current + 1, MaxTokens: current + 2}
	budget.R, budget.S = permissiveFieldBands(), permissiveFieldBands()
	activateManagedWritePolicy(t, root, managedscope.LegacyPolicy(), budget)
	before := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "src/b.go", SourceSHA256: sourceSHA256(t, root, "src/b.go"),
		NewEntry: "b.go[X.Y.5.T]: F:projected whole index gate | R:- | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("projected Whole-Index overage did not reject the batch: %+v", result)
	}
	if readBatchIndex(t, root) != before {
		t.Fatal("whole_index_budget_exceeded changed formal index")
	}
}

func TestMCPObserveBudgetStillBlocksProjectedWholeIndexOverMaximum(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	budget.WholeIndex = cognitionbudget.WholeIndexPolicy{TargetTokens: 1, WarningTokens: 2, MaxTokens: 3}
	budget.R, budget.S = permissiveFieldBands(), permissiveFieldBands()
	activateManagedWritePolicy(t, root, managedscope.LegacyPolicy(), budget)
	before := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "src/b.go", SourceSHA256: sourceSHA256(t, root, "src/b.go"),
		NewEntry: "b.go[X.Y.5.T]: F:observe transition write | R:- | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("observe transition allowed projected Whole-Index above max: %+v", result)
	}
	if readBatchIndex(t, root) != before {
		t.Fatal("observe-mode Whole-Index hard gate changed formal index")
	}
}

func TestMCPManagedObserveTargetCannotReceiveEntry(t *testing.T) {
	root := buildRepo(t)
	path := filepath.Join(root, "src", "a_test.go")
	if err := os.WriteFile(path, []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	activateManagedWritePolicy(t, root, managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction), budget)
	before := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "src/a_test.go", SourceSHA256: sourceSHA256(t, root, "src/a_test.go"),
		NewEntry: "a_test.go[T.Y.5.T]: F:test | R:src/a.go | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("observe target accepted a formal Entry: %+v", result)
	}
	if readBatchIndex(t, root) != before {
		t.Fatal("observe target rejection changed formal index")
	}
}

func TestMaintainCandidateCarriesScopeAndBudgetAuthoringContext(t *testing.T) {
	root := buildRepo(t)
	indexText := maintainHeader(true) + "\n===代码索引" + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" +
		"a.go[CRT5T]: F:production source | R:- | A:- | S:non-obvious constraint\n"
	if err := os.WriteFile(filepath.Join(root, ".aoci", "index.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	budget := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
	activateManagedWritePolicy(t, root, managedscope.LegacyPolicy(), budget)
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A2"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	if result.Status != autoStatusRepairRequired || len(result.Candidates) != 1 || result.ManagedGovernance == nil {
		t.Fatalf("managed stale source did not produce one authoring task: %+v", result)
	}
	candidate := result.Candidates[0]
	rBand, _ := cognitionbudget.LimitFor(budget.R, 5)
	sBand, _ := cognitionbudget.LimitFor(budget.S, 5)
	if candidate.Path != "src/a.go" || candidate.ScopeRole != machinecontract.ScopeRoleIndex ||
		candidate.WholeIndexTokens == 0 || candidate.ProjectedWholeIndexTokens != candidate.WholeIndexTokens ||
		!candidate.ProjectionPending || candidate.RemainingTokens == 0 ||
		candidate.RTargetTokens != rBand.TargetTokens || candidate.RMaxTokens != rBand.MaxTokens ||
		candidate.STargetTokens != sBand.TargetTokens || candidate.SMaxTokens != sBand.MaxTokens ||
		len(candidate.RFieldBands) != len(budget.R) || len(candidate.SFieldBands) != len(budget.S) {
		t.Fatalf("authoring context incomplete: %+v", candidate)
	}
}
