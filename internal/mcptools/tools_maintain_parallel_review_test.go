package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// callVolumeToolQuiet is callVolumeTool for a goroutine: it returns the text
// or the error as text and never touches the test.
func callVolumeToolQuiet(session *mcp.ClientSession, name string, arguments map[string]any) string {
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "call failed: " + err.Error()
	}
	text := ""
	for _, content := range result.Content {
		if item, ok := content.(*mcp.TextContent); ok {
			text += item.Text
		}
	}
	return text
}

func setBatchEntries(t *testing.T, root string, entries int) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionBatchEntries(entries); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

// A sibling drops an orphan beside its Entries: the delete is recorded with
// the Entry it removed, the later siblings replay it, and the wave reaches
// aligned with no receipt left behind.
func TestASiblingDropsAnOrphanAndThePlanCompletes(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 12)
	session := connectMCPClient(t, root)
	buildToAligned(t, session)
	for index := 0; index < 8; index++ {
		path := filepath.Join(root, "pkg", fmt.Sprintf("mod%03d", index/10), fmt.Sprintf("file%03d.go", index))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n// changed\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "pkg", "mod001", "file011.go")); err != nil {
		t.Fatal(err)
	}
	setBatchEntries(t, root, 3)
	wave := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, wave)
	if first.CodePlan == nil || first.CodePlan.LogicalPlan == "" || len(first.OrphanRemovals) != 1 {
		t.Fatalf("the wave must issue siblings beside one orphan: plan=%#v orphans=%v", first.CodePlan, first.OrphanRemovals)
	}
	for round := 1; ; round++ {
		if round > 20 {
			t.Fatal("the wave with an orphan did not converge")
		}
		maintain := maintainVolumeBatch(t, wave)
		if maintain.Aligned {
			break
		}
		arguments := map[string]any{}
		if maintain.CodePlan != nil && len(maintain.Candidates) > 0 {
			entries := []map[string]any{}
			for _, candidate := range maintain.Candidates {
				entries = append(entries, map[string]any{"path": candidate.Path, "new_entry": filepath.Base(candidate.Path) +
					fmt.Sprintf("[CD5S]: F:implement fixture behavior, wave %d | R:- | A:- | S:Keep the fixture deterministic", round)})
			}
			arguments["code_batch_id"], arguments["entries"] = maintain.CodePlan.BatchID, entries
		}
		if len(maintain.OrphanRemovals) > 0 {
			removals := []map[string]any{}
			for _, orphan := range maintain.OrphanRemovals {
				removals = append(removals, map[string]any{"path": orphan})
			}
			arguments["removals"] = removals
		}
		applied := applyVolumeBatch(t, wave, arguments)
		if applied.Status != autoStatusApplied {
			t.Fatalf("round %d: a sibling with a removal did not apply: %#v", round, applied)
		}
		if applied.Aligned {
			break
		}
	}
	if pending, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "*.json")); len(pending) != 0 {
		t.Fatalf("a receipt was left pending: %v", pending)
	}
}

// A single batch a Host took before the parallel setting rose (an upgrade
// that changed the default, or 1 changed to 3) has the batch id the first
// sibling gets. Maintain must still issue work, never answer aligned over a
// receipt conflict, and the batch the Host holds must still apply.
func TestASingleBatchTakenBeforeTheSettingRoseStillApplies(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 12)
	setBatchEntries(t, root, 3)
	pinSingleBatch(t, root)
	held := maintainVolumeBatch(t, connectMCPClient(t, root))
	if held.CodePlan == nil || held.CodePlan.LogicalPlan != "" || len(held.Candidates) != 3 {
		t.Fatalf("the pinned repository must issue one single batch: %#v", held.CodePlan)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionParallelBatches(3); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	for attempt := 0; attempt < 2; attempt++ {
		after := maintainVolumeBatch(t, session)
		if after.Aligned || after.Status != autoStatusRepairRequired || len(after.Candidates) == 0 {
			t.Fatalf("Maintain must keep issuing work after the setting rose: status=%s aligned=%v candidates=%d", after.Status, after.Aligned, len(after.Candidates))
		}
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(held))); applied.Status != autoStatusApplied || applied.Applied != 3 {
		t.Fatalf("the batch taken before the change no longer applies: %#v", applied)
	}
	buildToAligned(t, session)
}

