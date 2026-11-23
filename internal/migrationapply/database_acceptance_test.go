package migrationapply

import (
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A Migration that creates a Database Volume binds its Entries to accepted
// Evidence only, and says so before any write; the post-write verify reads
// only accepted Evidence and used to fail after the writes instead.
func TestMigrationRefusesUnacceptedDatabaseEvidenceBeforeWriting(t *testing.T) {
	root := migrationFixture(t, true)
	if err := os.Remove(dbevidence.BaselinePath(root)); err != nil {
		t.Fatal(err)
	}
	request := migrationApplyRequest(t, root, []string{"code", "database"})
	legacy, err := os.ReadFile(root + "/aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(root, request)
	if err == nil || !strings.Contains(err.Error(), "cognition_database_evidence_not_accepted: primary") ||
		!strings.Contains(err.Error(), "aoci database baseline accept") {
		t.Fatalf("unaccepted Evidence: %v", err)
	}
	after, err := os.ReadFile(root + "/aoci.txt")
	if err != nil || string(after) != string(legacy) {
		t.Fatalf("the refusal changed the Legacy index: %v", err)
	}
}
