package dbcognition

import (
	"os"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// With a Database Volume present, assessment and submission read only the
// snapshot database baseline accept recorded (audit G6-02).
func TestPresentVolumeReadsOnlyAcceptedSnapshot(t *testing.T) {
	root, sources := databaseFixture(t, []string{"users", "orders"}, []string{"users"})
	set := loadFixtureSet(t, root)
	assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
	plan, err := BuildPlan(root, assessment, set, 20, 1<<20)
	if err != nil || len(plan.Candidates) == 0 {
		t.Fatalf("fixture plan: %v %#v", err, plan)
	}
	submission := make([]Submission, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		submission = append(submission, Submission{ObjectRef: candidate.ObjectRef, CandidateID: candidate.CandidateID})
	}
	if _, err := ValidateSubmission(root, sources, plan.BatchID, submission); err != nil {
		t.Fatalf("accepted snapshot refused: %v", err)
	}

	// A newer snapshot captured but not accepted.
	manifest, snapshot, _, err := dbevidence.LoadSnapshot(root, "primary")
	if err != nil {
		t.Fatal(err)
	}
	next, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{fixtureTable("database://primary/public/users"), fixtureTable("database://primary/public/orders"), fixtureTable("database://primary/public/items")})
	if err != nil || next.SourceSnapshotSHA256 == snapshot.SourceSnapshotSHA256 {
		t.Fatalf("fixture snapshot: %v", err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, next, files); err != nil {
		t.Fatal(err)
	}
	unaccepted := Assess(root, sources, set, baseline.NewBaseline(nil))
	if len(unaccepted.Sources) != 1 || unaccepted.Sources[0].ErrorCode != "database_snapshot_not_accepted" || unaccepted.CognitionCurrent {
		t.Fatalf("unaccepted snapshot was assessed: %#v", unaccepted.Sources)
	}
	if _, err := ValidateSubmission(root, sources, plan.BatchID, submission); err == nil || err.Error() != "database_candidate_evidence_not_accepted" {
		t.Fatalf("unaccepted snapshot submission: %v", err)
	}

	if err := os.Remove(dbevidence.BaselinePath(root)); err != nil {
		t.Fatal(err)
	}
	missing := Assess(root, sources, set, baseline.NewBaseline(nil))
	if missing.Sources[0].State != machinecontract.DatabaseCognitionEvidenceUnavailable || missing.Sources[0].ErrorCode != "database_evidence_baseline_missing" {
		t.Fatalf("missing accept record: %#v", missing.Sources)
	}
}
