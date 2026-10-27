package onboarding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// Collecting database Evidence during onboarding snapshots only the sources a
// collector exists for. A declared SQLite source beside them is left alone,
// and declarations alone are refused by name rather than by the collector's
// first error.
func TestOnboardingCollectsOnlyCollectableSources(t *testing.T) {
	declared := dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite, Path: "data/app.db",
		Namespaces: []string{"main"}, Enabled: true}
	primary := dbevidence.SourceConfig{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "lower"}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/users",
		Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: "users", Kind: "base_table",
		Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	start := func(sources ...dbevidence.SourceConfig) string {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.DefaultConfig()
		cfg.Locale = "en-US"
		cfg.DatabaseSources = sources
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := Start(root, "en-US", time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return root
	}

	collector := &fixedSnapshotCollector{manifest: manifest, snapshot: snapshot, files: files}
	if _, err := CollectDatabaseEvidence(context.Background(), start(primary, declared), collector); err != nil || collector.calls != 2 {
		t.Fatalf("collection did not snapshot only the collectable source twice: calls=%d err=%v", collector.calls, err)
	}
	alone := &fixedSnapshotCollector{manifest: manifest, snapshot: snapshot, files: files}
	if _, err := CollectDatabaseEvidence(context.Background(), start(declared), alone); err == nil ||
		err.Error() != "onboarding_database_engine_not_implemented: local" || alone.calls != 0 {
		t.Fatalf("declarations alone were not refused by name: calls=%d err=%v", alone.calls, err)
	}
}
