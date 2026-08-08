package baseline

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestDatabaseCognitionBindingsRoundTripAndSort(t *testing.T) {
	root := t.TempDir()
	state := NewBaseline(nil)
	for _, objectRef := range []string{"database://primary/public/zeta", "database://primary/public/alpha"} {
		if err := UpdateDatabaseCognitionBinding(state, DatabaseCognitionBinding{
			ObjectRef: objectRef, SourceID: "primary", EvidenceVersion: "database-evidence/v1",
			TableEvidenceSHA256: repeatSHA('a'), EntrySHA256: repeatSHA('b'),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Save(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := Load(root)
	if err != nil || !exists || loaded.DatabaseCognition == nil {
		t.Fatalf("load failed: exists=%t err=%v state=%#v", exists, err, loaded)
	}
	if loaded.DatabaseCognition.Version != machinecontract.DatabaseCognitionBindingVersion ||
		loaded.DatabaseCognition.Entries[0].ObjectRef != "database://primary/public/alpha" {
		t.Fatalf("bindings were not canonical: %#v", loaded.DatabaseCognition)
	}
}

func TestDatabaseCognitionBindingsRejectInvalidSHA(t *testing.T) {
	state := NewBaseline(nil)
	err := UpdateDatabaseCognitionBinding(state, DatabaseCognitionBinding{
		ObjectRef: "database://primary/public/users", SourceID: "primary", EvidenceVersion: "database-evidence/v1",
		TableEvidenceSHA256: "bad", EntrySHA256: repeatSHA('b'),
	})
	if err == nil {
		t.Fatal("invalid binding was accepted")
	}
}

func TestDatabaseCognitionBindingsRejectSourceMismatch(t *testing.T) {
	state := NewBaseline(nil)
	err := UpdateDatabaseCognitionBinding(state, DatabaseCognitionBinding{
		ObjectRef: "database://primary/public/users", SourceID: "other", EvidenceVersion: "database-evidence/v1",
		TableEvidenceSHA256: repeatSHA('a'), EntrySHA256: repeatSHA('b'),
	})
	if err == nil {
		t.Fatal("binding source_id did not match object_ref")
	}
}

func repeatSHA(value byte) string {
	result := make([]byte, 64)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
