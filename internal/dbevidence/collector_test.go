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
	engine                 Engine
	queries                []string
	readOnly               bool
	isolation              driver.IsolationLevel
	pingErr                error
	queryErr               error
	openDriverName         string
	mysqlVersion           string
	openGaussVersion       string
	openGaussDeployment    string
	openGaussCompatibility string
	openGaussRows          map[string][][]driver.Value
	postgresIndexRows      [][]driver.Value
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
	conn.state.isolation = opts.Isolation
	return mockCatalogTx{}, nil
}
func (conn *mockCatalogConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	conn.state.queries = append(conn.state.queries, query)
	if conn.state.queryErr != nil {
		return nil, conn.state.queryErr
	}
	columns, values := mockCatalogResult(conn.state.engine, query)
	if query == postgresIndexesSQL && conn.state.postgresIndexRows != nil {
		values = conn.state.postgresIndexRows
	}
	if override, ok := conn.state.openGaussRows[query]; ok {
		values = override
	}
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
	if !state.readOnly || state.isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || manifest.BusinessDataRead || snapshot.BusinessDataRead || len(snapshot.Tables) != 1 || len(files) != 1 {
		t.Fatalf("collector boundary failed: readOnly=%t isolation=%d manifest=%+v snapshot=%+v", state.readOnly, state.isolation, manifest, snapshot)
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

func TestOpenGaussCollectorUsesDedicatedDriverAndCatalog(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	state := &mockCatalogState{engine: EngineOpenGauss}
	activeMockCatalogState = state
	database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
	collector := newCollectorForTest(func(string) string { return "host=secret-host user=reader password=credential_sentinel dbname=app" }, func(driverName, _ string) (*sql.DB, error) {
		state.openDriverName = driverName
		return database, nil
	})
	source := SourceConfig{SourceID: "ogtemp", Engine: EngineOpenGauss, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_OGTEMP_DSN", Enabled: true}
	manifest, snapshot, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if state.openDriverName != "opengauss" || !state.readOnly || manifest.Engine != EngineOpenGauss || len(snapshot.Tables) != 1 {
		t.Fatalf("openGauss routing failed: driver=%s readOnly=%t manifest=%+v snapshot=%+v", state.openDriverName, state.readOnly, manifest, snapshot)
	}
	var table TableEvidence
	if err := decodeStrict(files[snapshot.Tables[0].ObjectRef], &table); err != nil {
		t.Fatal(err)
	}
	if table.Engine != EngineOpenGauss || table.PrimaryKey == nil || strings.Join(table.PrimaryKey.Columns, ",") != "id,email" || len(table.Indexes) != 1 {
		t.Fatalf("openGauss evidence lost catalog semantics: %+v", table)
	}
	if table.Indexes[0].Visible == nil || !*table.Indexes[0].Visible || len(table.Indexes[0].Elements) != 2 || table.Indexes[0].Elements[0].Descending || !table.Indexes[0].Elements[1].Descending {
		t.Fatalf("openGauss index direction or visibility was lost: %+v", table.Indexes[0])
	}
	want := map[string]struct{}{}
	for _, query := range catalogQueries() {
		if query.Engine == EngineOpenGauss {
			want[query.SQL] = struct{}{}
		}
	}
	for _, query := range state.queries {
		if _, exists := want[query]; !exists {
			t.Fatalf("openGauss collector executed an unregistered query:\n%s", query)
		}
		delete(want, query)
	}
	if len(want) != 0 {
		t.Fatalf("openGauss collector skipped registered queries: %d", len(want))
	}
}

func TestOpenGaussCollectorRejectsUnsupportedCatalogFeatures(t *testing.T) {
	for _, feature := range []string{"partitioned_table", "foreign_table", "blockchain_schema", "constraint_state", "view", "materialized_view", "mot_table", "global_partition_index"} {
		t.Run(feature, func(t *testing.T) {
			assertOpenGaussCatalogObject(t, openGaussUnsupportedSQL, []driver.Value{"public", "accounts", feature}, "unsupported", feature, "public.accounts")
		})
	}
	for partType, feature := range map[string]string{"p": "pg_partition", "s": "subpartition"} {
		t.Run(feature, func(t *testing.T) {
			assertOpenGaussCatalogObject(t, openGaussPartitionsSQL, []driver.Value{"public", "accounts", partType, "r"}, "unsupported", feature, "public.accounts")
		})
	}
}

func TestOpenGaussCollectorNamesUnsupportedIndex(t *testing.T) {
	// The modified column in the fixture matches openGaussIndexesSQL.
	const (
		vector     = 7
		definition = 8
		totalCount = 11
		options    = 12
		valid      = 15
	)
	for _, test := range []struct {
		feature string
		column  int
		value   driver.Value
	}{
		{"included_index_columns", totalCount, int64(3)},
		{"index_state", valid, false},
		{"partition_index", definition, "CREATE UNIQUE INDEX accounts_pk ON accounts (id, email) LOCAL"},
		{"expression_index", vector, "0 2"},
		{"index_null_order", options, "0 1"},
		{"repeated_column", vector, "1 1"},
	} {
		t.Run(test.feature, func(t *testing.T) {
			_, rows := mockOpenGaussResult(openGaussIndexesSQL)
			row := rows[0]
			row[test.column] = test.value
			assertOpenGaussCatalogObject(t, openGaussIndexesSQL, row, "indexes", test.feature, "public.accounts (accounts_pk)")
		})
	}
}

func TestOpenGaussCollectorNamesRepeatedKeyColumn(t *testing.T) {
	t.Run("key", func(t *testing.T) {
		row := []driver.Value{"public", "accounts", "accounts_pk", "p", "id", "1 1"}
		assertOpenGaussCatalogObject(t, openGaussKeysSQL, row, "keys", "repeated_column", "public.accounts (accounts_pk)")
	})
	for _, test := range []struct{ name, local, referenced string }{
		{"local", "1 1", "2"},
		{"referenced", "1", "2 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := []driver.Value{"public", "accounts", "accounts_org_fk", int64(1), "id", "public", "organizations", int64(2), "account_id", "a", "a", test.local, test.referenced}
			assertOpenGaussCatalogObject(t, openGaussForeignKeysSQL, row, "foreign_keys", "repeated_column", "public.accounts (accounts_org_fk)")
		})
	}
}

func assertOpenGaussCatalogObject(t *testing.T, query string, row []driver.Value, stage, feature, object string) *SourceError {
	t.Helper()
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	activeMockCatalogState = &mockCatalogState{engine: EngineOpenGauss, openGaussRows: map[string][][]driver.Value{query: {row}}}
	database, err := sql.Open("aoci_mock_catalog", "credential_sentinel")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	collector := newCollectorForTest(func(string) string { return "credential_sentinel" }, func(string, string) (*sql.DB, error) { return database, nil })
	source := SourceConfig{SourceID: "ogtemp", Engine: EngineOpenGauss, Database: "app", Namespaces: []string{row[0].(string)}, CredentialEnv: "AOCI_DB_OGTEMP_DSN", Enabled: true}
	_, snapshot, files, err := collector.Snapshot(context.Background(), source)
	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || sourceErr.Code != "unsupported_catalog_feature" || sourceErr.Op != "catalog_query_"+stage+"_"+feature ||
		files != nil || len(snapshot.Tables) != 0 {
		t.Fatalf("expected unsupported %s with no partial evidence, got %v", feature, err)
	}
	if sourceErr.Object != object {
		t.Fatalf("%s object = %q, want %q", feature, sourceErr.Object, object)
	}
	return sourceErr
}

func TestOpenGaussCollectorRejectsUnsupportedServerProfiles(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	for _, test := range []struct {
		name, version, deployment, compatibility, database, feature string
	}{
		{"version", "6.0.4", "OpenSourceCentralized", "A", "app", "opengauss_version"},
		{"deployment", "6.0.5", "OpenSourceDistributed", "A", "app", "deployment_mode"},
		{"compatibility", "6.0.5", "OpenSourceCentralized", "B", "app", "compatibility_mode"},
		{"database", "6.0.5", "OpenSourceCentralized", "A", "other_app", "database_identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &mockCatalogState{engine: EngineOpenGauss, openGaussVersion: test.version, openGaussDeployment: test.deployment, openGaussCompatibility: test.compatibility}
			activeMockCatalogState = state
			database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
			collector := newCollectorForTest(func(string) string { return "credential_sentinel" }, func(string, string) (*sql.DB, error) { return database, nil })
			source := SourceConfig{SourceID: "ogtemp", Engine: EngineOpenGauss, Database: test.database, Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_OGTEMP_DSN", Enabled: true}
			_, _, _, err := collector.Snapshot(context.Background(), source)
			var sourceErr *SourceError
			if !errors.As(err, &sourceErr) || sourceErr.Code != "unsupported_catalog_feature" ||
				sourceErr.Op != "catalog_query_facts_"+test.feature || sourceErr.Object != "" {
				t.Fatalf("unsupported openGauss profile must be rejected without an object: %+v, err=%v", sourceErr, err)
			}
		})
	}
}

func TestOpenGaussCollectorAcceptsAAndPGProfiles(t *testing.T) {
	registerMockCatalogDriver.Do(func() { sql.Register("aoci_mock_catalog", mockCatalogDriver{}) })
	for _, compatibility := range []string{"A", "PG"} {
		t.Run(compatibility, func(t *testing.T) {
			state := &mockCatalogState{engine: EngineOpenGauss, openGaussCompatibility: compatibility}
			activeMockCatalogState = state
			database, _ := sql.Open("aoci_mock_catalog", "credential_sentinel")
			collector := newCollectorForTest(func(string) string { return "credential_sentinel" }, func(string, string) (*sql.DB, error) { return database, nil })
			source := SourceConfig{SourceID: "ogtemp", Engine: EngineOpenGauss, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_OGTEMP_DSN", Enabled: true}
			manifest, _, _, err := collector.Snapshot(context.Background(), source)
			if err != nil || !strings.Contains(manifest.ServerVersion, "compatibility "+compatibility) {
				t.Fatalf("supported openGauss compatibility profile was rejected: manifest=%+v err=%v", manifest, err)
			}
		})
	}
}

func TestDriverNameForEngineFailsClosed(t *testing.T) {
	for engine, want := range map[Engine]string{EnginePostgreSQL: "pgx", EngineMySQL: "mysql", EngineOpenGauss: "opengauss"} {
		got, err := driverNameForEngine(engine)
		if err != nil || got != want {
			t.Fatalf("engine %s routed to %q: %v", engine, got, err)
		}
	}
	if _, err := driverNameForEngine("oracle"); err == nil {
		t.Fatal("unknown engine received a driver")
	}
}

func TestCatalogTransactionOptionsFailClosed(t *testing.T) {
	for _, engine := range []Engine{EnginePostgreSQL, EngineMySQL, EngineOpenGauss} {
		options, err := catalogTransactionOptions(engine)
		if err != nil || !options.ReadOnly || options.Isolation != sql.LevelRepeatableRead {
			t.Fatalf("engine %s received unexpected transaction options: options=%+v err=%v", engine, options, err)
		}
	}
	if _, err := catalogTransactionOptions("oracle"); err == nil {
		t.Fatal("unsupported engine received transaction options")
	}
}

func mockCatalogResult(engine Engine, query string) ([]string, [][]driver.Value) {
	if engine == EngineMySQL {
		return mockMySQLResult(query)
	}
	if engine == EngineOpenGauss {
		return mockOpenGaussResult(query)
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
		return []string{"schema", "table", "name", "unique", "primary", "method", "position", "column", "expression", "included", "predicate", "descending"}, nil
	case postgresPartitionParentsSQL:
		return []string{"schema", "table", "method", "expression"}, nil
	case postgresPartitionChildrenSQL:
		return []string{"schema", "table", "parent_schema", "parent_table", "bound"}, nil
	default:
		return []string{"unexpected"}, nil
	}
}

func mockOpenGaussResult(query string) ([]string, [][]driver.Value) {
	switch query {
	case openGaussFactsSQL:
		version, deployment, compatibility := "6.0.5", "OpenSourceCentralized", "A"
		if activeMockCatalogState.openGaussVersion != "" {
			version = activeMockCatalogState.openGaussVersion
		}
		if activeMockCatalogState.openGaussDeployment != "" {
			deployment = activeMockCatalogState.openGaussDeployment
		}
		if activeMockCatalogState.openGaussCompatibility != "" {
			compatibility = activeMockCatalogState.openGaussCompatibility
		}
		return []string{"opengauss_version", "server_version", "deployment", "compatibility", "database"}, [][]driver.Value{{version, "9.2.4", deployment, compatibility, "app"}}
	case openGaussTablesSQL:
		return []string{"schema", "table"}, [][]driver.Value{{"public", "accounts"}}
	case openGaussUnsupportedSQL:
		return []string{"schema", "table", "feature"}, [][]driver.Value{{"partitioned", "events", "partitioned_table"}}
	case openGaussPartitionsSQL:
		return []string{"schema", "table", "parttype", "partstrategy"}, nil
	case openGaussColumnsSQL:
		return []string{"schema", "table", "ordinal", "column", "native_type", "canonical_type", "nullable", "default", "serial", "generated", "generation"}, [][]driver.Value{
			{"public", "accounts", int64(2), "email", "text", "text", false, nil, false, "NEVER", nil},
			{"public", "accounts", int64(1), "id", "bigint", "bigint", false, "nextval('accounts_id_seq'::regclass)", true, "NEVER", nil},
		}
	case openGaussKeysSQL:
		return []string{"schema", "table", "name", "kind", "column", "vector"}, [][]driver.Value{
			{"public", "accounts", "accounts_pk", "p", "email", "1 2"},
			{"public", "accounts", "accounts_pk", "p", "id", "1 2"},
		}
	case openGaussForeignKeysSQL:
		return []string{"schema", "table", "name", "local_attnum", "column", "ref_schema", "ref_table", "ref_attnum", "ref_column", "update", "delete", "local_vector", "ref_vector"}, nil
	case openGaussChecksSQL:
		return []string{"schema", "table", "name", "expression"}, nil
	case openGaussIndexesSQL:
		return []string{"schema", "table", "name", "unique", "primary", "method", "column", "vector", "definition", "predicate", "key_count", "total_count", "options", "visible", "usable", "valid", "ready", "exclusion", "immediate"}, [][]driver.Value{
			{"public", "accounts", "accounts_pk", true, true, "btree", "email", "1 2", "CREATE UNIQUE INDEX accounts_pk ON accounts USING btree (id, email DESC)", "", int64(2), int64(2), "0 3", true, true, true, true, false, true},
			{"public", "accounts", "accounts_pk", true, true, "btree", "id", "1 2", "CREATE UNIQUE INDEX accounts_pk ON accounts USING btree (id, email DESC)", "", int64(2), int64(2), "0 3", true, true, true, true, false, true},
		}
	default:
		return []string{"unexpected"}, nil
	}
}

func mockMySQLResult(query string) ([]string, [][]driver.Value) {
	switch query {
	case mysqlFactsSQL:
		version := "8.4.0"
		if activeMockCatalogState.mysqlVersion != "" {
			version = activeMockCatalogState.mysqlVersion
		}
		return []string{"version", "lower_case_table_names"}, [][]driver.Value{{version, int64(0)}}
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
	case mysqlIndexesSQL, mysqlIndexesPre8013SQL:
		return []string{"schema", "table", "name", "non_unique", "position", "column", "expression", "prefix", "collation", "method", "visible"}, [][]driver.Value{{"aoci_test", "Order Items", "name_prefix", int64(1), int64(1), "select", nil, int64(12), "A", "BTREE", "NO"}}
	case mysqlIndexesPre80SQL:
		// What the 5.7 projection can answer: NULL AS EXPRESSION, 'YES' AS IS_VISIBLE.
		return []string{"schema", "table", "name", "non_unique", "position", "column", "expression", "prefix", "collation", "method", "visible"}, [][]driver.Value{{"aoci_test", "Order Items", "name_prefix", int64(1), int64(1), "select", nil, int64(12), "A", "BTREE", "YES"}}
	case mysqlPartitionsSQL:
		return []string{"schema", "table", "name", "ordinal", "method", "expression", "description"}, nil
	default:
		return []string{"unexpected"}, nil
	}
}
