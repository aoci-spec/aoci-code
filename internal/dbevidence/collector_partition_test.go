package dbevidence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
)

func TestPostgreSQLPartitionSelection(t *testing.T) {
	const bound = "FOR VALUES FROM (1) TO (10)"
	for _, test := range []struct {
		name    string
		row     []driver.Value
		parent  bool
		want    *Partition
		errorOp string
	}{
		{
			name: "unselected_namespace_missing_bound",
			row:  []driver.Value{"archive", "users_recent", "archive", "users", nil},
		},
		{
			name: "excluded_tables_invalid_identity",
			row:  []driver.Value{"public", "excluded_child ", "public", "excluded_parent", bound},
		},
		{
			name: "child_only_keeps_parent_reference",
			row:  []driver.Value{"public", "users", "archive", "users_parent", bound},
			want: &Partition{ParentObject: "database://primary/archive/users_parent", Bound: bound},
		},
		{
			name:   "parent_only_keeps_child_reference",
			row:    []driver.Value{"public", "excluded_child", "public", "users", bound},
			parent: true,
			want:   &Partition{Partitioned: true, Method: "range", Expression: "RANGE (id)", ChildObjects: []string{"database://primary/public/excluded_child"}},
		},
		{
			name:    "child_only_validates_parent_identity",
			row:     []driver.Value{"public", "users", "archive", "users_parent ", bound},
			errorOp: "catalog_query_partition_children_identity",
		},
		{
			name:    "parent_only_validates_bound",
			row:     []driver.Value{"public", "excluded_child", "public", "users", nil},
			parent:  true,
			errorOp: "catalog_query_partition_children_validate",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := map[string][][]driver.Value{
				postgresTablesSQL: {
					{"public", "users"}, {test.row[0], test.row[1]}, {test.row[2], test.row[3]},
				},
				postgresPartitionChildrenSQL: {test.row},
			}
			if test.parent {
				rows[postgresPartitionParentsSQL] = [][]driver.Value{{"public", "users", "range", "RANGE (id)"}}
			}
			collector := partitionTestCollector(t, &mockCatalogState{engine: EnginePostgreSQL, catalogRows: rows})
			source := SourceConfig{
				SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
				ExcludeTables: []string{"excluded_*"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true,
			}
			_, snapshot, files, err := collector.Snapshot(context.Background(), source)
			if test.errorOp != "" {
				var sourceErr *SourceError
				if !errors.As(err, &sourceErr) || sourceErr.Op != test.errorOp || len(files) != 0 || len(snapshot.Tables) != 0 {
					t.Fatalf("expected %s without partial evidence, got %v", test.errorOp, err)
				}
				return
			}
			table := onlyPartitionTestTable(t, snapshot, files, err)
			if table.Name != "users" || !reflect.DeepEqual(table.Partition, test.want) {
				t.Fatalf("got table %s partition %#v, want users partition %#v", table.Name, table.Partition, test.want)
			}
		})
	}
}

func TestMySQLPartitionSelection(t *testing.T) {
	const tableName = "Order Items"
	for _, version := range []string{"5.7.44", "8.4.0"} {
		t.Run(version, func(t *testing.T) {
			source := SourceConfig{
				SourceID: "primary", Engine: EngineMySQL, Database: "aoci_test", Namespaces: []string{"aoci_test"},
				ExcludeTables: []string{"Archived Orders"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true,
			}
			t.Run("selected_subpartitions", func(t *testing.T) {
				collector := partitionTestCollector(t, &mockCatalogState{
					engine: EngineMySQL, mysqlVersion: version, catalogRows: map[string][][]driver.Value{
						mysqlPartitionsSQL: {
							{"aoci_test", tableName, "p0", int64(1), "RANGE", "id", "100", "p0a"},
							{"aoci_test", tableName, "p0", int64(1), "RANGE", "id", "100", "p0b"},
						},
					},
				})
				_, snapshot, files, err := collector.Snapshot(context.Background(), source)
				var sourceErr *SourceError
				if !errors.As(err, &sourceErr) || sourceErr.Code != "unsupported_catalog_feature" ||
					sourceErr.Op != "catalog_query_partitions_mysql_subpartition" || len(files) != 0 || len(snapshot.Tables) != 0 {
					t.Fatalf("expected subpartition error without partial evidence, got %v", err)
				}
				if sourceErr.Object != tableName {
					t.Fatalf("subpartition error names %q, want %q", sourceErr.Object, tableName)
				}
			})
			t.Run("excludes_unselected_subpartitions", func(t *testing.T) {
				collector := partitionTestCollector(t, &mockCatalogState{
					engine: EngineMySQL, mysqlVersion: version, catalogRows: map[string][][]driver.Value{
						mysqlTablesSQL: {{"aoci_test", "Archived Orders"}, {"aoci_test", tableName}},
						mysqlPartitionsSQL: {
							{"aoci_test", "Archived Orders", "p0", int64(1), "RANGE", "id", "100", "p0a"},
							{"aoci_test", tableName, "p1", int64(2), "RANGE", "id", "MAXVALUE", ""},
							{"aoci_test", tableName, "p0", int64(1), "RANGE", "id", "100", nil},
						},
					},
				})
				_, snapshot, files, err := collector.Snapshot(context.Background(), source)
				table := onlyPartitionTestTable(t, snapshot, files, err)
				want := &Partition{Partitioned: true, Method: "range", Expression: "id", Definitions: []PartitionDefinition{
					{Name: "p0", Ordinal: 1, Description: "100"}, {Name: "p1", Ordinal: 2, Description: "MAXVALUE"},
				}}
				if table.Name != tableName || !reflect.DeepEqual(table.Partition, want) {
					t.Fatalf("got table %s partition %#v, want %s partition %#v", table.Name, table.Partition, tableName, want)
				}
			})
		})
	}
}

func partitionTestCollector(t *testing.T, state *mockCatalogState) *Collector {
	t.Helper()
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	activeMockCatalogState = state
	database, err := sql.Open("aoci_mock_catalog", "test-only")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return newCollectorForTest(func(string) string { return "test-only" }, func(string, string) (*sql.DB, error) {
		return database, nil
	})
}

func onlyPartitionTestTable(t *testing.T, snapshot Snapshot, files map[string][]byte, err error) TableEvidence {
	t.Helper()
	if err != nil || len(snapshot.Tables) != 1 || len(files) != 1 {
		t.Fatalf("expected exactly the selected table: refs=%v files=%d err=%v", snapshot.Tables, len(files), err)
	}
	var table TableEvidence
	if err := decodeStrict(files[snapshot.Tables[0].ObjectRef], &table); err != nil {
		t.Fatal(err)
	}
	return table
}
