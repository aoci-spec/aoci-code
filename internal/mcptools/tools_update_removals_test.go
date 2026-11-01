package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// Orphans of 2026-11-01: a Code Entry whose file is gone no longer stops
// Maintain. It rides beside the batch as an orphan_remove_candidate, the
// batch drops it through removals in the same transaction, and the Apply's
// terminal proof reports aligned. Until then the Agent removed each orphan
// with its own aoci_remove_entry call and called Maintain again.
func TestUpdateBatchRemovesOrphansInTheSameTransaction(t *testing.T) {
	root := buildHeldSourcesVolumeRepo(t, true)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeResult(t, root)
	if maintain.Status != autoStatusRepairRequired || maintain.Result != volumegovernance.ResultAuthoringRequired ||
		strings.Join(maintain.OrphanRemovals, ",") != "code:gone.go" || heldCandidatePaths(maintain) != "pkg/util.go" {
		t.Fatalf("an orphan beside authoring work must not block: status=%s result=%s orphans=%v candidates=%s",
			maintain.Status, maintain.Result, maintain.OrphanRemovals, heldCandidatePaths(maintain))
	}
	if !strings.Contains(strings.Join(maintain.Instructions, "\n"), "removals") {
		t.Fatalf("the instructions do not tell the model to drop the orphans in this batch: %v", maintain.Instructions)
	}
	arguments := codeBatchArguments(wireCodePlan(maintain))
	arguments["removals"] = []map[string]any{{"path": "code:gone.go"}}
	applied := applyVolumeBatch(t, session, arguments)
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 2 {
		t.Fatalf("the batch with a removal did not apply to aligned: %#v", applied)
	}
	if applied.TerminalProof == nil || !applied.TerminalProof.Aligned || applied.TerminalProof.GuideNextAction != "none" {
		t.Fatalf("the terminal proof does not close the task: %#v", applied.TerminalProof)
	}
	code, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(code), "gone.go[") || !strings.Contains(string(code), "util.go[") {
		t.Fatalf("the Code Volume after the batch:\n%s", code)
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil || state.Files["aoci.code.txt"].SHA256 != fingerprint.SHA256 {
		t.Fatalf("the Baseline does not bind the Volume the batch wrote: %v", err)
	}
}

// A removal is admitted only for an orphan the facts report. Naming a live
// Entry is a repair finding with zero writes, and the model drops it.
func TestRemovalOfALiveEntryIsRefusedWithZeroWrites(t *testing.T) {
	root := buildHeldSourcesVolumeRepo(t, true)
	session := connectMCPClient(t, root)
	before, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	indexed := ""
	for _, line := range strings.Split(string(before), "\n") {
		if strings.Contains(line, "[") && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "===") {
			indexed = strings.SplitN(line, "[", 2)[0]
			break
		}
	}
	if indexed == "" {
		t.Fatalf("fixture has no live Entry to refuse:\n%s", before)
	}
	result := applyVolumeBatch(t, session, map[string]any{"removals": []map[string]any{{"path": indexed}}})
	if result.Status != autoStatusRepairRequired || result.Applied != 0 || result.FormalWritesStarted ||
		len(result.Findings) != 1 || result.Findings[0].RuleCode != "removal_target_not_orphan" ||
		result.Findings[0].Field != "removals[].path" {
		t.Fatalf("a live Entry's removal must be a zero-write repair finding: %#v", result)
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("the refused removal touched the Volume: %v", err)
	}
	missing := applyVolumeBatch(t, session, map[string]any{"removals": []map[string]any{{"path": "never/indexed.go"}}})
	if missing.Status != autoStatusRepairRequired || len(missing.Findings) != 1 || missing.Findings[0].RuleCode != "removal_target_missing" {
		t.Fatalf("a removal of an unindexed path must name removal_target_missing: %#v", missing)
	}
}

// With nothing to author, Maintain still has orphans to drop: it answers
// remove_orphans_in_update_batch and one call with removals alone closes
// them.
func TestMaintainWithOnlyOrphansAsksForRemovalsInTheBatch(t *testing.T) {
	root := buildHeldSourcesVolumeRepo(t, true)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeResult(t, root)
	arguments := codeBatchArguments(wireCodePlan(maintain))
	arguments["removals"] = []map[string]any{{"path": "gone.go"}}
	if applied := applyVolumeBatch(t, session, arguments); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("fixture did not reach aligned: %#v", applied)
	}
	codePath := filepath.Join(root, "aoci.code.txt")
	text, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	text = append(text, []byte("vanished.go[CD5T]: F:was removed from the tree too | R:- | A:- | S:-\n")...)
	if err := os.WriteFile(codePath, text, 0o644); err != nil {
		t.Fatal(err)
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	state.Files["aoci.code.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	again := maintainVolumeResult(t, root)
	if again.Status != autoStatusRepairRequired || again.NextAction != machinecontract.MaintainActionRemoveOrphansInBatch ||
		again.Batch.NextAction != again.NextAction || len(again.Candidates) != 0 ||
		strings.Join(again.OrphanRemovals, ",") != "code:pkg/vanished.go" || len(again.Instructions) == 0 {
		t.Fatalf("orphans without authoring work must ask for removals in the batch: status=%s next=%s batch_next=%s orphans=%v instructions=%v",
			again.Status, again.NextAction, again.Batch.NextAction, again.OrphanRemovals, again.Instructions)
	}
	closed := applyVolumeBatch(t, session, map[string]any{"removals": []map[string]any{{"path": "code:pkg/vanished.go"}}})
	if closed.Status != autoStatusApplied || !closed.Aligned || closed.Applied != 1 || closed.TerminalProof == nil || !closed.TerminalProof.Aligned {
		t.Fatalf("removals alone did not close the orphans: %#v", closed)
	}
}
