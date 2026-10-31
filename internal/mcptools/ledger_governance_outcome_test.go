package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func ledgerEvents(t *testing.T, root, op string) []ledger.Event {
	t.Helper()
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("Ledger has %d corrupt lines", corrupt)
	}
	matched := []ledger.Event{}
	for _, event := range events {
		if event.Op == op {
			matched = append(matched, event)
		}
	}
	return matched
}

func flagIs(value *bool, want bool) bool {
	return value != nil && *value == want
}

// An auditor reading only the Ledger must tell ordinary authoring, a
// candidate the validator refused, and the write that followed apart, and
// join them by batch (#150).
func TestLedgerTellsAuthoringRefusalAndWriteApart(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 1)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if maintain.CodePlan == nil || maintain.CodePlan.BatchID == "" {
		t.Fatalf("fixture issued no Code batch: %#v", maintain)
	}
	batchID := maintain.CodePlan.BatchID
	maintained := ledgerEvents(t, root, "maintain")
	if len(maintained) != 1 {
		t.Fatalf("maintain events %#v", maintained)
	}
	if event := maintained[0]; event.SchemaVersion != ledger.EventSchemaVersion || event.Result != ledger.ResultRepairRequired ||
		event.GovernanceResult != volumegovernance.ResultAuthoringRequired || event.NextAction != maintain.NextAction ||
		event.BatchID != batchID || !flagIs(event.FormalWritesStarted, false) || event.Blocker != "" {
		t.Fatalf("ordinary authoring is not recognizable: %#v", event)
	}

	arguments := codeBatchArguments(wireCodePlan(maintain))
	entries := arguments["entries"].([]map[string]any)
	valid := entries[0]["new_entry"]
	entries[0]["new_entry"] = "not an entry"
	if refused := applyVolumeBatch(t, session, arguments); refused.Status != autoStatusRepairRequired || refused.FormalWritesStarted {
		t.Fatalf("invalid candidate: %#v", refused)
	}
	entries[0]["new_entry"] = valid
	if applied := applyVolumeBatch(t, session, arguments); applied.Status != autoStatusApplied {
		t.Fatalf("valid batch did not apply: %#v", applied)
	}
	var refusal, write *ledger.Event
	for _, event := range ledgerEvents(t, root, "update_entries_batch") {
		event := event
		switch event.Result {
		case ledger.ResultRepairRequired:
			refusal = &event
		case ledger.ResultOK:
			write = &event
		}
	}
	if refusal == nil || refusal.GovernanceResult != autoStatusRepairRequired || refusal.FailCode == "" ||
		!flagIs(refusal.FormalWritesStarted, false) {
		t.Fatalf("a refused candidate reads as an execution error: %#v", refusal)
	}
	if write == nil || !flagIs(write.FormalWritesStarted, true) || write.BatchID != batchID || write.AppliedCount != 1 {
		t.Fatalf("the write does not join its Maintain: %#v", write)
	}
	// Resubmitting Entries already in place reconciles without a formal write
	// and still joins its Maintain.
	if again := applyVolumeBatch(t, session, arguments); again.Status != autoStatusApplied || again.FormalWritesStarted {
		t.Fatalf("resubmitted batch: %#v", again)
	}
	reconciled := ledgerEvents(t, root, "update_entries_batch_recover")
	if len(reconciled) != 1 || reconciled[0].BatchID != batchID || !flagIs(reconciled[0].FormalWritesStarted, false) {
		t.Fatalf("a reconciled batch does not join its Maintain: %#v", reconciled)
	}
}

func TestLedgerNamesWhatBlockedMaintain(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("observe-only release notes, revised\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := handleMaintainInput(root, "test", maintainIn{}, nil); result.IsError {
		t.Fatalf("maintain failed: %s", resText(t, result))
	}
	events := ledgerEvents(t, root, "maintain")
	if len(events) == 0 {
		t.Fatal("no maintain event")
	}
	event := events[len(events)-1]
	if event.Result != ledger.ResultError || event.GovernanceResult != volumegovernance.ResultBlocked ||
		event.Blocker != "observed_pending" || event.NextAction != machinecontract.ActionReviewObservedEvidence ||
		!flagIs(event.FormalWritesStarted, false) {
		t.Fatalf("an observe review wait is not recognizable: %#v", event)
	}

	legacy := buildRepo(t)
	handleMaintainInput(legacy, "test", maintainIn{}, nil)
	stopped := ledgerEvents(t, legacy, "maintain")
	if len(stopped) == 0 || stopped[len(stopped)-1].Blocker != machinecontract.StatusLegacyReadOnlyMigrateRequired ||
		stopped[len(stopped)-1].NextAction != machinecontract.MaintainActionMigrateLegacyRepository {
		t.Fatalf("the Legacy stop is not recognizable: %#v", stopped)
	}
}
