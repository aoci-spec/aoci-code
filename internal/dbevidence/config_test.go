package dbevidence

import (
	"strings"
	"testing"
)

func TestNormalizeSourcesRejectsUnsafeConfiguration(t *testing.T) {
	valid := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	tests := []struct {
		name   string
		mutate func(*SourceConfig)
	}{
		{"unknown engine", func(source *SourceConfig) { source.Engine = "oracle" }},
		{"source host", func(source *SourceConfig) { source.SourceID = "db.example.com" }},
		{"credential value", func(source *SourceConfig) { source.CredentialEnv = "postgres://user:password@host/db" }},
		{"dsn in database", func(source *SourceConfig) { source.Database = "postgres://user:password@host/db" }},
		{"credential in filter", func(source *SourceConfig) { source.IncludeTables = []string{"token=secret"} }},
		{"bad filter", func(source *SourceConfig) { source.IncludeTables = []string{"[broken"} }},
		{"mysql namespace mismatch", func(source *SourceConfig) { source.Engine = EngineMySQL; source.Namespaces = []string{"other"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := valid
			test.mutate(&source)
			if _, err := NormalizeSources([]SourceConfig{source}); err == nil {
				t.Fatal("unsafe configuration was accepted")
			}
		})
	}
	if _, err := NormalizeSources([]SourceConfig{valid, valid}); err == nil {
		t.Fatal("duplicate source_id was accepted")
	}
}

func TestNoSourceConfigurationIsEmptyAndOffline(t *testing.T) {
	sources, err := NormalizeSources(nil)
	if err != nil || len(sources) != 0 {
		t.Fatalf("empty source configuration changed: sources=%#v err=%v", sources, err)
	}
}

func TestSystemNamespacesAreAlwaysExcluded(t *testing.T) {
	postgres := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public", "pg_catalog"}, CredentialEnv: "AOCI_DB_DSN", Enabled: true}
	if err := NormalizeSource(&postgres); err != nil {
		t.Fatal(err)
	}
	if Included(postgres, "pg_catalog", "pg_class") || !Included(postgres, "public", "users") {
		t.Fatal("postgres system namespace boundary failed")
	}
	mysql := SourceConfig{SourceID: "primary", Engine: EngineMySQL, Database: "mysql", Namespaces: []string{"mysql"}, CredentialEnv: "AOCI_DB_DSN", Enabled: true}
	if err := NormalizeSource(&mysql); err != nil {
		t.Fatal(err)
	}
	if Included(mysql, "mysql", "user") {
		t.Fatal("mysql system namespace boundary failed")
	}
}

func TestTableFiltersTreatSlashAsIdentifierText(t *testing.T) {
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, IncludeTables: []string{"sales*"}, CredentialEnv: "AOCI_DB_DSN", Enabled: true}
	if err := NormalizeSource(&source); err != nil {
		t.Fatal(err)
	}
	if !Included(source, "public", "sales/2026") {
		t.Fatal("table filter treated an identifier as a file path")
	}
}

func TestConfigurationErrorsDoNotEchoCredentialShapedValues(t *testing.T) {
	secret := "postgres://secret-user:secret-password@secret-host/app"
	source := SourceConfig{SourceID: "primary", Engine: Engine(secret), Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_DSN", Enabled: true}
	_, err := NormalizeSources([]SourceConfig{source})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("configuration error leaked input: %v", err)
	}
}

func TestSourceConfigMatchesManifestUsesOnlyNonSecretSelection(t *testing.T) {
	source := SourceConfig{
		SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{"public"}, ExcludeNamespaces: []string{"private"},
		IncludeTables: []string{"orders*"}, ExcludeTables: []string{"orders_tmp"},
		CredentialEnv: "PRIMARY_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	}
	manifest := SourceManifest{
		Version: SourceManifestVersion, SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{"public"}, ExcludeNamespaces: []string{"private"},
		IncludeTables: []string{"orders*"}, ExcludeTables: []string{"orders_tmp"},
	}
	if !SourceConfigMatchesManifest(source, manifest) {
		t.Fatal("identical source selection did not match")
	}
	source.CredentialEnv = "ROTATED_DSN"
	source.ConnectTimeoutSeconds = 99
	if !SourceConfigMatchesManifest(source, manifest) {
		t.Fatal("credential name or timeout incorrectly changed Evidence selection identity")
	}
	source.IncludeTables = []string{"users*"}
	if SourceConfigMatchesManifest(source, manifest) {
		t.Fatal("changed table selection was accepted")
	}
}
