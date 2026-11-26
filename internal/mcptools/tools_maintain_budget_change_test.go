package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func setMaintainBudget(t *testing.T, root string, budget int) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetMaintainTransportBudgetBytes(budget); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLowerMaintainBudgetPreservesDeliveredSiblings(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 160)
	setMaintainBudget(t, root, 64<<10)
	leader := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, leader)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 || len(first.Candidates) != 50 {
		t.Fatalf("fixture needs a delivered 50-object lead and two unclaimed siblings: %+v", first.CodePlan)
	}
	setMaintainBudget(t, root, 16<<10)
	read := func(raw string) volumeMaintainResult {
		t.Helper()
		var batch volumeMaintainResult
		if err := json.Unmarshal([]byte(raw), &batch); err != nil {
			t.Fatal(err)
		}
		if batch.Status != autoStatusRepairRequired || batch.CodePlan == nil || len(batch.Candidates) == 0 {
			t.Fatalf("Maintain must continue with a complete Code batch, got status=%s candidates=%d next=%q", batch.Status, len(batch.Candidates), batch.NextAction)
		}
		return batch
	}
	byID := func(id string) volumeMaintainResult {
		t.Helper()
		return read(callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", map[string]any{"batch_id": id}))
	}
	assertLead := func(batch volumeMaintainResult) {
		t.Helper()
		if batch.CodePlan.BatchID != first.CodePlan.BatchID || !reflect.DeepEqual(batch.Candidates, first.Candidates) ||
			batch.Batch.TransportBudgetBytes != 16<<10 || !batch.Batch.TransportBudgetExceeded {
			t.Fatalf("redelivery must keep the lead binding and mark the new budget, got id=%s included=%d exceeded=%v",
				batch.CodePlan.BatchID, len(batch.Candidates), batch.Batch.TransportBudgetExceeded)
		}
	}
	assertLead(byID(first.CodePlan.BatchID))
	replay := read(callVolumeTool(t, leader, "aoci_maintain", nil))
	assertLead(replay)
	recalled := read(callVolumeTool(t, leader, "aoci_maintain", map[string]any{"include_instructions": true}))
	assertLead(recalled)
	if recalled.AuthoringMeta == "" || len(recalled.Instructions) == 0 {
		t.Fatal("a Host that lost its old response must be able to recall the full contract")
	}
	worker := connectMCPClient(t, root)
	workerRaw := callVolumeTool(t, worker, "aoci_maintain", nil)
	second := read(workerRaw)
	thirdWorker := connectMCPClient(t, root)
	thirdRaw := callVolumeTool(t, thirdWorker, "aoci_maintain", map[string]any{"batch_id": first.CodePlan.Siblings[1].BatchID})
	third := read(thirdRaw)
	plan, err := codebatch.LoadParallelPlan(root)
	if err != nil || plan == nil {
		t.Fatalf("reload resized plan: %v", err)
	}
	seen := map[string]bool{}
	for i, batch := range []volumeMaintainResult{replay, second, third} {
		if i > 0 {
			old := first.CodePlan.Siblings[i-1]
			if plan.HasSibling(old.BatchID) || !plan.HasSibling(batch.CodePlan.BatchID) || len(batch.Candidates) >= old.Included || batch.Batch.TransportBudgetExceeded {
				t.Fatalf("undelivered sibling %d must be replaced by a smaller binding: %d candidates, exceeded=%v", i, len(batch.Candidates), batch.Batch.TransportBudgetExceeded)
			}
		}
		for _, candidate := range batch.Candidates {
			if seen[candidate.Path] {
				t.Fatalf("overlapping delivered sibling target %s", candidate.Path)
			}
			seen[candidate.Path] = true
		}
	}
	if len(workerRaw) > 16<<10 || len(thirdRaw) > 16<<10 {
		t.Fatalf("replacement responses exceed the new budget: %d and %d bytes", len(workerRaw), len(thirdRaw))
	}
	retired, err := codebatch.LoadReceipt(root, first.CodePlan.Siblings[0].BatchID)
	if err != nil {
		t.Fatal(err)
	}
	retiredPlan := &codebatch.Plan{BatchID: retired.BatchID}
	for _, target := range retired.Targets {
		retiredPlan.Candidates = append(retiredPlan.Candidates, codebatch.Candidate{Target: target})
	}
	before := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	if raw := callVolumeTool(t, worker, "aoci_update_entry", codeBatchArguments(retiredPlan)); !strings.Contains(raw, "code_candidate_plan_stale") {
		t.Fatalf("a replaced receipt still applies: %s", raw)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", before)
	assertLead(read(callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", nil)))
	for _, applied := range []autoResult{
		applyVolumeBatch(t, leader, codeBatchArguments(wireCodePlan(replay))),
		applyVolumeBatch(t, worker, codeBatchArguments(wireCodePlan(second))),
		applyVolumeBatch(t, thirdWorker, codeBatchArguments(wireCodePlan(third))),
	} {
		if applied.Status != autoStatusApplied || applied.Applied == 0 {
			t.Fatalf("delivered binding did not Apply: %+v", applied)
		}
	}
	t.Logf("replayed %d candidates, recut unclaimed siblings to %d and %d", len(replay.Candidates), len(second.Candidates), len(third.Candidates))
	buildToAligned(t, leader)
}

func TestLowerMaintainBudgetPreservesDeliveredOptimizationBatch(t *testing.T) {
	const targets = 145
	root := buildCognitionOptimizationRepo(t, targets)
	setMaintainBudget(t, root, 64<<10)
	first := callCognitionOptimizationMaintain(t, connectMCPClient(t, root), cognitionOptimizationObjectRefs(targets))
	stored, err := cognitionoptimization.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	setMaintainBudget(t, root, 16<<10)
	restarted := connectMCPClient(t, root)
	current := callCognitionOptimizationMaintain(t, restarted, nil)
	if current.Status != autoStatusApplied || current.CodePlan == nil || first.CodePlan.BatchID != current.CodePlan.BatchID ||
		!reflect.DeepEqual(first.Candidates, current.Candidates) || !current.Batch.TransportBudgetExceeded || current.Batch.TransportBudgetBytes != 16<<10 {
		t.Fatalf("optimization redelivery lost its binding or oversize marker: status=%s included=%d exceeded=%v", current.Status, len(current.Candidates), current.Batch.TransportBudgetExceeded)
	}
	for reviewed := 0; reviewed < targets; {
		refs := optimizationCandidateRefs(current.Candidates)
		if !reflect.DeepEqual(refs, stored.Checkpoint.RemainingObjectRefs[reviewed:reviewed+len(refs)]) {
			t.Fatalf("optimization changed the remaining order at object %d", reviewed)
		}
		if reviewed > 0 && (len(refs) >= len(first.Candidates) || (current.Batch.TransportBudgetExceeded && len(refs) != 1)) {
			t.Fatalf("an unissued optimization batch did not fit the new budget: %d objects, exceeded=%v", len(refs), current.Batch.TransportBudgetExceeded)
		}
		closed := callCognitionOptimizationUpdate(t, restarted, optimizationUpdateArguments(current, nil))
		reviewed += len(refs)
		if closed.Status != autoStatusApplied || closed.Optimization == nil || closed.Optimization.Reviewed != reviewed {
			t.Fatalf("optimization Apply did not progress: %+v", closed)
		}
		if reviewed == targets {
			if closed.Optimization.State != "complete" || !closed.Aligned {
				t.Fatalf("optimization did not finish: %+v", closed)
			}
			break
		}
		current = callCognitionOptimizationMaintain(t, restarted, nil)
		if len(current.Candidates) == 0 {
			t.Fatal("optimization returned an empty pending batch")
		}
	}
	t.Logf("replayed %d delivered optimization candidates and reviewed all %d objects", len(first.Candidates), targets)
}

func TestMaintainCapsCombinedCandidateAndOrphanBatch(t *testing.T) {
	for _, fixture := range []struct {
		name             string
		orphans, creates int
	}{{"orphan_only", 205, 0}, {"mixed", 200, 1}} {
		t.Run(fixture.name, func(t *testing.T) {
			root := buildCognitionOptimizationRepo(t, fixture.orphans)
			for i := range fixture.orphans {
				if err := os.Remove(filepath.Join(root, "optimized", fmt.Sprintf("%04d.go", i))); err != nil {
					t.Fatal(err)
				}
			}
			if fixture.creates > 0 {
				writeVolumeTestFile(t, root, "optimized/new.go", "package optimized\n")
			}
			session := connectMCPClient(t, root)
			remaining := fixture.orphans + fixture.creates
			for round := 1; remaining > 0; round++ {
				batch := maintainVolumeBatch(t, session)
				count := len(batch.Candidates) + len(batch.OrphanRemovals)
				if batch.OrphanRemovalTotal != remaining-len(batch.Candidates) || count == 0 || count > machinecontract.EntriesBatchMaxItems || batch.Batch.Remaining != remaining-count ||
					batch.Batch.ContinuationRequired != (remaining > count) {
					t.Fatalf("invalid batch %d: %d candidates + %d of %d removals, remaining=%d", round, len(batch.Candidates), len(batch.OrphanRemovals), batch.OrphanRemovalTotal, batch.Batch.Remaining)
				}
				args := map[string]any{}
				if len(batch.Candidates) > 0 {
					args = codeBatchArguments(wireCodePlan(batch))
				}
				removals := make([]map[string]string, 0, len(batch.OrphanRemovals))
				for _, path := range batch.OrphanRemovals {
					removals = append(removals, map[string]string{"path": path})
				}
				args["removals"] = removals
				applied := applyVolumeBatch(t, session, args)
				remaining -= count
				if applied.Status != autoStatusApplied || applied.Applied != count || applied.Aligned != (remaining == 0) {
					t.Fatalf("batch %d Apply did not progress: %+v", round, applied)
				}
				t.Logf("batch %d: %d candidates + %d removals, aligned=%v", round, len(batch.Candidates), len(removals), applied.Aligned)
			}
		})
	}
}
