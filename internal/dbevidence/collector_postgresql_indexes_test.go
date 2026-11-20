package dbevidence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"
)

func TestPostgreSQLCollectorPreservesIndexDirections(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	activeMockCatalogState = &mockCatalogState{
		engine: EnginePostgreSQL,
		postgresIndexRows: [][]driver.Value{
			{"public", "users", "mixed", false, false, "btree", int64(2), "email", nil, false, "", true},
			{"public", "users", "mixed", false, false, "btree", int64(1), "id", nil, false, "", false},
			{"public", "users", "mixed", false, false, "btree", int64(3), "name", nil, true, "", false},
			{"public", "users", "expression", false, false, "btree", int64(1), nil, "lower(email)", false, "", true},
			{"public", "users", "unordered", false, false, "hash", int64(1), "id", nil, false, "", false},
		},
	}
	database, err := sql.Open("aoci_mock_catalog", "test-only")
	if err != nil {
		t.Fatal(err)
	}
	collector := newCollectorForTest(func(string) string { return "test-only" }, func(string, string) (*sql.DB, error) {
		return database, nil
	})
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	_, snapshot, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	var table TableEvidence
	if err := decodeStrict(files[snapshot.Tables[0].ObjectRef], &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Indexes) != 3 {
		t.Fatalf("indexes lost: %+v", table.Indexes)
	}
	want := []Index{
		{Name: "expression", Method: "btree", Elements: []IndexElement{{Position: 1, Expression: stringPointer("lower(email)"), Descending: true}}},
		{Name: "mixed", Method: "btree", Elements: []IndexElement{
			{Position: 1, Column: stringPointer("id")},
			{Position: 2, Column: stringPointer("email"), Descending: true},
			{Position: 3, Column: stringPointer("name"), Included: true},
		}},
		{Name: "unordered", Method: "hash", Elements: []IndexElement{{Position: 1, Column: stringPointer("id")}}},
	}
	if !reflect.DeepEqual(table.Indexes, want) {
		t.Fatalf("index directions or positions lost: got %+v, want %+v", table.Indexes, want)
	}
}
