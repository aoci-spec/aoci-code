package dbevidence

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

type catalogQuery struct {
	Engine Engine
	Name   string
	SQL    string
}

func newCollectorForTest(getenv func(string) string, open databaseOpener) *Collector {
	return &Collector{getenv: getenv, open: open}
}

func TestOpenGaussOpenerFailsClosedOnUnsafeDriverOptions(t *testing.T) {
	for _, test := range []struct {
		name string
		dsn  string
	}{
		{"implicit_tls", "postgres://reader:secret@127.0.0.1:1/app"},
		{"remote_plaintext", "postgres://reader:secret@db.example.test:5432/app?sslmode=disable"},
		{"downgradable_tls", "postgres://reader:secret@db.example.test:5432/app?sslmode=require"},
		{"logger", "postgres://reader:secret@127.0.0.1:1/app?sslmode=disable&loggerLevel=debug"},
		{"service", "postgres://reader:secret@127.0.0.1:1/app?sslmode=disable&service=aoci"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if database, err := openOpenGaussDatabase(test.dsn, func(string) string { return "" }); err == nil {
				database.Close()
				t.Fatal("unsafe openGauss connection option was accepted")
			}
		})
	}
}

func TestOpenGaussOpenerAcceptsExplicitPlainTransportWithoutLogging(t *testing.T) {
	database, err := openOpenGaussDatabase("opengauss://reader:secret@127.0.0.1:1/app?sslmode=disable", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
}

func TestOpenGaussRuntimeOpenerIgnoresUnsafeAmbientEnvironment(t *testing.T) {
	database, err := openOpenGaussDatabase("postgres://reader:secret@127.0.0.1:1/app?sslmode=disable", func(string) string { return "unsafe" })
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
}

func catalogQueries() []catalogQuery {
	return []catalogQuery{
		{EnginePostgreSQL, "facts", postgresFactsSQL},
		{EnginePostgreSQL, "tables", postgresTablesSQL},
		{EnginePostgreSQL, "columns", postgresColumnsSQL},
		{EnginePostgreSQL, "keys", postgresKeysSQL},
		{EnginePostgreSQL, "foreign_keys", postgresForeignKeysSQL},
		{EnginePostgreSQL, "checks", postgresChecksSQL},
		{EnginePostgreSQL, "indexes", postgresIndexesSQL},
		{EnginePostgreSQL, "partition_parents", postgresPartitionParentsSQL},
		{EnginePostgreSQL, "partition_children", postgresPartitionChildrenSQL},
		{EngineMySQL, "facts", mysqlFactsSQL},
		{EngineMySQL, "tables", mysqlTablesSQL},
		{EngineMySQL, "columns", mysqlColumnsSQL},
		{EngineMySQL, "keys", mysqlKeysSQL},
		{EngineMySQL, "foreign_keys", mysqlForeignKeysSQL},
		{EngineMySQL, "checks", mysqlChecksSQL},
		{EngineMySQL, "indexes", mysqlIndexesSQL},
		{EngineMySQL, "partitions", mysqlPartitionsSQL},
		{EngineOpenGauss, "facts", openGaussFactsSQL},
		{EngineOpenGauss, "tables", openGaussTablesSQL},
		{EngineOpenGauss, "unsupported", openGaussUnsupportedSQL},
		{EngineOpenGauss, "partitions", openGaussPartitionsSQL},
		{EngineOpenGauss, "columns", openGaussColumnsSQL},
		{EngineOpenGauss, "keys", openGaussKeysSQL},
		{EngineOpenGauss, "foreign_keys", openGaussForeignKeysSQL},
		{EngineOpenGauss, "checks", openGaussChecksSQL},
		{EngineOpenGauss, "indexes", openGaussIndexesSQL},
	}
}

func TestCatalogQueriesStayInsideMetadataBoundary(t *testing.T) {
	queries := catalogQueries()
	if len(queries) != 26 {
		t.Fatalf("unexpected query inventory: %d", len(queries))
	}
	for _, query := range queries {
		upper := strings.ToUpper(query.SQL)
		for _, forbidden := range []string{" INSERT ", " UPDATE ", " DELETE ", " TRUNCATE ", " ALTER ", " CREATE ", " DROP ", " COUNT(", "SELECT *"} {
			if strings.Contains(" "+upper+" ", forbidden) {
				t.Fatalf("query %s contains forbidden token %q:\n%s", query.Name, forbidden, query.SQL)
			}
		}
		if query.Name != "facts" && !strings.Contains(upper, "INFORMATION_SCHEMA") && !strings.Contains(upper, "PG_CATALOG") {
			t.Fatalf("query %s leaves catalog boundary:\n%s", query.Name, query.SQL)
		}
	}
}

func TestOpenGaussUnsupportedConstraintStateUsesNativeMatchDefault(t *testing.T) {
	if !strings.Contains(openGaussUnsupportedSQL, "k.confmatchtype <> 'u'") || strings.Contains(openGaussUnsupportedSQL, "k.confmatchtype <> 's'") {
		t.Fatal("openGauss default MATCH UNSPECIFIED catalog code was treated as unsupported")
	}
	for _, fragment := range []string{"n.nspblockchain", "k.condisable", "k.consoft", "k.conopt", "k.conincluding", "k.connoinherit", "NOT k.conislocal", "k.coninhcount <> 0"} {
		if !strings.Contains(openGaussUnsupportedSQL, fragment) {
			t.Fatalf("openGauss unsupported state detector omitted %s", fragment)
		}
	}
}

func TestOpenGaussUnsupportedObjectsAndPartitionVisibilityStayFailClosed(t *testing.T) {
	for _, fragment := range []string{
		"c.relkind = 'v'", "c.relkind = 'm'", "%storage_type=mot%",
	} {
		if !strings.Contains(openGaussUnsupportedSQL, fragment) {
			t.Fatalf("openGauss unsupported object detector omitted %s", fragment)
		}
	}
	for _, fragment := range []string{
		"has_schema_privilege(n.oid, 'USAGE')", "has_table_privilege(c.oid", "has_any_column_privilege(c.oid",
	} {
		if !strings.Contains(openGaussPartitionsSQL, fragment) {
			t.Fatalf("openGauss partition detector omitted visibility predicate %s", fragment)
		}
	}
}

func TestMissingCredentialDoesNotOpenNetworkOrLeakValues(t *testing.T) {
	opened := false
	collector := newCollectorForTest(func(string) string { return "" }, func(string, string) (*sql.DB, error) {
		opened = true
		return nil, nil
	})
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	_, err := collector.Inspect(context.Background(), source)
	if err == nil || opened {
		t.Fatalf("missing credential behavior failed: opened=%t err=%v", opened, err)
	}
	if strings.Contains(err.Error(), "AOCI_DB_PRIMARY_DSN") {
		t.Fatalf("error leaked environment name unexpectedly: %v", err)
	}
}