// Two servers that take different siblings at the same moment are both on
// record as delivered, so neither sibling can be released under its Agent.
func TestSiblingsTakenAtOnceAreBothOnRecord(t *testing.T) {
	for run := 0; run < 5; run++ {
		root := buildManyFileVolumesRepo(t, 150)
		first := maintainVolumeBatch(t, connectMCPClient(t, root))
		if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
			t.Fatalf("expected three siblings: %#v", first.CodePlan)
		}
		sessions := []*mcp.ClientSession{connectMCPClient(t, root), connectMCPClient(t, root)}
		var group sync.WaitGroup
		answers := make([]string, 2)
		for index := range sessions {
			group.Add(1)
			go func(index int) {
				defer group.Done()
				answers[index] = callVolumeToolQuiet(sessions[index], "aoci_maintain", map[string]any{"batch_id": first.CodePlan.Siblings[index].BatchID})
			}(index)
		}
		group.Wait()
		for index, answer := range answers {
			if !strings.Contains(answer, `"status":"repair_required"`) {
				t.Fatalf("sibling %d was not delivered: %.200s", index, answer)
			}
		}
		plan, err := codebatch.LoadParallelPlan(root)
		if err != nil || plan == nil {
			t.Fatalf("the plan is gone: %v", err)
		}
		onRecord := strings.Join(plan.Delivered, ",")
		for _, sibling := range first.CodePlan.Siblings {
			if !strings.Contains(onRecord, sibling.BatchID) {
				t.Fatalf("run %d: a delivery was lost: delivered=%v wanted %s", run, plan.Delivered, sibling.BatchID)
			}
		}
	}
}

