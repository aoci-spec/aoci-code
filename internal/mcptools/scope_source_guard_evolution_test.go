package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func activateMixedObserveFixture(t *testing.T, root string) {
	t.Helper()
	code := volumeFileText(t, root, "aoci.code.txt")
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(code, "\n"), "\n") {
		if !strings.HasPrefix(line, "contracts_test.go[") {
			lines = append(lines, line)
		}
	}
	writeVolumeTestFile(t, root, "aoci.code.txt", strings.Join(lines, "\n")+"\n")
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	budget, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope, cfg.CognitionBudget = &policy, &budget
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load mixed fixture Baseline: exists=%t err=%v", exists, err)
	}
	budgetIdentity, err := cognitionbudget.Identity(budget)
	if err != nil {
		t.Fatal(err)
	}
	state.Files = files
	state.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budget}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.ManagedScope.ObserveCount != 1 || facts.DatabaseCognition.Summary.Current != 12 {
		t.Fatalf("mixed Observe fixture is not aligned: facts=%#v err=%v", facts, err)
	}
}

func cognitionEntryLines(t *testing.T, root, domain string) map[string]string {
	t.Helper()
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, object := range set.Volumes[domain].Objects {
		result[object.CanonicalRef] = object.Entry.FullLine
	}
	return result
}

func TestMixedCognitionEvolutionContinuesAfterObserveAcknowledge(t *testing.T) {
	root, tablesBySource, _ := buildDatabaseAgentNativeRepo(t)
	initialDatabase := decodeDatabaseMaintain(t, handleDatabaseMaintain(root, "source-guard-test", cognition.ScopeDatabase))
	if initialDatabase.Plan == nil {
		t.Fatal("fixture did not produce initial Database candidates")
	}
	initialEntries := make([]updateEntryItemIn, 0, len(initialDatabase.Plan.Candidates))
	for _, candidate := range initialDatabase.Plan.Candidates {
		initialEntries = append(initialEntries, updateEntryItemIn{ObjectRef: candidate.ObjectRef, CandidateID: candidate.CandidateID,
			BatchID: initialDatabase.Plan.BatchID, NewEntry: databaseAgentEntries(false)[candidate.ObjectRef]})
	}
	if applied := decodeAutoResult(t, handleMCPUpdateBatch(root, "source-guard-test", initialEntries)); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("initial Database Cognition did not align: %#v", applied)
	}
	activateMixedObserveFixture(t, root)
	codeBefore := cognitionEntryLines(t, root, cognition.ScopeCode)
	databaseBefore := cognitionEntryLines(t, root, cognition.ScopeDatabase)

	changedCodePath := "internal/domain/orders.go"
	changedCode, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(changedCodePath)))
	if err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, changedCodePath, string(changedCode)+"// focused source guard evolution change\n")
	newCodePath := "internal/domain/source_guard_feature.go"
	writeVolumeTestFile(t, root, newCodePath, "package domain\n// SourceGuardFeature is deterministic fixture evidence.\n")
	observePath := "internal/domain/contracts_test.go"
	observeBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(observePath)))
	if err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, observePath, string(observeBytes)+"// reviewed Observe-only evolution evidence\n")
	pgTables := append([]dbevidence.TableEvidence{}, tablesBySource["pgtemp"]...)
	for index := range pgTables {
		if pgTables[index].Name == "orders" {
			pgTables[index].Columns = append(pgTables[index].Columns, dbevidence.Column{Ordinal: len(pgTables[index].Columns) + 1,
				Name: "source_guard_revision", NativeType: "integer", CanonicalType: "integer", Nullable: false})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), pgTables)

	var blocked volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "source-guard-test", maintainIn{}, nil))), &blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.Status != autoStatusStopped || blocked.Result != volumegovernance.ResultBlocked || len(blocked.Candidates) != 0 ||
		blocked.Governance.ManagedScope.ObservedPendingReview != 1 || blocked.Governance.DatabaseCognition.Summary.Stale != 1 {
		t.Fatalf("first Maintain did not stop precisely for Observe review: %#v", blocked)
	}
	candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: []string{observePath},
			ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: "mixed-evolution-test"}}
	preview, err := scopechange.Build(root, "2026-08-05T00:20:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scopechange.Apply(root, preview, nil); err != nil {
		t.Fatalf("Observe acknowledgement blocked mixed evolution: %v", err)
	}

	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "source-guard-test", maintainIn{}, nil))), &maintain); err != nil {
		t.Fatal(err)
	}
	codeCount, databaseCount := 0, 0
	for _, candidate := range maintain.Candidates {
		switch candidate.Domain {
		case cognition.ScopeCode:
			codeCount++
		case cognition.ScopeDatabase:
			databaseCount++
		}
	}
	if maintain.Status != autoStatusRepairRequired || maintain.Result != volumegovernance.ResultAuthoringRequired ||
		codeCount != 2 || databaseCount != 1 || maintain.DatabasePlan == nil || maintain.DatabasePlan.TargetCount != 1 ||
		maintain.Governance.ManagedScope.ObservedPendingReview != 0 {
		t.Fatalf("post-acknowledgement Maintain did not return the exact mixed delta: %#v", maintain)
	}
	entries := make([]updateEntryItemIn, 0, 3)
	for _, candidate := range maintain.Candidates {
		if candidate.Domain == cognition.ScopeDatabase {
			entries = append(entries, updateEntryItemIn{ObjectRef: candidate.ObjectRef, CandidateID: candidate.CandidateID,
				BatchID: maintain.DatabasePlan.BatchID, NewEntry: databaseAgentEntries(true)[candidate.ObjectRef]})
			continue
		}
		entries = append(entries, updateEntryItemIn{Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
			NewEntry: filepath.Base(candidate.Path) + "[CD7S]: F:provide focused mixed-evolution behavior | R:- | A:- | S:Behavior remains deterministic under replay"})
	}
	applied := decodeAutoResult(t, handleMCPUpdateBatch(root, "source-guard-test", entries))
	if applied.Status != autoStatusApplied || applied.Applied != 3 {
		t.Fatalf("mixed three-item batch did not apply atomically: %#v", applied)
	}
	var final volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "source-guard-test", maintainIn{}, nil))), &final); err != nil {
		t.Fatal(err)
	}
	if !final.Aligned || final.Result != volumegovernance.ResultAligned || final.Governance.DatabaseCognition.Summary.Current != 12 {
		t.Fatalf("mixed evolution did not return to aligned: %#v", final)
	}
	codeAfter := cognitionEntryLines(t, root, cognition.ScopeCode)
	databaseAfter := cognitionEntryLines(t, root, cognition.ScopeDatabase)
	for objectRef, before := range codeBefore {
		if objectRef != "code:"+changedCodePath && codeAfter[objectRef] != before {
			t.Fatalf("unrelated Code object changed: %s", objectRef)
		}
	}
	for objectRef, before := range databaseBefore {
		if objectRef != "database://pgtemp/aoci_d0/orders" && databaseAfter[objectRef] != before {
			t.Fatalf("unrelated Database object changed: %s", objectRef)
		}
	}
	if _, exists := codeAfter["code:"+observePath]; exists {
		t.Fatal("Observe-only source entered Code Cognition")
	}
}
