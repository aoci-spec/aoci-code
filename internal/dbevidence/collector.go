package dbevidence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	opengauss "gitcode.com/opengauss/openGauss-connector-go-pq"
)

type SourceError struct {
	Code     string
	SourceID string
	Op       string
}

type catalogQueryFailure struct {
	name string
	err  error
}

type catalogStageFailure struct {
	kind string
	err  error
}

type unsupportedCatalogFeature struct {
	feature string
}

func (err *catalogQueryFailure) Error() string { return "catalog query " + err.name + " failed" }
func (err *catalogQueryFailure) Unwrap() error { return err.err }
func (err *catalogStageFailure) Error() string { return "catalog stage " + err.kind + " failed" }
func (err *catalogStageFailure) Unwrap() error { return err.err }
func (err *unsupportedCatalogFeature) Error() string {
	return "unsupported catalog feature " + err.feature
}

func wrapCatalogQuery(name string, err error) error {
	if err == nil {
		return nil
	}
	return &catalogQueryFailure{name: name, err: err}
}

func (err *SourceError) Error() string {
	return fmt.Sprintf("database source %s: %s (%s)", err.SourceID, err.Code, err.Op)
}

type databaseOpener func(driverName, dsn string) (*sql.DB, error)

type Collector struct {
	getenv             func(string) string
	open               databaseOpener
	credentialProvider CredentialProvider
}

func NewCollector() *Collector {
	return &Collector{getenv: os.Getenv, open: openDatabase, credentialProvider: NewEnvironmentCredentialProvider(os.Getenv)}
}

func openDatabase(driverName, dsn string) (*sql.DB, error) {
	if driverName != "opengauss" {
		return sql.Open(driverName, dsn)
	}
	return openOpenGaussDatabase(dsn, os.Getenv)
}

func openOpenGaussDatabase(dsn string, _ func(string) string) (*sql.DB, error) {
	config, err := opengauss.ParseConfigStrict(dsn)
	if err != nil {
		return nil, err
	}
	connector, err := opengauss.NewConnectorConfig(config)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (collector *Collector) Inspect(ctx context.Context, source SourceConfig) (Inspection, error) {
	// Inspect exercises the complete catalog-query permission boundary but
	// discards the collected facts instead of writing runtime Evidence.
	manifest, tables, err := collector.collect(ctx, source, false)
	if err != nil {
		return Inspection{}, err
	}
	namespaces := make([]string, 0)
	seen := map[string]struct{}{}
	for _, table := range tables {
		if _, exists := seen[table.Namespace]; !exists {
			seen[table.Namespace] = struct{}{}
			namespaces = append(namespaces, table.Namespace)
		}
	}
	sort.Strings(namespaces)
	return Inspection{
		Version: 1, SourceID: manifest.SourceID, Engine: manifest.Engine,
		Database: manifest.Database, ServerVersion: manifest.ServerVersion,
		CaseSemantics: manifest.CaseSemantics, VisibleNamespaces: namespaces,
		VisibleTables: len(tables), BusinessDataRead: false,
	}, nil
}

func (collector *Collector) Snapshot(ctx context.Context, source SourceConfig) (SourceManifest, Snapshot, map[string][]byte, error) {
	manifest, tables, err := collector.collect(ctx, source, false)
	if err != nil {
		return SourceManifest{}, Snapshot{}, nil, err
	}
	snapshot, files, err := BuildSnapshot(manifest, tables)
	if err != nil {
		return SourceManifest{}, Snapshot{}, nil, &SourceError{Code: "evidence_invalid", SourceID: source.SourceID, Op: "canonicalize"}
	}
	return manifest, snapshot, files, nil
}

func (collector *Collector) collect(ctx context.Context, source SourceConfig, inspectOnly bool) (SourceManifest, []TableEvidence, error) {
	if err := NormalizeSource(&source); err != nil {
		return SourceManifest{}, nil, &SourceError{Code: "configuration_invalid", SourceID: "invalid", Op: "validate"}
	}
	if !source.Enabled {
		return SourceManifest{}, nil, &SourceError{Code: "source_disabled", SourceID: source.SourceID, Op: "connect"}
	}
	provider := collector.credentialProvider
	if provider == nil {
		provider = NewEnvironmentCredentialProvider(collector.getenv)
	}
	dsn, credentialErr := provider.Resolve(ctx, CredentialRequest{SourceID: source.SourceID, Reference: source.CredentialEnv})
	if credentialErr != nil || strings.TrimSpace(dsn) == "" {
		return SourceManifest{}, nil, &SourceError{Code: "credential_env_missing", SourceID: source.SourceID, Op: "connect"}
	}
	driverName, err := driverNameForEngine(source.Engine)
	if err != nil {
		return SourceManifest{}, nil, &SourceError{Code: "configuration_invalid", SourceID: source.SourceID, Op: "driver_select"}
	}
	database, err := collector.open(driverName, dsn)
	if err != nil {
		return SourceManifest{}, nil, &SourceError{Code: "connection_failed", SourceID: source.SourceID, Op: "open"}
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	connectCtx, cancel := context.WithTimeout(ctx, source.ConnectTimeout())
	err = database.PingContext(connectCtx)
	cancel()
	if err != nil {
		return SourceManifest{}, nil, classifySourceError(ctx, source.SourceID, "ping", err)
	}
	txCtx, cancel := context.WithTimeout(ctx, source.QueryTimeout())
	defer cancel()
	tx, err := database.BeginTx(txCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return SourceManifest{}, nil, classifySourceError(ctx, source.SourceID, "begin_read_only", err)
	}
	manifest := SourceManifest{
		Version: SourceManifestVersion, SourceID: source.SourceID, Engine: source.Engine,
		Database: source.Database, Namespaces: sortedCopy(source.Namespaces),
		IncludeNamespaces: sortedCopy(source.IncludeNamespaces), ExcludeNamespaces: sortedCopy(source.ExcludeNamespaces),
		IncludeTables: sortedCopy(source.IncludeTables), ExcludeTables: sortedCopy(source.ExcludeTables),
		BusinessDataRead: false,
	}
	var tables []TableEvidence
	switch source.Engine {
	case EnginePostgreSQL:
		manifest.CaseSemantics = CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}
		manifest.ServerVersion, tables, err = collectPostgreSQL(txCtx, tx, source, inspectOnly)
	case EngineMySQL:
		var lowerCase int
		manifest.ServerVersion, lowerCase, tables, err = collectMySQL(txCtx, tx, source, inspectOnly)
		manifest.CaseSemantics = CaseSemantics{IdentifierCase: "server_lower_case_table_names", LowerCaseTableNames: &lowerCase}
	case EngineOpenGauss:
		manifest.CaseSemantics = CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}
		manifest.ServerVersion, tables, err = collectOpenGauss(txCtx, tx, source, inspectOnly)
	default:
		err = fmt.Errorf("unsupported engine")
	}
	if err != nil {
		_ = tx.Rollback()
		op := "catalog_query"
		var queryFailure *catalogQueryFailure
		if errors.As(err, &queryFailure) {
			op += "_" + queryFailure.name
		}
		var stageFailure *catalogStageFailure
		if errors.As(err, &stageFailure) {
			op += "_" + stageFailure.kind
		}
		var unsupported *unsupportedCatalogFeature
		if errors.As(err, &unsupported) {
			return SourceManifest{}, nil, &SourceError{Code: "unsupported_catalog_feature", SourceID: source.SourceID, Op: op + "_" + unsupported.feature}
		}
		return SourceManifest{}, nil, classifySourceError(ctx, source.SourceID, op, err)
	}
	manifest.ServerVersion = normalizeCatalogText(manifest.ServerVersion)
	if err := tx.Commit(); err != nil {
		return SourceManifest{}, nil, classifySourceError(ctx, source.SourceID, "commit_read_only", err)
	}
	return manifest, tables, nil
}

