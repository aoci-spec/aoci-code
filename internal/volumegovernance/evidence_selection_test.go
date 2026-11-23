package volumegovernance

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Evidence taken under another source selection is not current: the
// database_evidence subfield agrees with the verdict (audit G2-08).
func TestEvidenceUnderAnotherSelectionIsNotCurrent(t *testing.T) {
	root, cfg := alignedFixture(t, true, true)
	if len(cfg.DatabaseSources) == 0 {
		t.Fatal("fixture has no Database source")
	}
	cfg.DatabaseSources[0].Namespaces = append(append([]string{}, cfg.DatabaseSources[0].Namespaces...), "another_schema")
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	if facts.DatabaseEvidence.State == machinecontract.DatabaseCognitionCurrent || facts.DatabaseEvidence.BaselineCurrent {
		t.Fatalf("Evidence under a drifted selection reads current: %+v (result %s)", facts.DatabaseEvidence, facts.Result)
	}
}
