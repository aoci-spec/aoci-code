package dbevidence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func collectMySQL(ctx context.Context, tx *sql.Tx, source SourceConfig, inspectOnly bool) (string, int, []TableEvidence, error) {
	var serverVersion string
	var lowerCaseTableNames int
	if err := queryRow(ctx, tx, mysqlFactsSQL).Scan(&serverVersion, &lowerCaseTableNames); err != nil {
		return "", 0, nil, wrapCatalogQuery("facts", err)
	}
	if lowerCaseTableNames < 0 || lowerCaseTableNames > 2 {
		return "", 0, nil, fmt.Errorf("unsupported lower_case_table_names value %d", lowerCaseTableNames)
	}
	tables, err := mysqlTables(ctx, tx, source)
	err = wrapCatalogQuery("tables", err)
	if err != nil || inspectOnly {
		return serverVersion, lowerCaseTableNames, sortedTableValues(tables), err
	}
	for _, loader := range []struct {
		name string
		load func(context.Context, *sql.Tx, SourceConfig, map[string]*TableEvidence) error
	}{
		{"columns", mysqlColumns}, {"keys", mysqlKeys},
		{"foreign_keys", mysqlForeignKeys}, {"checks", mysqlChecks},
		{"indexes", mysqlIndexes}, {"partitions", mysqlPartitions},
	} {
		if err := loader.load(ctx, tx, source, tables); err != nil {
			return "", 0, nil, wrapCatalogQuery(loader.name, err)
		}
	}
	return serverVersion, lowerCaseTableNames, sortedTableValues(tables), nil
}

func mysqlTables(ctx context.Context, tx *sql.Tx, source SourceConfig) (map[string]*TableEvidence, error) {
	rows, err := queryRows(ctx, tx, mysqlTablesSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := map[string]*TableEvidence{}
	for rows.Next() {
		var namespace, name string
		if err := rows.Scan(&namespace, &name); err != nil {
			return nil, err
		}
		if !Included(source, namespace, name) {
			continue
		}
		table, err := newEvidenceTable(source, namespace, name)
		if err != nil {
			return nil, err
		}
		tables[tableKey(namespace, name)] = &table
	}
	return tables, rows.Err()
}

func mysqlColumns(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlColumnsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, columnName, nativeType, canonicalType, extra, generationExpression string
		var ordinal int
		var nullable bool
		var defaultExpression sql.NullString
		if err := rows.Scan(&namespace, &tableName, &ordinal, &columnName, &nativeType, &canonicalType,
			&nullable, &defaultExpression, &extra, &generationExpression); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		lowerExtra := strings.ToLower(extra)
		generated := ""
		if strings.Contains(lowerExtra, "stored generated") {
			generated = "stored"
		} else if strings.Contains(lowerExtra, "virtual generated") {
			generated = "virtual"
		}
		table.Columns = append(table.Columns, Column{
			Ordinal: ordinal, Name: columnName, NativeType: nativeType, CanonicalType: canonicalType,
			Nullable: nullable, DefaultExpression: nullableString(defaultExpression),
			AutoIncrement: strings.Contains(lowerExtra, "auto_increment"), Generated: generated,
			GenerationExpression: generationExpression,
		})
	}
	return rows.Err()
}

func mysqlKeys(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlKeysSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	type keyState struct {
		table   *TableEvidence
		kind    string
		key     KeyConstraint
		columns map[int]string
	}
	states := map[string]*keyState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, name, kind, column string
		var ordinal int
		if err := rows.Scan(&namespace, &tableName, &name, &kind, &ordinal, &column); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			state = &keyState{table: table, kind: kind, key: KeyConstraint{Name: name}, columns: map[int]string{}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		state.columns[ordinal] = column
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, stateKey := range order {
		state := states[stateKey]
		state.key.Columns = orderedColumns(state.columns)
		if state.kind == "PRIMARY KEY" {
			key := state.key
			state.table.PrimaryKey = &key
		} else {
			state.table.UniqueConstraints = append(state.table.UniqueConstraints, state.key)
		}
	}
	return nil
}

func mysqlForeignKeys(ctx context.Context, tx *sql.Tx, source SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlForeignKeysSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	type fkState struct {
		table             *TableEvidence
		fk                ForeignKey
		columns           map[int]string
		referencedColumns map[int]string
	}
	states := map[string]*fkState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, name, column, refNamespace, refTable, refColumn, updateAction, deleteAction string
		var ordinal int
		if err := rows.Scan(&namespace, &tableName, &name, &ordinal, &column, &refNamespace, &refTable, &refColumn, &updateAction, &deleteAction); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		ref, err := CanonicalObjectRef(source.SourceID, refNamespace, refTable)
		if err != nil {
			return err
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			state = &fkState{table: table, fk: ForeignKey{Name: name, ReferencedObject: ref, UpdateAction: updateAction, DeleteAction: deleteAction}, columns: map[int]string{}, referencedColumns: map[int]string{}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		if state.fk.ReferencedObject != ref {
			return fmt.Errorf("foreign key %s has inconsistent target", name)
		}
		state.columns[ordinal] = column
		state.referencedColumns[ordinal] = refColumn
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range order {
		state := states[key]
		state.fk.Columns = orderedColumns(state.columns)
		state.fk.ReferencedColumns = orderedColumns(state.referencedColumns)
		state.table.ForeignKeys = append(state.table.ForeignKeys, state.fk)
	}
	return nil
}

func mysqlChecks(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlChecksSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, name, expression string
		if err := rows.Scan(&namespace, &tableName, &name, &expression); err != nil {
			return err
		}
		if table := tables[tableKey(namespace, tableName)]; table != nil {
			table.Checks = append(table.Checks, CheckConstraint{Name: name, Expression: expression})
		}
	}
	return rows.Err()
}

func mysqlIndexes(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlIndexesSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	type indexState struct {
		table *TableEvidence
		index Index
	}
	states := map[string]*indexState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, name, method, visibleText string
		var nonUnique, position int
		var column, expression, collation sql.NullString
		var prefix sql.NullInt64
		if err := rows.Scan(&namespace, &tableName, &name, &nonUnique, &position, &column, &expression, &prefix, &collation, &method, &visibleText); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			visible := strings.EqualFold(visibleText, "YES")
			state = &indexState{table: table, index: Index{Name: name, Unique: nonUnique == 0, Primary: name == "PRIMARY", Visible: &visible, Method: method, Elements: []IndexElement{}}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		element := IndexElement{Position: position, Descending: collation.Valid && strings.EqualFold(collation.String, "D")}
		if column.Valid {
			value := column.String
			element.Column = &value
		} else if expression.Valid {
			value := expression.String
			element.Expression = &value
		} else {
			return fmt.Errorf("index %s element %d has neither column nor expression", name, position)
		}
		if prefix.Valid {
			value := int(prefix.Int64)
			element.PrefixLength = &value
		}
		state.index.Elements = append(state.index.Elements, element)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range order {
		state := states[key]
		state.table.Indexes = append(state.table.Indexes, state.index)
	}
	return nil
}

func mysqlPartitions(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, mysqlPartitionsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, name, method string
		var ordinal int
		var expression, description sql.NullString
		if err := rows.Scan(&namespace, &tableName, &name, &ordinal, &method, &expression, &description); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		if table.Partition == nil {
			table.Partition = &Partition{Partitioned: true, Method: method, Expression: expression.String, Definitions: []PartitionDefinition{}}
		}
		table.Partition.Definitions = append(table.Partition.Definitions, PartitionDefinition{Name: name, Ordinal: ordinal, Description: description.String})
	}
	return rows.Err()
}
