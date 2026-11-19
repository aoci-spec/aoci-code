package databasebootstrap

import (
	"errors"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// SQLite can feed a Database Volume only after its Evidence has been accepted.
func TestBootstrapRequiresSQLiteEvidence(t *testing.T) {
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
	if !errors.Is(err, ErrNotReady) || preview != nil {
		t.Fatalf("Bootstrap ignored missing SQLite Evidence: %#v %v", preview, err)
	}
}

// With only MongoDB declarations there is no Evidence to bind: Bootstrap is
// not required, rather than asking for an Evidence Baseline no command can
// produce.
func TestBootstrapIsNotRequiredForMongoDBOnlyDeclarations(t *testing.T) {
	root := codeOnlyFixture(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "app", Engine: dbevidence.EngineMongoDB,
		Database: "app", CredentialEnv: "AOCI_DB_APP_URI", Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if preview != nil || err == nil || errors.Is(err, ErrNotReady) || err.Error() != "database_bootstrap_not_required" {
		t.Fatalf("a MongoDB-only repository was asked to bootstrap: %#v %v", preview, err)
	}
}
