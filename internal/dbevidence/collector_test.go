package dbevidence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

var registerMockCatalogDriver sync.Once

type mockCatalogState struct {
	engine   Engine
	queries  []string
	readOnly bool
	pingErr  error
	queryErr error
}

type mockCatalogDriver struct{}
type mockCatalogConn struct{ state *mockCatalogState }
type mockCatalogTx struct{}
type mockCatalogRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

type mockSQLStateError string

func (err mockSQLStateError) Error() string    { return "redacted database failure" }
func (err mockSQLStateError) SQLState() string { return string(err) }

var activeMockCatalogState *mockCatalogState

func (driverValue mockCatalogDriver) Open(string) (driver.Conn, error) {
	return &mockCatalogConn{state: activeMockCatalogState}, nil
}

func (conn *mockCatalogConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not used")
}
func (conn *mockCatalogConn) Close() error { return nil }
func (conn *mockCatalogConn) Begin() (driver.Tx, error) {
	return nil, errors.New("legacy begin is not used")
}
func (conn *mockCatalogConn) Ping(context.Context) error { return conn.state.pingErr }
func (conn *mockCatalogConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	conn.state.readOnly = opts.ReadOnly
	return mockCatalogTx{}, nil
}
func (conn *mockCatalogConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	conn.state.queries = append(conn.state.queries, query)
	if conn.state.queryErr != nil {
		return nil, conn.state.queryErr
	}
	columns, values := mockCatalogResult(conn.state.engine, query)
	return &mockCatalogRows{columns: columns, values: values}, nil
}
func (mockCatalogTx) Commit() error             { return nil }
func (mockCatalogTx) Rollback() error           { return nil }
func (rows *mockCatalogRows) Columns() []string { return rows.columns }
func (rows *mockCatalogRows) Close() error      { return nil }
func (rows *mockCatalogRows) Next(target []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(target, rows.values[rows.index])
	rows.index++
	return nil
}

func TestCollectorUsesReadOnlyTransactionAndOnlyCatalogQueries(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	state := &mockCatalogState{engine: EnginePostgreSQL}
	activeMockCatalogState = state
	database, err := sql.Open("aoci_mock_catalog", "credential_sentinel")
	if err != nil {
		t.Fatal(err)
	}
	collector := newCollectorForTest(func(string) string { return "postgres://user:credential_sentinel@host/app" }, func(string, string) (*sql.DB, error) {
		return database, nil
	})
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	manifest, snapshot, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !state.readOnly || manifest.BusinessDataRead || snapshot.BusinessDataRead || len(snapshot.Tables) != 1 || len(files) != 1 {
		t.Fatalf("collector boundary failed: readOnly=%t manifest=%+v snapshot=%+v", state.readOnly, manifest, snapshot)
	}
	var table TableEvidence
	if err := decodeStrict(files[snapshot.Tables[0].ObjectRef], &table); err != nil {
		t.Fatal(err)
	}
	if table.Columns[0].Name != "id" || table.Columns[1].Name != "email" || table.Columns[2].Name != "name" || strings.Join(table.PrimaryKey.Columns, ",") != "id,email" {
		t.Fatalf("catalog return order leaked into canonical evidence: %+v", table)
	}
	want := map[string]struct{}{}
	for _, query := range catalogQueries() {
		if query.Engine == EnginePostgreSQL {
			want[query.SQL] = struct{}{}
		}
	}
	for _, query := range state.queries {
		if _, exists := want[query]; !exists {
			t.Fatalf("collector executed an unregistered query:\n%s", query)
		}
		delete(want, query)
	}
	if len(want) != 0 {
		t.Fatalf("collector skipped registered queries: %d", len(want))
	}
	encoded, _ := CanonicalJSON(struct {
		Manifest SourceManifest `json:"manifest"`
		Snapshot Snapshot       `json:"snapshot"`
	}{manifest, snapshot})
	for _, sentinel := range []string{"credential_sentinel", "business_row_sentinel", "user@host"} {
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("collector output leaked sentinel %q", sentinel)
		}
	}
}

