package mcptools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestVolumeRemoveReceiptVersionProtectsOlderReaders(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	original := removeRecoveryFile
	removeRecoveryFile = func(string) error { return errors.New("injected cleanup failure") }
	t.Cleanup(func() { removeRecoveryFile = original })
	if _, fail := ApplyRemoveEntry(root, "code:main.go", ledger.SourceAgent, true, false); fail == nil {
		t.Fatal("expected cleanup failure")
	}
	data, err := os.ReadFile(removeRecoveryPath(root, "code:main.go"))
	if err != nil {
		t.Fatal(err)
	}
	var olderReader struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &olderReader); err != nil || olderReader.Version != 2 {
		t.Fatalf("older readers must reject the new receipt version: %+v, %v", olderReader, err)
	}
	receipt, err := loadRemoveRecovery(root, "code:main.go")
	if err != nil || !validRecoverySHA256(receipt.LedgerTransactionID) {
		t.Fatalf("new reader must retain transaction identity: %+v, %v", receipt, err)
	}
}

func TestVolumeRemoveLedgerIdentityRecovery(t *testing.T) {
	for _, mode := range []string{"historical removal", "legacy completed", "same removal conflict"} {
		t.Run(mode, func(t *testing.T) {
			root := buildVolumeRepo(t, true, false)
			if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
				t.Fatal(err)
			}
			plan, fail := planVolumeRemoveEntry(root, "code:main.go", true)
			if fail != nil {
				t.Fatal(fail)
			}
			receiptFromPlan(t, root, plan)
			if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(plan.newText), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), []byte(plan.baselinePostimage), 0o644); err != nil {
				t.Fatal(err)
			}
			old := ledger.Event{Op: "remove_entry", Source: ledger.SourceAgent, Result: ledger.ResultOK, AppliedCount: 1,
				RecoveryTransactionID: removeTransactionID(root, "code:main.go"), PreIndexSHA256: plan.indexHash,
				PostIndexSHA256: indexTextHash(plan.newText), IndexSHA256: indexTextHash(plan.newText), BaselineSHA256: plan.baselinePostSHA256}
			if mode == "historical removal" {
				old.PreIndexSHA256, old.PostIndexSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
			} else if mode == "same removal conflict" {
				old.BaselineSHA256 = strings.Repeat("c", 64)
			}
			ledger.Append(root, true, old)
			_, fail = ApplyRemoveEntry(root, "code:main.go", ledger.SourceAgent, true, false)
			if mode == "same removal conflict" {
				if fail == nil || !strings.Contains(fail.Msg, "remove_ledger_completion_failed") || len(pendingRemoveReceipts(t, root)) != 1 {
					t.Fatalf("same removal conflict was not retained: %+v", fail)
				}
				return
			}
			if fail != nil || len(pendingRemoveReceipts(t, root)) != 0 {
				t.Fatalf("recovery failed: %+v", fail)
			}
			events, _ := ledger.Recent(root, 0)
			var removals []ledger.Event
			for _, event := range events {
				if event.Op == "remove_entry" {
					removals = append(removals, event)
				}
			}
			want := 1
			if mode == "historical removal" {
				want = 2
			}
			if len(removals) != want || (want == 2 && removals[0].RecoveryTransactionID == removals[1].RecoveryTransactionID) {
				t.Fatalf("terminal events: %+v", removals)
			}
		})
	}
}

func TestVolumeRemoveLedgerIdentitySurvivesCleanupRetry(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	original := removeRecoveryFile
	removeRecoveryFile = func(string) error { return errors.New("injected cleanup failure") }
	t.Cleanup(func() { removeRecoveryFile = original })
	_, fail := ApplyRemoveEntry(root, "code:main.go", ledger.SourceAgent, true, false)
	if fail == nil || !strings.Contains(fail.Msg, "remove_recovery_cleanup_failed") {
		t.Fatalf("expected cleanup failure: %+v", fail)
	}
	removeRecoveryFile = original
	if _, fail = ApplyRemoveEntry(root, "code:main.go", ledger.SourceAgent, true, false); fail != nil {
		t.Fatalf("retry failed: %+v", fail)
	}
	events, _ := ledger.Recent(root, 0)
	count := 0
	for _, event := range events {
		if event.Op == "remove_entry" {
			count++
		}
	}
	if count != 1 || len(pendingRemoveReceipts(t, root)) != 0 {
		t.Fatalf("retry duplicated the terminal event or left a receipt: %d", count)
	}
}

func TestSupersededVolumeRemovalUsesReceiptIdentity(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	ref := "code:gone.go"
	writeStaleVolumeRemoveReceipt(t, root, ref, "gone.go[CD5T]: F:was removed from the tree | R:- | A:- | S:-")
	receipt, err := loadRemoveRecovery(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Version = 2
	receipt.LedgerTransactionID = strings.Repeat("f", 64)
	if err := saveRemoveRecovery(root, *receipt); err != nil {
		t.Fatal(err)
	}
	ledger.Append(root, true, ledger.Event{Op: "remove_entry", Result: ledger.ResultOK,
		RecoveryTransactionID: removeTransactionID(root, ref) + "-superseded-" + shortSHA(receipt.PreIndexSHA256),
		PostIndexSHA256:       strings.Repeat("c", 64)})
	if _, fail := ApplyRemoveEntry(root, ref, ledger.SourceAgent, true, false); fail != nil {
		t.Fatalf("superseded recovery collided with an earlier removal: %+v", fail)
	}
	events, _ := ledger.Recent(root, 0)
	if events[len(events)-1].RecoveryTransactionID != receipt.LedgerTransactionID+"-superseded" {
		t.Fatalf("superseded recovery lost its receipt identity: %+v", events)
	}
}

func TestVolumeRemoveRecreatedObjectGetsNewLedgerIdentity(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	codePath := filepath.Join(root, "aoci.code.txt")
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	code, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			if err := os.WriteFile(codePath, code, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(baselinePath, base, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, fail := ApplyRemoveEntry(root, "code:main.go", ledger.SourceAgent, true, false); fail != nil {
			t.Fatalf("removal %d: %+v", i, fail)
		}
	}
	events, _ := ledger.Recent(root, 0)
	var ids []string
	for _, event := range events {
		if event.Op == "remove_entry" {
			ids = append(ids, event.RecoveryTransactionID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("identical removals must have distinct terminal events: %v", ids)
	}
}
