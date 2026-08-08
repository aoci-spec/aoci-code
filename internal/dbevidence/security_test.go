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
	}
}

func TestCatalogQueriesStayInsideMetadataBoundary(t *testing.T) {
	queries := catalogQueries()
	if len(queries) != 17 {
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
