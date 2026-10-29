package dbevidence

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ncruces/go-sqlite3"
	sqlitedriver "github.com/ncruces/go-sqlite3/driver"
)

func sqliteDataSource(root string, source SourceConfig) (string, error) {
	path := source.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", &SourceError{Code: "configuration_invalid", SourceID: source.SourceID, Op: "file_path"}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", &SourceError{Code: "connection_failed", SourceID: source.SourceID, Op: "file_stat"}
	}
	path = filepath.ToSlash(abs)
	uri := url.URL{Path: path}
	query := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "trusted_schema(0)", "busy_timeout(1000)"}}.Encode()
	return "file:" + uri.EscapedPath() + "?" + query, nil
}

func openSQLiteDatabase(dsn string) (*sql.DB, error) {
	return sqlitedriver.Open(dsn, func(conn *sqlite3.Conn) error {
		return conn.SetAuthorizer(sqliteCatalogAuthorizer)
	})
}

// The file is untrusted input. In addition to read-only access, refuse business
// reads, ATTACH, extension loading, and SQL outside the catalog operations below.
func sqliteCatalogAuthorizer(action sqlite3.AuthorizerActionCode, first, second, schema, _ string) sqlite3.AuthorizerReturnCode {
	switch action {
	case sqlite3.AUTH_SELECT, sqlite3.AUTH_TRANSACTION:
		return sqlite3.AUTH_OK
	case sqlite3.AUTH_FUNCTION:
		if second == "sqlite_version" {
			return sqlite3.AUTH_OK
		}
	case sqlite3.AUTH_READ:
		if schema == "main" && (first == "sqlite_master" || first == "sqlite_schema") {
			return sqlite3.AUTH_OK
		}
	case sqlite3.AUTH_PRAGMA:
		if sqliteCatalogPragma(first) || (first == "query_only" && second == "") {
			return sqlite3.AUTH_OK
		}
	}
	return sqlite3.AUTH_DENY
}

func sqliteCatalogPragma(name string) bool {
	switch name {
	case "table_list", "table_xinfo", "index_list", "index_xinfo", "foreign_key_list":
		return true
	}
	return false
}

func collectSQLite(ctx context.Context, tx *sql.Tx, source SourceConfig) (string, []TableEvidence, error) {
	var version string
	if err := tx.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return "", nil, wrapCatalogQuery("facts", err)
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA main.table_list`)
	if err != nil {
		return "", nil, wrapCatalogQuery("tables", err)
	}
	type selection struct {
		name string
	}
	var selected []selection
	for rows.Next() {
		var item selection
		var schema, kind string
		var columns int
		var wr, strict bool
		if err := rows.Scan(&schema, &item.name, &kind, &columns, &wr, &strict); err != nil {
			rows.Close()
			return "", nil, err
		}
		if schema != "main" || strings.HasPrefix(strings.ToLower(item.name), "sqlite_") || !Included(source, "main", item.name) || kind == "view" {
			continue
		}
		if kind != "table" {
			rows.Close()
			return "", nil, &unsupportedCatalogFeature{feature: "sqlite_virtual_table"}
		}
		if wr || strict {
			rows.Close()
			return "", nil, &unsupportedCatalogFeature{feature: "sqlite_table_options"}
		}
		selected = append(selected, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	tables := make([]TableEvidence, 0, len(selected))
	for _, item := range selected {
		table, err := newEvidenceTable(source, "main", item.name)
		if err != nil {
			return "", nil, err
		}
		var ddl string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM main.sqlite_schema WHERE type = 'table' AND name = ?`, item.name).Scan(&ddl); err != nil {
			return "", nil, wrapCatalogQuery("definition", err)
		}
		tokens := sqliteDDLTokens(ddl)
		for _, unsupported := range []string{"CHECK", "DEFERRABLE", "COLLATE", "CONFLICT"} {
			if tokens[unsupported] {
				return "", nil, &unsupportedCatalogFeature{feature: "sqlite_" + strings.ToLower(unsupported)}
			}
		}
		if err := sqliteColumns(ctx, tx, &table); err != nil {
			return "", nil, wrapCatalogQuery("columns", err)
		}
		if err := sqliteIndexes(ctx, tx, &table); err != nil {
			return "", nil, wrapCatalogQuery("indexes", err)
		}
		// An INTEGER primary key without a separate primary-key index aliases
		// rowid; INTEGER PRIMARY KEY DESC is the exception and has that index.
		if table.PrimaryKey != nil && len(table.PrimaryKey.Columns) == 1 {
			indexed := false
			for _, index := range table.Indexes {
				indexed = indexed || index.Primary
			}
			for i := range table.Columns {
				column := &table.Columns[i]
				if !indexed && column.Name == table.PrimaryKey.Columns[0] && strings.EqualFold(column.NativeType, "INTEGER") {
					column.Nullable = false
					column.AutoIncrement = tokens["AUTOINCREMENT"]
				}
			}
		}
		if err := sqliteForeignKeys(ctx, tx, &table); err != nil {
			return "", nil, wrapCatalogQuery("foreign_keys", err)
		}
		tables = append(tables, table)
	}
	return version, tables, nil
}

