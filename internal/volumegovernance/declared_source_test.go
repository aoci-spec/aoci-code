package volumegovernance

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// Beside the source that fed the Database Volume, a declared SQLite source
// left governance at evidence_required, and the snapshot it asked for answers
// engine_not_implemented. It is now one informational finding naming the
// source, and the repository stays aligned.
func TestDeclaredOnlySourceLeavesGovernanceAligned(t *testing.T) {
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
	if facts.Result != ResultAligned || !facts.GovernanceAligned || facts.NextRequiredAction != "none" || !named ||
		!Informational(FindingCodeDatabaseEngineNotImplemented) {
		t.Fatalf("a declared-only source changed governance: result=%s next=%s findings=%#v", facts.Result, facts.NextRequiredAction, facts.Findings)
	}
}
