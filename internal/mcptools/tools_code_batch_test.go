package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestVolumeCode201CandidatesApplyAs200PlusOne(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || first.Batch.TotalTargets != 201 || first.Batch.Included != 200 ||
		first.Batch.Remaining != 1 || !first.Batch.ContinuationRequired || len(first.Candidates) != 200 {
		t.Fatalf("first machine batch is invalid: %#v", first)
	}
	firstArgs := codeBatchArguments(first.CodePlan)
	firstApply := applyVolumeBatch(t, session, firstArgs)
	if firstApply.Status != autoStatusApplied || firstApply.Aligned || firstApply.Applied != 200 || firstApply.Remaining != 1 {
		t.Fatalf("first batch claimed whole-index alignment: %#v", firstApply)
	}
	second := maintainVolumeBatch(t, session)
	if second.CodePlan == nil || second.Batch.TotalTargets != 1 || second.Batch.Included != 1 || second.Batch.Remaining != 0 {
		t.Fatalf("second machine batch is invalid: %#v", second)
	}
	secondApply := applyVolumeBatch(t, session, codeBatchArguments(second.CodePlan))
	if secondApply.Status != autoStatusApplied || !secondApply.Aligned || secondApply.Applied != 1 || secondApply.Remaining != 0 {
		t.Fatalf("second batch did not close alignment: %#v", secondApply)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 202 {
		t.Fatalf("final code entry set is incomplete: count=%d err=%v", set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func TestVolumeCodeBatchSourceDriftAndDuplicateSubmissionStaySafe(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	firstArgs := codeBatchArguments(first.CodePlan)
	if applied := applyVolumeBatch(t, session, firstArgs); applied.Applied != 200 || applied.Aligned {
		t.Fatalf("first batch failed: %#v", applied)
	}
	duplicate := applyVolumeBatch(t, session, firstArgs)
	if duplicate.Status != autoStatusApplied || duplicate.Applied != 0 || duplicate.Metrics.DuplicateApplies != 1 {
		t.Fatalf("duplicate batch was not stable: %#v", duplicate)
	}
	second := maintainVolumeBatch(t, session)
	path := second.CodePlan.Candidates[0].Path
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, append(data, []byte("// drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	drift := applyVolumeBatch(t, session, codeBatchArguments(second.CodePlan))
	if drift.Status != autoStatusStopped || drift.Applied != 0 || drift.FormalWritesStarted {
		t.Fatalf("source drift did not fail closed: %#v", drift)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 201 {
		t.Fatalf("successful first batch was lost or repeated: count=%d err=%v", set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func TestVolumeCodeSecondBatchRepairPreservesFirstBatch(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if applied := applyVolumeBatch(t, session, codeBatchArguments(first.CodePlan)); applied.Applied != 200 || applied.Aligned {
		t.Fatalf("first batch failed: %#v", applied)
	}
	second := maintainVolumeBatch(t, session)
	broken := codeBatchArguments(second.CodePlan)
	broken["entries"].([]map[string]any)[0]["new_entry"] = "not an entry"
	repair := applyVolumeBatch(t, session, broken)
	if repair.Status != autoStatusRepairRequired || repair.Applied != 0 || repair.FormalWritesStarted {
		t.Fatalf("second batch repair was not isolated: %#v", repair)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(second.CodePlan)); !applied.Aligned || applied.Applied != 1 {
		t.Fatalf("repaired second batch did not complete: %#v", applied)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 202 {
		t.Fatalf("repair changed the successful first batch: count=%d err=%v", set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func TestVolumeCodeScopeChangeInvalidatesOutstandingBatch(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if applied := applyVolumeBatch(t, session, codeBatchArguments(first.CodePlan)); applied.Applied != 200 || applied.Aligned {
		t.Fatalf("first batch failed: %#v", applied)
	}
	second := maintainVolumeBatch(t, session)
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "batch-scope-change", Action: machinecontract.ScopeRoleIndex,
			Pattern: "generated/**", PatternKind: machinecontract.ScopePatternGlob, Reason: "fixture policy change",
			DecisionBasis: machinecontract.ScopeDecisionSemanticDensity, Source: machinecontract.ScopeRuleUser,
			CreatedBy: "batch-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stale := applyVolumeBatch(t, session, codeBatchArguments(second.CodePlan))
	if stale.Status != autoStatusStopped || stale.Applied != 0 || stale.FormalWritesStarted {
		t.Fatalf("scope drift did not invalidate the outstanding batch: %#v", stale)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 201 {
		t.Fatalf("scope drift changed successful entries: count=%d err=%v", set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func TestVolumeCodeSecondBatchRecoveryDoesNotReplayFirstBatch(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	first := maintainVolumeBatch(t, connectMCPClient(t, root))
	if applied := applyVolumeBatch(t, connectMCPClient(t, root), codeBatchArguments(first.CodePlan)); applied.Applied != 200 {
		t.Fatalf("first batch failed: %#v", applied)
	}
	second := maintainVolumeBatch(t, connectMCPClient(t, root))
	items := atomicCodeBatchItems(second.CodePlan)
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if err := original(target, data, expected); err != nil {
			return err
		}
		return errors.New("simulated second-batch interruption")
	}
	interrupted, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || interrupted == nil || interrupted.BaselineComplete || interrupted.AppliedCount != 0 {
		t.Fatalf("second batch did not retain recovery: outcome=%#v fail=%+v", interrupted, fail)
	}
	recovered, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || !recovered.AlreadyApplied || recovered.RecoveredCount != 1 {
		t.Fatalf("second batch recovery did not converge: outcome=%#v fail=%+v", recovered, fail)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 202 {
		t.Fatalf("recovery replayed or lost the first batch: count=%d err=%v", set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func TestVolumeCodeRelationToPendingTargetTriggersMachineReplan(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	arguments := codeBatchArguments(first.CodePlan)
	entries := arguments["entries"].([]map[string]any)
	entries[0]["new_entry"] = filepath.Base(entries[0]["path"].(string)) +
		"[CD5S]: F:implement fixture behavior | R:code:generated/0200.go | A:- | S:Keep the fixture deterministic"
	replanResult := applyVolumeBatch(t, session, arguments)
	if replanResult.Status != autoStatusStopped || replanResult.FormalWritesStarted || replanResult.CodePlan == nil ||
		replanResult.CodePlan.Included != 200 || replanResult.CodePlan.Remaining != 1 {
		t.Fatalf("pending relation did not issue a zero-write replan: %#v", replanResult)
	}
	seenSource, seenTarget := false, false
	for _, candidate := range replanResult.CodePlan.Candidates {
		seenSource = seenSource || candidate.ObjectRef == "code:generated/0000.go"
		seenTarget = seenTarget || candidate.ObjectRef == "code:generated/0200.go"
	}
	if !seenSource || !seenTarget {
		t.Fatalf("relation closure is incomplete: %#v", replanResult.CodePlan)
	}
	replannedArguments := codeBatchArguments(replanResult.CodePlan)
	for _, entry := range replannedArguments["entries"].([]map[string]any) {
		if entry["path"] == "generated/0000.go" {
			entry["new_entry"] = "0000.go[CD5S]: F:implement fixture behavior | R:code:generated/0200.go | A:- | S:Keep the fixture deterministic"
		}
	}
	if applied := applyVolumeBatch(t, session, replannedArguments); applied.Status != autoStatusApplied || applied.Aligned || applied.Applied != 200 {
		t.Fatalf("relation-closed batch failed: %#v", applied)
	}
	last := maintainVolumeBatch(t, session)
	if last.CodePlan == nil || last.CodePlan.Included != 1 {
		t.Fatalf("last relation plan is invalid: %#v", last)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(last.CodePlan)); !applied.Aligned || applied.Applied != 1 {
		t.Fatalf("last relation batch failed: %#v", applied)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Warnings) != 0 || set.Volumes[cognition.ScopeCode].ObjectCount != 202 {
		t.Fatalf("final relation projection is incomplete: warnings=%#v count=%d err=%v",
			set.Warnings, set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

func buildLargeCodeCandidateRepo(t *testing.T, count int) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	for index := 0; index < count; index++ {
		path := filepath.Join(root, "generated", fmt.Sprintf("%04d.go", index))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(fmt.Sprintf("package generated\n\nconst Value%d = %d\n", index, index)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func maintainVolumeBatch(t *testing.T, session *mcp.ClientSession) volumeMaintainResult {
	t.Helper()
	text := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var result volumeMaintainResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func codeBatchArguments(plan *codebatch.Plan) map[string]any {
	entries := make([]map[string]any, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		entries = append(entries, map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"candidate_id": candidate.CandidateID, "new_entry": filepath.Base(candidate.Path) +
				"[CD5S]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic"})
	}
	return map[string]any{"code_batch_id": plan.BatchID, "entries": entries}
}

func applyVolumeBatch(t *testing.T, session *mcp.ClientSession, arguments map[string]any) autoResult {
	t.Helper()
	text := callVolumeTool(t, session, "aoci_update_entry", arguments)
	var result autoResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode update result: %v\n%s", err, text)
	}
	return result
}

func atomicCodeBatchItems(plan *codebatch.Plan) []AtomicUpdateItem {
	result := make([]AtomicUpdateItem, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		result = append(result, AtomicUpdateItem{Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
			CandidateID: candidate.CandidateID, BatchID: plan.BatchID, NewEntry: filepath.Base(candidate.Path) +
				"[CD5S]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic"})
	}
	return result
}