func sqliteColumns(ctx context.Context, tx *sql.Tx, table *TableEvidence) error {
	rows, err := sqlitePragma(ctx, tx, "table_xinfo", table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()
	primary := map[int]string{}
	for rows.Next() {
		var cid, pk, hidden int
		var column Column
		var notNull bool
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.NativeType, &notNull, &defaultValue, &pk, &hidden); err != nil {
			return err
		}
		if hidden != 0 {
			return &unsupportedCatalogFeature{feature: "sqlite_generated_column"}
		}
		column.Ordinal, column.CanonicalType = cid+1, sqliteAffinity(column.NativeType)
		column.Nullable = !notNull
		column.DefaultExpression = nullableString(defaultValue)
		table.Columns = append(table.Columns, column)
		if pk > 0 {
			primary[pk] = column.Name
		}
	}
	if len(primary) > 0 {
		table.PrimaryKey = &KeyConstraint{Name: "sqlite_primary_key", Columns: orderedColumns(primary)}
	}
	return rows.Err()
}

func sqliteIndexes(ctx context.Context, tx *sql.Tx, table *TableEvidence) error {
	rows, err := sqlitePragma(ctx, tx, "index_list", table.Name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var index Index
		var seq int
		var origin string
		var partial bool
		if err := rows.Scan(&seq, &index.Name, &index.Unique, &origin, &partial); err != nil {
			rows.Close()
			return err
		}
		if partial {
			rows.Close()
			return &unsupportedCatalogFeature{feature: "sqlite_partial_index"}
		}
		index.Primary, index.Method = origin == "pk", "btree"
		table.Indexes = append(table.Indexes, index)
		if origin == "u" {
			table.UniqueConstraints = append(table.UniqueConstraints, KeyConstraint{Name: index.Name})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range table.Indexes {
		index := &table.Indexes[i]
		rows, err := sqlitePragma(ctx, tx, "index_xinfo", index.Name)
		if err != nil {
			return err
		}
		for rows.Next() {
			var seq, cid int
			var name sql.NullString
			var desc bool
			var key bool
			var coll string
			if err := rows.Scan(&seq, &cid, &name, &desc, &coll, &key); err != nil {
				rows.Close()
				return err
			}
			if !key {
				continue
			}
			if cid < 0 || !name.Valid || !strings.EqualFold(coll, "BINARY") {
				rows.Close()
				return &unsupportedCatalogFeature{feature: "sqlite_index_expression_or_collation"}
			}
			index.Elements = append(index.Elements, IndexElement{Position: seq + 1, Column: &name.String, Descending: desc})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for j := range table.UniqueConstraints {
			if table.UniqueConstraints[j].Name == index.Name {
				sort.Slice(index.Elements, func(i, j int) bool { return index.Elements[i].Position < index.Elements[j].Position })
				for _, element := range index.Elements {
					table.UniqueConstraints[j].Columns = append(table.UniqueConstraints[j].Columns, *element.Column)
				}
			}
		}
	}
	return nil
}

func sqliteForeignKeys(ctx context.Context, tx *sql.Tx, table *TableEvidence) error {
	rows, err := sqlitePragma(ctx, tx, "foreign_key_list", table.Name)
	if err != nil {
		return err
	}
	type foreignRow struct {
		id, seq                                   int
		parent, from, update, deleteAction, match string
		to                                        sql.NullString
	}
	var facts []foreignRow
	for rows.Next() {
		var row foreignRow
		if err := rows.Scan(&row.id, &row.seq, &row.parent, &row.from, &row.to, &row.update, &row.deleteAction, &row.match); err != nil {
			rows.Close()
			return err
		}
		facts = append(facts, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].id != facts[j].id {
			return facts[i].id < facts[j].id
		}
		return facts[i].seq < facts[j].seq
	})
	for _, row := range facts {
		if row.seq == 0 {
			table.ForeignKeys = append(table.ForeignKeys, ForeignKey{Name: fmt.Sprintf("sqlite_foreign_key_%d", row.id), ReferencedObject: row.parent, UpdateAction: row.update, DeleteAction: row.deleteAction})
		}
		foreign := &table.ForeignKeys[len(table.ForeignKeys)-1]
		foreign.Columns = append(foreign.Columns, row.from)
		foreign.ReferencedColumns = append(foreign.ReferencedColumns, row.to.String)
	}
	for i := range table.ForeignKeys {
		foreign := &table.ForeignKeys[i]
		var parent string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM main.sqlite_schema WHERE type = 'table' AND name = ? COLLATE NOCASE`, foreign.ReferencedObject).Scan(&parent); err != nil {
			return &unsupportedCatalogFeature{feature: "sqlite_foreign_key_target"}
		}
		parentTable := TableEvidence{Name: parent}
		if err := sqliteColumns(ctx, tx, &parentTable); err != nil {
			return err
		}
		if foreign.ReferencedColumns[0] == "" {
			if parentTable.PrimaryKey == nil || len(parentTable.PrimaryKey.Columns) != len(foreign.Columns) {
				return &unsupportedCatalogFeature{feature: "sqlite_foreign_key_target"}
			}
			foreign.ReferencedColumns = parentTable.PrimaryKey.Columns
		} else {
			for j, reference := range foreign.ReferencedColumns {
				found := false
				for _, column := range parentTable.Columns {
					if sqliteFold(reference) == sqliteFold(column.Name) {
						foreign.ReferencedColumns[j], found = column.Name, true
						break
					}
				}
				if !found {
					return &unsupportedCatalogFeature{feature: "sqlite_foreign_key_target"}
				}
			}
		}
		foreign.ReferencedObject, err = CanonicalObjectRef(table.SourceID, "main", parent)
		if err != nil {
			return err
		}
	}
	return nil
}

func sqliteFold(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}

// Direct PRAGMAs cannot be shadowed by a business table named pragma_*.
// SQLite accepts a quoted identifier as the argument; quote every byte.
func sqlitePragma(ctx context.Context, tx *sql.Tx, pragma, name string) (*sql.Rows, error) {
	return tx.QueryContext(ctx, `PRAGMA main.`+pragma+`("`+strings.ReplaceAll(name, `"`, `""`)+`")`)
}

func sqliteAffinity(native string) string {
	native = sqliteFold(native)
	switch {
	case strings.Contains(native, "int"):
		return "integer"
	case strings.Contains(native, "char"), strings.Contains(native, "clob"), strings.Contains(native, "text"):
		return "text"
	case native == "", strings.Contains(native, "blob"):
		return "blob"
	case strings.Contains(native, "real"), strings.Contains(native, "floa"), strings.Contains(native, "doub"):
		return "real"
	default:
		return "numeric"
	}
}

// This is only a conservative feature guard, never a DDL-to-Evidence parser.
// Ignore literals, quoted names and comments so their text cannot name features.
func sqliteDDLTokens(ddl string) map[string]bool {
	tokens := map[string]bool{}
	for i := 0; i < len(ddl); {
		switch {
		case ddl[i] == '\'' || ddl[i] == '"' || ddl[i] == '`' || ddl[i] == '[':
			end := ddl[i]
			if end == '[' {
				end = ']'
			}
			i++
			for i < len(ddl) {
				if ddl[i] == end {
					i++
					if i >= len(ddl) || ddl[i] != end || end == ']' {
						break
					}
				}
				i++
			}
		case strings.HasPrefix(ddl[i:], "--"):
			for i < len(ddl) && ddl[i] != '\n' {
				i++
			}
		case strings.HasPrefix(ddl[i:], "/*"):
			if end := strings.Index(ddl[i+2:], "*/"); end >= 0 {
				i += end + 4
			} else {
				i = len(ddl)
			}
		case ddl[i] >= 'a' && ddl[i] <= 'z' || ddl[i] >= 'A' && ddl[i] <= 'Z' || ddl[i] == '_':
			start := i
			for i < len(ddl) && (ddl[i] >= 'a' && ddl[i] <= 'z' || ddl[i] >= 'A' && ddl[i] <= 'Z' || ddl[i] >= '0' && ddl[i] <= '9' || ddl[i] == '_' || ddl[i] == '$' || ddl[i] >= 128) {
				i++
			}
			tokens[strings.ToUpper(ddl[start:i])] = true
		default:
			i++
		}
	}
	return tokens
}
