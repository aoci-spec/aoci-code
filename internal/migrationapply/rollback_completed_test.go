package migrationapply

import (
	"errors"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A Migration whose completion result is written is closed by Resume and
// never rolled back (audit G1-12).
func TestMigrationRollbackRefusesAWrittenCompletion(t *testing.T) {
	root := migrationFixture(t, false)
	envelope, approval := preparedMigrationFixture(t, root, []string{"code"})
	previous := migrationFault
	t.Cleanup(func() { migrationFault = previous })
	cut := errors.New("test-only interruption")
	migrationFault = func(point string) error {
		if point == "after_completion_receipt" {
			return cut
		}
		return nil
	}
	if _, err := Apply(root, envelope, approval); !errors.Is(err, cut) {
		t.Fatalf("Apply was not cut after its completion receipt: %v", err)
	}
	migrationFault = previous
	id := transactionIdentity(envelope, approval)
	if _, err := Rollback(root, id); err == nil || err.Error() != "migration_completed_transaction_rollback_forbidden" {
		t.Fatalf("Rollback after the completion receipt: %v", err)
	}
	if result, err := Resume(root, id); err != nil || (result.Status != machinecontract.CognitionMigrationStatusApplied && result.Status != machinecontract.CognitionMigrationStatusAlreadyApplied) {
		t.Fatalf("Resume after the completion receipt: %#v %v", result, err)
	}
}