// Under the default every response of a first build with long paths stays
// inside the transport budget: the first, which lists siblings, and a
// sibling delivered with the contract a fresh session needs.
func TestDefaultSiblingResponsesStayUnderBudgetWithLongPaths(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	directory := filepath.Join("ruoyi-admin", "src", "main", "java", "com", "example", "web", "controller", "system", "monitor", strings.Repeat("deep", 20))
	for i := 0; i < 120; i++ {
		rel := filepath.Join(directory, fmt.Sprintf("SysOperationLogQueryController%03d.java", i))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(fmt.Sprintf("package monitor;\n\npublic class C%03d {}\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	plan := codePlanOf(t, decodeMaintainJSON(t, raw))
	siblings := siblingsOf(plan)
	if len(siblings) == 0 {
		t.Fatalf("the fixture must issue siblings: %v", plan)
	}
	if len(raw) > machinecontract.MaintainTransportBudgetBytesDefault {
		t.Fatalf("the first response is %d bytes, over the budget %d", len(raw), machinecontract.MaintainTransportBudgetBytesDefault)
	}
	for _, sibling := range siblings {
		delivered := callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", map[string]any{"batch_id": sibling["batch_id"], "include_instructions": true})
		if len(delivered) > machinecontract.MaintainTransportBudgetBytesDefault {
			t.Fatalf("a sibling delivered with the contract is %d bytes, over the budget %d", len(delivered), machinecontract.MaintainTransportBudgetBytesDefault)
		}
	}
}

// A batch that drops an orphan beside its Entries and is cut after its
// Volume write must close when the same batch is submitted again: the
// removal already landed, which is the postimage its receipt proves.
func TestAnInterruptedBatchWithARemovalClosesOnResubmission(t *testing.T) {
	for _, pinned := range []bool{true, false} {
		root := buildManyFileVolumesRepo(t, 12)
		if pinned {
			pinSingleBatch(t, root)
		}
		session := connectMCPClient(t, root)
		buildToAligned(t, session)
		for index := 0; index < 2; index++ {
			path := filepath.Join(root, "pkg", fmt.Sprintf("mod%03d", index/10), fmt.Sprintf("file%03d.go", index))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\n// changed\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(filepath.Join(root, "pkg", "mod001", "file011.go")); err != nil {
			t.Fatal(err)
		}
		wave := connectMCPClient(t, root)
		maintain := maintainVolumeBatch(t, wave)
		entries := []map[string]any{}
		for _, candidate := range maintain.Candidates {
			entries = append(entries, map[string]any{"path": candidate.Path, "new_entry": filepath.Base(candidate.Path) +
				"[CD5S]: F:implement fixture behavior after the change | R:- | A:- | S:Keep the fixture deterministic"})
		}
		if len(entries) == 0 || len(maintain.OrphanRemovals) != 1 {
			t.Fatalf("pinned=%v: expected work beside one orphan: %d %v", pinned, len(entries), maintain.OrphanRemovals)
		}
		arguments := map[string]any{"code_batch_id": maintain.CodePlan.BatchID, "entries": entries,
			"removals": []map[string]any{{"path": maintain.OrphanRemovals[0]}}}
		originalSave := saveAtomicBaseline
		saveAtomicBaseline = func(string, *baseline.Baseline) error { return fmt.Errorf("injected baseline save failure") }
		cut := applyVolumeBatch(t, wave, arguments)
		saveAtomicBaseline = originalSave
		if cut.Status == autoStatusApplied && cut.Aligned {
			t.Fatalf("pinned=%v: the injected failure did not cut the write: %#v", pinned, cut)
		}
		again := applyVolumeBatch(t, wave, arguments)
		if again.Status != autoStatusApplied {
			t.Fatalf("pinned=%v: the same batch did not close its interrupted write: status=%s findings=%v", pinned, again.Status, again.Findings)
		}
		if pending, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "*.json")); len(pending) != 0 {
			t.Fatalf("pinned=%v: a receipt was left pending: %v", pinned, pending)
		}
	}
}

// Planning accepts any state a plan has been in, so the write must prove,
// under the lock, that the Volume is the plan's latest state. A Volume put
// back to an earlier state of the plan is a foreign change: the sibling
// still out is refused with zero writes.
func TestASiblingIsRefusedOverAVolumePutBackToAnEarlierPlanState(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("expected three siblings: %#v", first.CodePlan)
	}
	fetch := func(batchID string) volumeMaintainResult {
		var delivered volumeMaintainResult
		if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": batchID})), &delivered); err != nil {
			t.Fatal(err)
		}
		return delivered
	}
	second, third := fetch(first.CodePlan.Siblings[0].BatchID), fetch(first.CodePlan.Siblings[1].BatchID)
	volumeAtStart := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	baselineAtStart := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
	for _, batch := range []volumeMaintainResult{first, second} {
		if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(batch))); applied.Status != autoStatusApplied {
			t.Fatalf("a sibling did not apply: %#v", applied)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), volumeAtStart, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), baselineAtStart, 0o644); err != nil {
		t.Fatal(err)
	}
	refused := applyVolumeBatch(t, connectMCPClient(t, root), codeBatchArguments(wireCodePlan(third)))
	if refused.Status == autoStatusApplied || refused.Applied != 0 || refused.FormalWritesStarted {
		t.Fatalf("a sibling wrote over a Volume put back to the plan's start: %#v", refused)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, "aoci.code.txt", volumeAtStart)
}

