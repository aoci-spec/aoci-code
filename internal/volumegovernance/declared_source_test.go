package volumegovernance

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A collectable SQLite source must participate in governance, rather than
// remain an informational declaration that silently contributes no Evidence.
func TestSQLiteSourceRequiresEvidenceForGovernance(t *testing.T) {
	root, cfg := alignedFixture(t, true, true)
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true})
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
	named := false
	for _, finding := range facts.Findings {
		named = named || finding.Code == FindingCodeDatabaseEngineNotImplemented && finding.Target == "local"
	}
	if facts.Result != ResultEvidenceRequired || facts.GovernanceAligned || facts.NextRequiredAction != "snapshot_or_repair_evidence" || named {
		t.Fatalf("SQLite Evidence was not required: result=%s next=%s findings=%#v", facts.Result, facts.NextRequiredAction, facts.Findings)
	}
}
