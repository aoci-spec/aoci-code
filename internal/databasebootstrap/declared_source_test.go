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
