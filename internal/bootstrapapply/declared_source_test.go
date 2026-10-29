package bootstrapapply

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// SQLite now has a collector, so policy-bound auto requires its Evidence too.
func TestSQLiteSourceRequiresAcceptedDatabaseEvidence(t *testing.T) {
	root := t.TempDir()
	installBootstrapDatabaseEvidence(t, root)
	if !currentDatabaseEvidenceAccepted(root) {
		t.Fatal("the fixture's accepted Evidence was not accepted")
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if currentDatabaseEvidenceAccepted(root) {
		t.Fatal("SQLite was ignored without accepted Evidence")
	}
}
