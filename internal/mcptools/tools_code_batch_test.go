package mcptools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestVolumeCodeCandidateBindingTypoReturnsExactZeroWriteRepairAndSameBatchSucceeds(t *testing.T) {
	tests := []struct {
		name  string
		field string
		alter func(map[string]any) string
	}{
		{name: "path", field: "path", alter: func(entry map[string]any) string {
			actual := entry["path"].(string) + "x"
			entry["path"] = actual
			return actual
		}},
		{name: "candidate_id", field: "candidate_id", alter: func(entry map[string]any) string {
			actual := oneHexTypo(entry["candidate_id"].(string))
			entry["candidate_id"] = actual
			return actual
		}},
		{name: "source_sha256", field: "source_sha256", alter: func(entry map[string]any) string {
			actual := oneHexTypo(entry["source_sha256"].(string))
			entry["source_sha256"] = actual
			return actual
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			if maintain.CodePlan == nil || len(maintain.CodePlan.Candidates) != 2 {
				t.Fatalf("expected two Code candidates: %#v", maintain.CodePlan)
			}
			arguments := codeBatchArguments(maintain.CodePlan)
			entries := arguments["entries"].([]map[string]any)
			expectedCandidate := maintain.CodePlan.Candidates[1]
			actual := test.alter(entries[1])
			codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
			baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

			rejected := applyVolumeBatch(t, session, arguments)
			if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted ||
				len(rejected.Findings) != 1 || !rejected.PreserveOtherCandidates {
				t.Fatalf("binding typo did not return one zero-write repair: %#v", rejected)
			}
			finding := rejected.Findings[0]
			if finding.CandidateIndex != 2 || finding.Path != expectedCandidate.Path ||
				finding.CanonicalObjectIdentity != expectedCandidate.ObjectRef || finding.Domain != cognition.ScopeCode ||
				finding.Field != test.field || finding.Expected == "" || finding.Actual != actual ||
				finding.Cause == "" || finding.SafeRepairAction == "" {
				t.Fatalf("binding typo diagnostic is incomplete or ambiguous: %+v", finding)
			}
			if len(rejected.RetryScope) != 1 || rejected.RetryScope[0] != expectedCandidate.ObjectRef {
				t.Fatalf("binding typo repair scope is not exact: %#v", rejected.RetryScope)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
			assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)

			corrected := codeBatchArguments(maintain.CodePlan)
			applied := applyVolumeBatch(t, session, corrected)
			if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 2 {
				t.Fatalf("corrected unchanged machine batch did not apply: %#v", applied)
			}
			assertVolumeTerminalProofAction(t, applied.NextAction, false)
			duplicate := applyVolumeBatch(t, session, corrected)
			if duplicate.Status != autoStatusApplied || !duplicate.Aligned || duplicate.Applied != 0 ||
				duplicate.Metrics.DuplicateApplies != 1 {
				t.Fatalf("repeated final Code batch was not idempotent: %#v", duplicate)
			}
			assertVolumeTerminalProofAction(t, duplicate.NextAction, true)
		})
	}
}

