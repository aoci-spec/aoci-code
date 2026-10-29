package onboarding

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestOnboardingCollectsSQLiteEvidenceAtRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "explicit.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY);`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Locale = "en-US"
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "local", Engine: dbevidence.EngineSQLite, Path: "explicit.db", Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(root, "en-US", time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := CollectDatabaseEvidence(context.Background(), root, nil); err != nil {
		t.Fatal(err)
	}
	manifest, snapshot, exists, err := dbevidence.LoadSnapshot(root, "local")
	if err != nil || !exists || manifest.Database != "explicit.db" || len(snapshot.Tables) != 1 {
		t.Fatalf("saved SQLite Evidence: %+v %v", snapshot, err)
	}
	baseline, exists, err := dbevidence.LoadBaseline(root)
	if err != nil || !exists || len(baseline.Sources) != 1 || baseline.Sources[0].SourceSnapshotSHA256 != snapshot.SourceSnapshotSHA256 {
		t.Fatalf("SQLite Evidence was not accepted: %+v %v", baseline, err)
	}
}