func TestConnectionFailureIsRedacted(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	state := &mockCatalogState{engine: EnginePostgreSQL, pingErr: errors.New("dial user:credential_sentinel@secret-host failed")}
	activeMockCatalogState = state
	database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
	collector := newCollectorForTest(func(string) string { return "credential_sentinel" }, func(string, string) (*sql.DB, error) { return database, nil })
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	_, err := collector.Inspect(context.Background(), source)
	if err == nil || strings.Contains(err.Error(), "credential_sentinel") || strings.Contains(err.Error(), "secret-host") || strings.Contains(err.Error(), "user") {
		t.Fatalf("connection error was not redacted: %v", err)
	}
}

func TestSQLStateDiagnosticsPreserveBaseFailureClassification(t *testing.T) {
	err := classifySourceError(context.Background(), "primary", "ping", mockSQLStateError("28P01"))
	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || sourceErr.Code != "permission_denied" || sourceErr.Op != "ping_sqlstate_28p01" {
		t.Fatalf("unexpected authentication classification: %v", err)
	}
	err = classifySourceError(context.Background(), "primary", "ping", mockSQLStateError("3D000"))
	if !errors.As(err, &sourceErr) || sourceErr.Code != "connection_failed" || sourceErr.Op != "ping_sqlstate_3d000" {
		t.Fatalf("unexpected connection classification: %v", err)
	}
}

func TestCollectorClassifiesCancellationAndPermissionWithoutRawDetails(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app", CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	for _, test := range []struct {
		name     string
		context  func() context.Context
		state    *mockCatalogState
		wantCode string
	}{
		{"cancelled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, &mockCatalogState{engine: EnginePostgreSQL}, "cancelled"},
		{"timeout", func() context.Context {
			ctx, cancel := context.WithTimeout(context.Background(), 0)
			defer cancel()
			return ctx
		}, &mockCatalogState{engine: EnginePostgreSQL}, "timeout"},
		{"permission", context.Background, &mockCatalogState{engine: EnginePostgreSQL, queryErr: errors.New("permission denied for secret-host user sentinel")}, "permission_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			activeMockCatalogState = test.state
			database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
			collector := newCollectorForTest(func(string) string { return "credential_sentinel" }, func(string, string) (*sql.DB, error) { return database, nil })
			_, err := collector.Inspect(test.context(), source)
			var sourceErr *SourceError
			if !errors.As(err, &sourceErr) || sourceErr.Code != test.wantCode {
				t.Fatalf("want %s, got %v", test.wantCode, err)
			}
			if strings.Contains(err.Error(), "secret-host") || strings.Contains(err.Error(), "sentinel") {
				t.Fatalf("error leaked raw detail: %v", err)
			}
		})
	}
}