func TestVolumeCodeCandidateBindingTypoWithSourceDriftStaysStopped(t *testing.T) {
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "candidate_id", alter: func(entry map[string]any) {
			entry["candidate_id"] = oneHexTypo(entry["candidate_id"].(string))
		}},
		{name: "source_sha256", alter: func(entry map[string]any) {
			entry["source_sha256"] = oneHexTypo(entry["source_sha256"].(string))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			if maintain.CodePlan == nil || len(maintain.CodePlan.Candidates) != 2 {
				t.Fatalf("expected two Code candidates: %#v", maintain.CodePlan)
			}
			arguments := codeBatchArguments(maintain.CodePlan)
			test.alter(arguments["entries"].([]map[string]any)[1])
			driftPath := filepath.Join(root, filepath.FromSlash(maintain.CodePlan.Candidates[0].Path))
			data, err := os.ReadFile(driftPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(driftPath, append(data, []byte("// drift\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
			baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

			rejected := applyVolumeBatch(t, session, arguments)
			if rejected.Status != autoStatusStopped || rejected.Applied != 0 || rejected.FormalWritesStarted ||
				rejected.PreserveOtherCandidates || len(rejected.RetryScope) != 0 {
				t.Fatalf("source drift with a binding typo was downgraded to repair: %#v", rejected)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
			assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
		})
	}
}

func TestVolumeCodeConflictingValidBindingsRequireUniqueSourceAnchor(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 2)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if maintain.CodePlan == nil || len(maintain.CodePlan.Candidates) != 2 {
		t.Fatalf("expected two Code candidates: %#v", maintain.CodePlan)
	}
	arguments := codeBatchArguments(maintain.CodePlan)
	entries := arguments["entries"].([]map[string]any)
	entries[0]["path"], entries[1]["path"] = entries[1]["path"], entries[0]["path"]
	codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

	rejected := applyVolumeBatch(t, session, arguments)
	if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted ||
		len(rejected.Findings) != 2 || !rejected.PreserveOtherCandidates {
		t.Fatalf("uniquely source-bound path swap did not return exact repairs: %#v", rejected)
	}
	findings := map[int]cognition.RepairFinding{}
	for _, finding := range rejected.Findings {
		findings[finding.CandidateIndex] = finding
	}
	for index, candidate := range maintain.CodePlan.Candidates {
		finding := findings[index+1]
		if finding.Field != "path" || finding.Path != candidate.Path ||
			finding.CanonicalObjectIdentity != candidate.ObjectRef ||
			finding.Expected != candidate.Path || finding.Actual != maintain.CodePlan.Candidates[1-index].Path {
			t.Fatalf("candidate %d lost its unique source anchor: %+v", index+1, finding)
		}
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)

	applied := applyVolumeBatch(t, session, codeBatchArguments(maintain.CodePlan))
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 2 {
		t.Fatalf("corrected uniquely source-bound batch did not apply: %#v", applied)
	}
}

func TestVolumeCodeConflictingValidBindingsWithSharedSourceStayStopped(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 2)
	firstPath := filepath.Join(root, "generated", "0000.go")
	sharedSource, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated", "0001.go"), sharedSource, 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if maintain.CodePlan == nil || len(maintain.CodePlan.Candidates) != 2 ||
		maintain.CodePlan.Candidates[0].SourceSHA256 != maintain.CodePlan.Candidates[1].SourceSHA256 {
		t.Fatalf("fixture did not create two candidates with shared source bytes: %#v", maintain.CodePlan)
	}
	arguments := codeBatchArguments(maintain.CodePlan)
	entries := arguments["entries"].([]map[string]any)
	entries[0]["path"], entries[1]["path"] = entries[1]["path"], entries[0]["path"]
	codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

	rejected := applyVolumeBatch(t, session, arguments)
	if rejected.Status != autoStatusStopped || rejected.Applied != 0 || rejected.FormalWritesStarted ||
		rejected.PreserveOtherCandidates || len(rejected.RetryScope) != 0 {
		t.Fatalf("ambiguous path/candidate bindings were downgraded to repair: %#v", rejected)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
}

func TestVolumeCodeWrongBatchDoesNotRepairUnprovenReceipt(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, *volumeMaintainResult, map[string]any)
	}{
		{name: "incomplete", mutate: func(t *testing.T, _ string, _ *volumeMaintainResult, arguments map[string]any) {
			entries := arguments["entries"].([]map[string]any)
			arguments["entries"] = entries[:len(entries)-1]
		}},
		{name: "stale", mutate: func(t *testing.T, root string, maintain *volumeMaintainResult, _ map[string]any) {
			path := filepath.Join(root, filepath.FromSlash(maintain.CodePlan.Candidates[0].Path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("// drift\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ambiguous", mutate: func(t *testing.T, root string, maintain *volumeMaintainResult, _ map[string]any) {
			directory := filepath.Join(root, ".aoci", "drafts", "code-cognition")
			data, err := os.ReadFile(filepath.Join(directory, "candidate-"+maintain.CodePlan.BatchID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			alias := strings.Repeat("e", 64)
			if alias == maintain.CodePlan.BatchID || alias == maintain.Batch.BatchIdentity {
				alias = strings.Repeat("f", 64)
			}
			if err := os.WriteFile(filepath.Join(directory, "candidate-"+alias+".json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := buildLargeCodeCandidateRepo(t, 2)
			session := connectMCPClient(t, root)
			maintain := maintainVolumeBatch(t, session)
			if maintain.CodePlan == nil || maintain.Batch.BatchIdentity == "" ||
				maintain.Batch.BatchIdentity == maintain.CodePlan.BatchID {
				t.Fatalf("fixture did not expose distinct batch identities: %#v", maintain)
			}
			arguments := codeBatchArguments(maintain.CodePlan)
			arguments["code_batch_id"] = maintain.Batch.BatchIdentity
			test.mutate(t, root, &maintain, arguments)
			codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
			baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

			rejected := applyVolumeBatch(t, session, arguments)
			if rejected.Status != autoStatusStopped || rejected.Applied != 0 || rejected.FormalWritesStarted ||
				rejected.PreserveOtherCandidates || len(rejected.RetryScope) != 0 {
				t.Fatalf("unproven wrong batch was downgraded to repair: %#v", rejected)
			}
			assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
			assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
		})
	}
}

func TestVolumeCodeAuthoringEnvelopeIdentityExplainsDomainBatchAndStaysZeroWrite(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 1)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if maintain.CodePlan == nil || maintain.Batch.BatchIdentity == "" ||
		maintain.Batch.BatchIdentity == maintain.CodePlan.BatchID {
		t.Fatalf("fixture did not expose distinct envelope and Code batch identities: %#v", maintain)
	}
	arguments := codeBatchArguments(maintain.CodePlan)
	arguments["code_batch_id"] = maintain.Batch.BatchIdentity
	codeBefore := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))

	rejected := applyVolumeBatch(t, session, arguments)
	if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted ||
		len(rejected.Findings) != 1 || !rejected.PreserveOtherCandidates || len(rejected.RetryScope) != 0 {
		t.Fatalf("envelope identity misuse did not return a top-level zero-write repair: %#v", rejected)
	}
	finding := rejected.Findings[0]
	if finding.CandidateIndex != 1 || finding.Path != maintain.CodePlan.Candidates[0].Path ||
		finding.Field != "code_batch_id" || finding.RuleCode != "code_candidate_batch_id_mismatch" ||
		!strings.Contains(finding.Expected, "code_plan.batch_id="+maintain.CodePlan.BatchID) ||
		!strings.Contains(finding.Expected, "candidates[].batch_id="+maintain.CodePlan.BatchID) ||
		!strings.Contains(finding.Actual, "code_batch_id="+maintain.Batch.BatchIdentity) ||
		!strings.Contains(finding.Actual, "authoring_batch.batch_identity") || finding.Cause == "" ||
		finding.SafeRepairAction == "" {
		t.Fatalf("envelope identity diagnostic did not name both identities: %+v", finding)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", codeBefore)
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)

	applied := applyVolumeBatch(t, session, codeBatchArguments(maintain.CodePlan))
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 1 {
		t.Fatalf("same machine batch did not apply after correcting only code_batch_id: %#v", applied)
	}
}

func oneHexTypo(value string) string {
	if strings.HasSuffix(value, "0") {
		return strings.TrimSuffix(value, "0") + "1"
	}
	return value[:len(value)-1] + "0"
}

func readCodeBatchFormalAsset(t *testing.T, root, rel string) []byte {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertCodeBatchFormalAssetUnchanged(t *testing.T, root, rel string, before []byte) {
	t.Helper()
	after := readCodeBatchFormalAsset(t, root, rel)
	if !bytes.Equal(before, after) {
		t.Fatalf("zero-write repair changed formal asset %s", rel)
	}
}

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

// 指向"下一批才会创作"的对象是完全正常的创作行为: 模型是按批看到代码的, 它
// 当然会写下还没落地的关系。机器既不重排也不阻断, 两批照常滚完, 索引照常建成。
//
// 这条曾经是反的 —— 那时一次跨批的 R 会触发零写重排, 于是在关系稠密的真仓上
// 反复重排、永不收敛, 直接把 400+ 文件仓库的索引建立卡死。
func TestVolumeCodeRelationToLaterBatchAppliesWithoutReplan(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 201)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	arguments := codeBatchArguments(first.CodePlan)
	entries := arguments["entries"].([]map[string]any)
	entries[0]["new_entry"] = filepath.Base(entries[0]["path"].(string)) +
		"[CD5S]: F:implement fixture behavior | R:code:generated/0200.go | A:- | S:Keep the fixture deterministic"
	applied := applyVolumeBatch(t, session, arguments)
	if applied.Status != autoStatusApplied || applied.Aligned || applied.Applied != 200 || applied.CodePlan != nil {
		t.Fatalf("跨批关系不应触发重排: %#v", applied)
	}
	last := maintainVolumeBatch(t, session)
	if last.CodePlan == nil || last.CodePlan.Included != 1 {
		t.Fatalf("末批计划不对: %#v", last)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(last.CodePlan)); !applied.Aligned || applied.Applied != 1 {
		t.Fatalf("末批失败: %#v", applied)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Warnings) != 0 || set.Volumes[cognition.ScopeCode].ObjectCount != 202 {
		t.Fatalf("最终索引不完整: warnings=%#v count=%d err=%v",
			set.Warnings, set.Volumes[cognition.ScopeCode].ObjectCount, err)
	}
}

// 环形互指是真实代码里的常态(A 调 B, B 回调 A)。整个环远大于单批上限时, 旧机制
// 会报"最小不可拆成分 203 > 200"并彻底停下 —— 那正是用户真仓的死局。现在关系
// 不再是排程约束, 同样的形状必须一路滚完。
func TestVolumeCodeMutualRelationsAcrossBatchesStillCompleteTheIndex(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 260)
	session := connectMCPClient(t, root)
	total := 0
	for round := 0; round < 4; round++ {
		plan := maintainVolumeBatch(t, session)
		if plan.CodePlan == nil {
			break
		}
		arguments := codeBatchArguments(plan.CodePlan)
		entries := arguments["entries"].([]map[string]any)
		// 每条都指向全集里的下一个对象, 首尾相接 —— 一个跨越所有批次的大环。
		for _, entry := range entries {
			path := entry["path"].(string)
			var index int
			if _, err := fmt.Sscanf(filepath.Base(path), "%04d.go", &index); err != nil {
				t.Fatal(err)
			}
			entry["new_entry"] = fmt.Sprintf("%s[CD5S]: F:implement fixture behavior | R:code:generated/%04d.go | A:- | S:Keep the fixture deterministic",
				filepath.Base(path), (index+1)%260)
		}
		applied := applyVolumeBatch(t, session, arguments)
		if applied.Status == autoStatusStopped || applied.CodePlan != nil {
			t.Fatalf("第 %d 批被关系环卡住: %#v", round, applied)
		}
		total += applied.Applied
		if applied.Aligned {
			break
		}
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeCode].ObjectCount != 261 {
		t.Fatalf("大环仓库未能建成索引: applied=%d count=%d err=%v",
			total, set.Volumes[cognition.ScopeCode].ObjectCount, err)
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