// Two siblings submitted at the same moment, one dropping an orphan: the
// one that lands second is projected again onto the first's postimage, and
// its removal must be projected as a removal.
func TestSiblingsAppliedAtOnceWithARemovalBothApply(t *testing.T) {
	for run := 0; run < 6; run++ {
		root := buildManyFileVolumesRepo(t, 20)
		buildToAligned(t, connectMCPClient(t, root))
		for index := 0; index < 17; index++ {
			path := filepath.Join(root, "pkg", fmt.Sprintf("mod%03d", index/10), fmt.Sprintf("file%03d.go", index))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\n// changed\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(filepath.Join(root, "pkg", "mod001", "file019.go")); err != nil {
			t.Fatal(err)
		}
		setBatchEntries(t, root, 9)
		sessions := []*mcp.ClientSession{connectMCPClient(t, root), connectMCPClient(t, root)}
		first := maintainVolumeBatch(t, sessions[0])
		if first.CodePlan == nil || len(first.CodePlan.Siblings) != 1 || len(first.OrphanRemovals) != 1 {
			t.Fatalf("expected two siblings beside one orphan: %#v %v", first.CodePlan, first.OrphanRemovals)
		}
		var second volumeMaintainResult
		if err := json.Unmarshal([]byte(callVolumeTool(t, sessions[1], "aoci_maintain", map[string]any{"batch_id": first.CodePlan.Siblings[0].BatchID})), &second); err != nil {
			t.Fatal(err)
		}
		request := func(batch volumeMaintainResult, removals bool) map[string]any {
			entries := []map[string]any{}
			for _, candidate := range batch.Candidates {
				entries = append(entries, map[string]any{"path": candidate.Path, "new_entry": filepath.Base(candidate.Path) +
					"[CD5S]: F:implement fixture behavior after the change | R:- | A:- | S:Keep the fixture deterministic"})
			}
			arguments := map[string]any{"code_batch_id": batch.CodePlan.BatchID, "entries": entries}
			if removals {
				arguments["removals"] = []map[string]any{{"path": first.OrphanRemovals[0]}}
			}
			return arguments
		}
		requests := []map[string]any{request(first, true), request(second, false)}
		answers := make([]string, 2)
		var group sync.WaitGroup
		for index := range sessions {
			group.Add(1)
			go func(index int) {
				defer group.Done()
				answers[index] = callVolumeToolQuiet(sessions[index], "aoci_update_entry", requests[index])
			}(index)
		}
		group.Wait()
		for index, answer := range answers {
			if !strings.Contains(answer, `"status":"applied"`) {
				t.Fatalf("run %d: sibling %d submitted at once was not applied: %.400s", run, index, answer)
			}
		}
	}
}

// The same proof holds for a sibling that only rebinds its sources: it
// writes no Volume but moves the Baseline, so over a Volume put back to an
// earlier plan state it is refused and the Baseline stays as it was.
func TestARebindingSiblingIsRefusedOverAVolumePutBack(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	buildToAligned(t, connectMCPClient(t, root))
	for index := 0; index < 150; index++ {
		path := filepath.Join(root, "pkg", fmt.Sprintf("mod%03d", index/10), fmt.Sprintf("file%03d.go", index))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n// wave\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) < 2 {
		t.Fatalf("expected at least three siblings: %#v", first.CodePlan)
	}
	fetch := func(batchID string) volumeMaintainResult {
		var delivered volumeMaintainResult
		if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": batchID})), &delivered); err != nil {
			t.Fatal(err)
		}
		return delivered
	}
	second, third := fetch(first.CodePlan.Siblings[0].BatchID), fetch(first.CodePlan.Siblings[1].BatchID)
	volumeAtStart := readCodeBatchFormalAsset(t, root, "aoci.code.txt")
	rewrite := func(batch volumeMaintainResult, rebindOnly bool) map[string]any {
		entries := []map[string]any{}
		for _, candidate := range batch.Candidates {
			entry := candidate.ExistingEntry
			if !rebindOnly {
				entry = filepath.Base(candidate.Path) + "[CD5S]: F:implement fixture behavior after the wave | R:- | A:- | S:Keep the fixture deterministic"
			}
			entries = append(entries, map[string]any{"path": candidate.Path, "new_entry": entry})
		}
		return map[string]any{"code_batch_id": batch.CodePlan.BatchID, "entries": entries}
	}
	for _, batch := range []volumeMaintainResult{first, second} {
		if applied := applyVolumeBatch(t, session, rewrite(batch, false)); applied.Status != autoStatusApplied {
			t.Fatalf("a sibling did not apply: %#v", applied)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), volumeAtStart, 0o644); err != nil {
		t.Fatal(err)
	}
	baselineBefore := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
	refused := applyVolumeBatch(t, connectMCPClient(t, root), rewrite(third, true))
	if refused.Status == autoStatusApplied {
		t.Fatalf("a rebinding sibling was accepted over a Volume put back: %#v", refused)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), baselineBefore)
}