func driverNameForEngine(engine Engine) (string, error) {
	switch engine {
	case EnginePostgreSQL:
		return "pgx", nil
	case EngineMySQL:
		return "mysql", nil
	case EngineOpenGauss:
		return "opengauss", nil
	default:
		return "", fmt.Errorf("unsupported engine")
	}
}

func classifySourceError(parent context.Context, sourceID, op string, err error) error {
	code := "catalog_query_failed"
	baseOp := op
	if errors.Is(parent.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		code = "cancelled"
	} else if errors.Is(parent.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	} else {
		cause := err
		var queryFailure *catalogQueryFailure
		if errors.As(err, &queryFailure) {
			cause = queryFailure.err
		}
		var stageFailure *catalogStageFailure
		if errors.As(cause, &stageFailure) {
			cause = stageFailure.err
		}
		sqlState := ""
		var state interface{ SQLState() string }
		if errors.As(cause, &state) {
			sqlState = strings.ToLower(state.SQLState())
			if len(sqlState) == 5 {
				op += "_sqlstate_" + sqlState
			}
		}
		lower := strings.ToLower(cause.Error())
		if strings.HasPrefix(sqlState, "28") || sqlState == "42501" || strings.Contains(lower, "permission") || strings.Contains(lower, "denied") || strings.Contains(lower, "privilege") {
			code = "permission_denied"
		} else if baseOp == "ping" || baseOp == "open" {
			code = "connection_failed"
		}
	}
	return &SourceError{Code: code, SourceID: sourceID, Op: op}
}

func queryRows(ctx context.Context, tx *sql.Tx, query string) (*sql.Rows, error) {
	return tx.QueryContext(ctx, query)
}

func queryRow(ctx context.Context, tx *sql.Tx, query string) *sql.Row {
	return tx.QueryRowContext(ctx, query)
}

func tableKey(namespace, table string) string { return namespace + "\x00" + table }

func newEvidenceTable(source SourceConfig, namespace, table string) (TableEvidence, error) {
	objectRef, err := CanonicalObjectRef(source.SourceID, namespace, table)
	if err != nil {
		return TableEvidence{}, err
	}
	return TableEvidence{
		Version: EvidenceVersion, ObjectRef: objectRef, Engine: source.Engine,
		SourceID: source.SourceID, Database: source.Database, Namespace: namespace,
		Name: table, Kind: "base_table", Columns: []Column{}, UniqueConstraints: []KeyConstraint{},
		ForeignKeys: []ForeignKey{}, Checks: []CheckConstraint{}, Indexes: []Index{},
	}, nil
}

func sortedTableValues(tables map[string]*TableEvidence) []TableEvidence {
	result := make([]TableEvidence, 0, len(tables))
	for _, table := range tables {
		result = append(result, *table)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ObjectRef < result[j].ObjectRef })
	return result
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func boolFromYES(value string) bool { return strings.EqualFold(value, "YES") }

func orderedColumns(values map[int]string) []string {
	ordinals := make([]int, 0, len(values))
	for ordinal := range values {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	result := make([]string, 0, len(ordinals))
	for _, ordinal := range ordinals {
		result = append(result, values[ordinal])
	}
	return result
}