func TestMySQLCollectorPreservesCaseModeAndIndexFactsOffline(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	state := &mockCatalogState{engine: EngineMySQL}
	activeMockCatalogState = state
	database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
	collector := newCollectorForTest(func(string) string { return "reader:credential_sentinel@tcp(secret-host)/aoci_test" }, func(string, string) (*sql.DB, error) { return database, nil })
	source := SourceConfig{SourceID: "primary", Engine: EngineMySQL, Database: "aoci_test", Namespaces: []string{"aoci_test"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	manifest, snapshot, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CaseSemantics.LowerCaseTableNames == nil || *manifest.CaseSemantics.LowerCaseTableNames != 0 || len(snapshot.Tables) != 1 {
		t.Fatalf("mysql case semantics failed: manifest=%+v snapshot=%+v", manifest, snapshot)
	}
	if snapshot.Tables[0].ObjectRef != "database://primary/aoci_test/Order%20Items" {
		t.Fatalf("mysql identity lost returned case: %s", snapshot.Tables[0].ObjectRef)
	}
	data := string(files[snapshot.Tables[0].ObjectRef])
	if !strings.Contains(data, `"prefix_length": 12`) || !strings.Contains(data, `"visible": false`) {
		t.Fatalf("mysql index evidence is incomplete: %s", data)
	}
}

func mockCatalogResult(engine Engine, query string) ([]string, [][]driver.Value) {
	if engine == EngineMySQL {
		return mockMySQLResult(query)
	}
	switch query {
	case postgresFactsSQL:
		return []string{"server_version"}, [][]driver.Value{{"18.1"}}
	case postgresTablesSQL:
		return []string{"table_schema", "table_name"}, [][]driver.Value{{"public", "users"}, {"pg_catalog", "pg_class"}}
	case postgresColumnsSQL:
		return []string{"table_schema", "table_name", "ordinal_position", "column_name", "native_type", "data_type", "nullable", "default", "serial", "is_identity", "identity_generation", "is_generated", "generation_expression"}, [][]driver.Value{
			{"public", "users", int64(3), "name", "text", "text", true, nil, false, "NO", nil, "NEVER", nil},
			{"public", "users", int64(1), "id", "bigint", "bigint", false, nil, false, "NO", nil, "NEVER", nil},
			{"public", "users", int64(2), "email", "text", "text", false, nil, false, "NO", nil, "NEVER", nil},
		}
	case postgresKeysSQL:
		return []string{"schema", "table", "name", "kind", "ordinal", "column"}, [][]driver.Value{
			{"public", "users", "users_pkey", "p", int64(2), "email"},
			{"public", "users", "users_pkey", "p", int64(1), "id"},
		}
	case postgresForeignKeysSQL:
		return []string{"schema", "table", "name", "ordinal", "column", "ref_schema", "ref_table", "ref_column", "update", "delete"}, nil
	case postgresChecksSQL:
		return []string{"schema", "table", "name", "expression"}, nil
	case postgresIndexesSQL:
		return []string{"schema", "table", "name", "unique", "primary", "method", "position", "column", "expression", "included", "predicate"}, nil
	case postgresPartitionParentsSQL:
		return []string{"schema", "table", "method", "expression"}, nil
	case postgresPartitionChildrenSQL:
		return []string{"schema", "table", "parent_schema", "parent_table", "bound"}, nil
	default:
		return []string{"unexpected"}, nil
	}
}

func mockMySQLResult(query string) ([]string, [][]driver.Value) {
	switch query {
	case mysqlFactsSQL:
		return []string{"version", "lower_case_table_names"}, [][]driver.Value{{"8.4.0", int64(0)}}
	case mysqlTablesSQL:
		return []string{"table_schema", "table_name"}, [][]driver.Value{{"mysql", "user"}, {"aoci_test", "Order Items"}}
	case mysqlColumnsSQL:
		return []string{"schema", "table", "ordinal", "column", "column_type", "data_type", "nullable", "default", "extra", "generation"}, [][]driver.Value{{"aoci_test", "Order Items", int64(1), "select", "varchar(255)", "varchar", false, nil, "", ""}}
	case mysqlKeysSQL:
		return []string{"schema", "table", "name", "type", "ordinal", "column"}, nil
	case mysqlForeignKeysSQL:
		return []string{"schema", "table", "name", "ordinal", "column", "ref_schema", "ref_table", "ref_column", "update", "delete"}, nil
	case mysqlChecksSQL:
		return []string{"schema", "table", "name", "check"}, nil
	case mysqlIndexesSQL:
		return []string{"schema", "table", "name", "non_unique", "position", "column", "expression", "prefix", "collation", "method", "visible"}, [][]driver.Value{{"aoci_test", "Order Items", "name_prefix", int64(1), int64(1), "select", nil, int64(12), "A", "BTREE", "NO"}}
	case mysqlPartitionsSQL:
		return []string{"schema", "table", "name", "ordinal", "method", "expression", "description"}, nil
	default:
		return []string{"unexpected"}, nil
	}
}
