package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestDatabaseSourcesAreTeamOnlyAndContainNoCredentialValue(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true,
	}}
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	local := DefaultConfig()
	local.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "override", Engine: dbevidence.EngineMySQL, Database: "other",
		Namespaces: []string{"other"}, CredentialEnv: "AOCI_DB_OTHER_DSN", Enabled: true,
	}}
	if err := SaveLocal(root, local); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.DatabaseSources) != 1 || loaded.DatabaseSources[0].SourceID != "primary" {
		t.Fatalf("local configuration overrode database sources: %+v", loaded.DatabaseSources)
	}
	localBytes, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(localBytes), "database_sources") || stringContains(string(localBytes), "AOCI_DB_OTHER_DSN") {
		t.Fatalf("local file retained team database configuration: %s", localBytes)
	}
}

func TestDatabaseSourceRejectsConnectionStringInTeamConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "postgres://user:password@host/app", Enabled: true,
	}}
	if err := Save(root, cfg); err == nil {
		t.Fatal("connection string was accepted in team configuration")
	}
}

func TestOpenGaussDatabaseSourceRoundTripsInTeamConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "warehouse", Engine: dbevidence.EngineOpenGauss, Database: "analytics",
		CredentialEnv: "AOCI_DB_WAREHOUSE_DSN", Enabled: true,
	}}
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.DatabaseSources) != 1 {
		t.Fatalf("openGauss source count changed after round trip: %+v", loaded.DatabaseSources)
	}
	got := loaded.DatabaseSources[0]
	if got.SourceID != "warehouse" || got.Engine != dbevidence.EngineOpenGauss || got.Database != "analytics" ||
		got.CredentialEnv != "AOCI_DB_WAREHOUSE_DSN" || !got.Enabled ||
		len(got.Namespaces) != 1 || got.Namespaces[0] != "public" {
		t.Fatalf("openGauss source changed after round trip: %+v", got)
	}

	data, err := os.ReadFile(FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !stringContains(string(data), `"engine": "opengauss"`) {
		t.Fatalf("team config did not preserve canonical openGauss engine: %s", data)
	}
}

func TestDatabaseSourceConfigRejectsUnknownAndOpenGaussAliases(t *testing.T) {
	for _, engine := range []string{"openGauss", "gaussdb", "open_gauss", "og", "postgres", "unknown"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig()
			cfg.DatabaseSources = []dbevidence.SourceConfig{{
				SourceID: "warehouse", Engine: dbevidence.Engine(engine), Database: "analytics",
				CredentialEnv: "AOCI_DB_WAREHOUSE_DSN", Enabled: true,
			}}
			if err := Save(root, cfg); err == nil {
				t.Fatalf("non-canonical database engine %q was accepted", engine)
			}
		})
	}
}

func stringContains(value, target string) bool {
	for index := 0; index+len(target) <= len(value); index++ {
		if value[index:index+len(target)] == target {
			return true
		}
	}
	return false
}
