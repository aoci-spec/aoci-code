package dbevidence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestMongoDBDeclarationValidation(t *testing.T) {
	valid := SourceConfig{SourceID: "app", Engine: EngineMongoDB, Database: "App",
		CredentialEnv: "AOCI_DB_APP_URI", Enabled: true}
	source := valid
	if err := NormalizeSource(&source); err != nil {
		t.Fatal(err)
	}
	if len(source.Namespaces) != 1 || source.Namespaces[0] != "App" || EngineCollectable(source.Engine) {
		t.Fatalf("unexpected declaration: %+v", source)
	}
	for name, change := range map[string]func(*SourceConfig){
		"database required":          func(s *SourceConfig) { s.Database = "" },
		"database not URI":           func(s *SourceConfig) { s.Database = "mongodb://user:secret@host/app" },
		"credential reference only":  func(s *SourceConfig) { s.CredentialEnv = "mongodb+srv://user:secret@host/app" },
		"no file path":               func(s *SourceConfig) { s.Path = "app.db" },
		"one database":               func(s *SourceConfig) { s.Namespaces = []string{"App", "other"} },
		"namespace matches database": func(s *SourceConfig) { s.Namespaces = []string{"app"} },
		"no URI filter":              func(s *SourceConfig) { s.IncludeTables = []string{"mongodb://host/app"} },
		"no engine alias":            func(s *SourceConfig) { s.Engine = "mongo" },
	} {
		t.Run(name, func(t *testing.T) {
			source := valid
			change(&source)
			if err := NormalizeSource(&source); err == nil {
				t.Fatalf("accepted invalid declaration: %+v", source)
			}
		})
	}
}

func TestMongoDBDeclarationStopsBeforeCredentialLookupOrConnection(t *testing.T) {
	source := SourceConfig{SourceID: "app", Engine: EngineMongoDB, Database: "app",
		CredentialEnv: "AOCI_DB_APP_URI", Enabled: true}
	lookup := func(string) string { t.Fatal("resolved MongoDB credentials"); return "" }
	provider := NewEnvironmentCredentialProvider(lookup)
	collector := NewCollectorAtRoot(t.TempDir())
	collector.getenv, collector.credentialProvider = lookup, provider
	collector.open = func(string, string) (*sql.DB, error) { t.Fatal("opened MongoDB connection"); return nil, nil }
	for _, operation := range []string{"access", "inspect", "snapshot"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			switch operation {
			case "access":
				_, err = InspectAccess(context.Background(), source, provider)
			case "inspect":
				_, err = collector.Inspect(context.Background(), source)
			case "snapshot":
				_, _, _, err = collector.Snapshot(context.Background(), source)
			}
			var sourceErr *SourceError
			if !errors.As(err, &sourceErr) || sourceErr.Code != "engine_not_implemented" || sourceErr.SourceID != "app" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMongoDBDeclarationDoesNotEnableTableEvidence(t *testing.T) {
	table, manifest := fixtureTable("users"), fixtureManifest()
	snapshot, _, err := BuildSnapshot(manifest, []TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	baseline := Baseline{Version: BaselineVersion, Sources: []BaselineSource{{SourceID: "primary",
		Engine: EnginePostgreSQL, Database: "app", EvidenceVersion: EvidenceVersion,
		SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256}}}
	if err := validateBaseline(baseline); err != nil {
		t.Fatal(err)
	}
	table.Engine, manifest.Engine, snapshot.Engine, baseline.Sources[0].Engine = EngineMongoDB, EngineMongoDB, EngineMongoDB, EngineMongoDB
	_, _, _, _, tableErr := CanonicalTable(table)
	for name, err := range map[string]error{
		"table": tableErr, "manifest": validateSourceManifest(manifest),
		"snapshot": validateSnapshot(snapshot), "baseline": validateBaseline(baseline),
	} {
		if err == nil || !strings.Contains(err.Error(), "engine") {
			t.Errorf("%s did not reject MongoDB engine: %v", name, err)
		}
	}
}
