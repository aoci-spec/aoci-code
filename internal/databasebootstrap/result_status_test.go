package databasebootstrap

import (
	"errors"
	"testing"
	"time"
)

// A rolled-back Database Bootstrap never reads as applied, and one whose
// applied result is written is closed by Resume, not rolled back (audit G1-09).
func TestRolledBackAndAppliedResultsKeepTheirMeaning(t *testing.T) {
	interrupt := func(t *testing.T, point string) (string, *Preview) {
		t.Helper()
		root := codeOnlyFixture(t)
		preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
		if err != nil {
			t.Fatal(err)
		}
		original := transactionFault
		t.Cleanup(func() { transactionFault = original })
		transactionFault = func(p string) error {
			if p == point {
				return errors.New("injected interruption")
			}
			return nil
		}
		if _, err := Apply(root, preview); err == nil {
			t.Fatal("injected interruption did not stop Apply")
		}
		transactionFault = original
		return root, preview
	}

	root, preview := interrupt(t, "after_publish_aoci.txt")
	id := preview.PreviewDigest[:32]
	if result, err := Rollback(root, id); err != nil || result.Status != StatusRolledBack {
		t.Fatalf("rollback: %#v %v", result, err)
	}
	if result, err := Resume(root, id); err != nil || result.Status != StatusRolledBack {
		t.Fatalf("Resume of a rolled-back transaction: %#v %v", result, err)
	}
	if _, err := Apply(root, preview); err == nil || err.Error() != "database_bootstrap_transaction_rolled_back" {
		t.Fatalf("Apply of a rolled-back preview: %v", err)
	}

	root, preview = interrupt(t, "after_result")
	id = preview.PreviewDigest[:32]
	if _, err := Rollback(root, id); err == nil || err.Error() != "database_bootstrap_completed_transaction_rollback_forbidden" {
		t.Fatalf("Rollback after the applied result: %v", err)
	}
	if result, err := Resume(root, id); err != nil || result.Status != StatusApplied {
		t.Fatalf("Resume after the applied result: %#v %v", result, err)
	}
}
