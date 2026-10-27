package databasebootstrap

import (
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A declared SQLite source beside the one holding accepted Evidence stopped
// Bootstrap at database_bootstrap_evidence_not_ready, which no command could
// clear. Bootstrap now builds the Database Volume from the collectable source.
func TestBootstrapBuildsFromCollectableSourcesOnly(t *testing.T) {
	root := codeOnlyFixture(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("a declared-only source stopped Bootstrap: %v", err)
	}
	if len(preview.EvidenceSources) != 1 || preview.EvidenceSources[0].SourceID != "primary" {
		t.Fatalf("Bootstrap bound a source without Evidence: %#v", preview.EvidenceSources)
	}
	if result, err := Apply(root, preview); err != nil || result.Status != StatusApplied {
		t.Fatalf("Bootstrap did not apply beside a declared-only source: %#v %v", result, err)
	}
}
