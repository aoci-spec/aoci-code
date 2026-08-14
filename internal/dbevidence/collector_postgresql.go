package dbevidence

import (
	"context"
	"database/sql"
	"fmt"
)

func collectPostgreSQL(ctx context.Context, tx *sql.Tx, source SourceConfig, inspectOnly bool) (string, []TableEvidence, error) {
	var serverVersion string
	if err := queryRow(ctx, tx, postgresFactsSQL).Scan(&serverVersion); err != nil {
		return "", nil, wrapCatalogQuery("facts", err)
	}
	tables, err := postgresqlTables(ctx, tx, source)
	err = wrapCatalogQuery("tables", err)
	if err != nil || inspectOnly {
		return serverVersion, sortedTableValues(tables), err
	}
	for _, loader := range []struct {
		name string
		load func(context.Context, *sql.Tx, SourceConfig, map[string]*TableEvidence) error
	}{
		{"columns", postgresqlColumns}, {"keys", postgresqlKeys},
		{"foreign_keys", postgresqlForeignKeys}, {"checks", postgresqlChecks},
		{"indexes", postgresqlIndexes}, {"partition_parents", postgresqlPartitionParents},
		{"partition_children", postgresqlPartitionChildren},
	} {
		if err := loader.load(ctx, tx, source, tables); err != nil {
			return "", nil, wrapCatalogQuery(loader.name, err)
		}
	}
	return serverVersion, sortedTableValues(tables), nil
}

func postgresqlTables(ctx context.Context, tx *sql.Tx, source SourceConfig) (map[string]*TableEvidence, error) {
	rows, err := queryRows(ctx, tx, postgresTablesSQL)
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

func postgresqlColumns(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresColumnsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, columnName, nativeType, canonicalType string
		var ordinal int
		var nullable, serial bool
		var defaultExpression, identityGeneration, generationExpression sql.NullString
		var isIdentity, isGenerated string
		if err := rows.Scan(&namespace, &tableName, &ordinal, &columnName, &nativeType, &canonicalType,
			&nullable, &defaultExpression, &serial, &isIdentity, &identityGeneration, &isGenerated, &generationExpression); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		identity := ""
		if boolFromYES(isIdentity) {
			identity = identityGeneration.String
		}
		generated := ""
		if isGenerated != "NEVER" {
			generated = isGenerated
		}
		table.Columns = append(table.Columns, Column{
			Ordinal: ordinal, Name: columnName, NativeType: nativeType, CanonicalType: canonicalType,
			Nullable: nullable, DefaultExpression: nullableString(defaultExpression), Identity: identity,
			Serial: serial, Generated: generated, GenerationExpression: generationExpression.String,
		})
	}
	return rows.Err()
}

func postgresqlKeys(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresKeysSQL)
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
		var namespace, tableName, constraintName, kind, columnName string
		var ordinal int
		if err := rows.Scan(&namespace, &tableName, &constraintName, &kind, &ordinal, &columnName); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + constraintName
		state := states[stateKey]
		if state == nil {
			state = &keyState{table: table, kind: kind, key: KeyConstraint{Name: constraintName}, columns: map[int]string{}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		state.columns[ordinal] = columnName
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, stateKey := range order {
		state := states[stateKey]
		state.key.Columns = orderedColumns(state.columns)
		if state.kind == "p" {
			key := state.key
			state.table.PrimaryKey = &key
		} else {
			state.table.UniqueConstraints = append(state.table.UniqueConstraints, state.key)
		}
	}
	return nil
}

func postgresqlForeignKeys(ctx context.Context, tx *sql.Tx, source SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresForeignKeysSQL)
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

func postgresqlChecks(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresChecksSQL)
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

func postgresqlIndexes(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresIndexesSQL)
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
		var namespace, tableName, name, method, predicate string
		var column, expression sql.NullString
		var unique, primary, included bool
		var position int
		if err := rows.Scan(&namespace, &tableName, &name, &unique, &primary, &method, &position, &column, &expression, &included, &predicate); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			state = &indexState{table: table, index: Index{Name: name, Unique: unique, Primary: primary, Method: method, Predicate: predicate, Elements: []IndexElement{}}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		element := IndexElement{Position: position, Included: included}
		if column.Valid {
			columnCopy := column.String
			element.Column = &columnCopy
		} else if expression.Valid {
			expressionCopy := expression.String
			element.Expression = &expressionCopy
		} else {
			return fmt.Errorf("index %s element %d has neither column nor expression", name, position)
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

func postgresqlPartitionParents(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresPartitionParentsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, method, expression string
		if err := rows.Scan(&namespace, &tableName, &method, &expression); err != nil {
			return err
		}
		if table := tables[tableKey(namespace, tableName)]; table != nil {
			table.Partition = &Partition{Partitioned: true, Method: method, Expression: expression, ChildObjects: []string{}}
		}
	}
	return rows.Err()
}

func postgresqlPartitionChildren(ctx context.Context, tx *sql.Tx, source SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, postgresPartitionChildrenSQL)
	if err != nil {
		return &catalogStageFailure{kind: "execute", err: err}
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, parentNamespace, parentTable string
		var bound sql.NullString
		if err := rows.Scan(&namespace, &tableName, &parentNamespace, &parentTable, &bound); err != nil {
			return &catalogStageFailure{kind: "decode", err: err}
		}
		if !bound.Valid {
			return &catalogStageFailure{kind: "validate", err: fmt.Errorf("partition has no catalog bound")}
		}
		childRef, err := CanonicalObjectRef(source.SourceID, namespace, tableName)
		if err != nil {
			return &catalogStageFailure{kind: "identity", err: err}
		}
		parentRef, err := CanonicalObjectRef(source.SourceID, parentNamespace, parentTable)
		if err != nil {
			return &catalogStageFailure{kind: "identity", err: err}
		}
		attachPartitionParent(tables[tableKey(namespace, tableName)], parentRef, bound.String)
		appendPartitionChild(tables[tableKey(parentNamespace, parentTable)], childRef)
	}
	if err := rows.Err(); err != nil {
		return &catalogStageFailure{kind: "iterate", err: err}
	}
	return nil
}

// attachPartitionParent 记录本表所属的父分区,并保留它自身可能已经成立的
// "本表也是分区父表"事实。
//
// 多级分区里的中间层表既是分区又是父表,分区父表阶段已经为它写入
// Partitioned/Method/Expression。此前这里整体覆盖成 {Partitioned:false},
// 中间层表被记成非分区、丢掉分区方法,ChildObjects 还会随子表名的字典序
// 决定是否幸存 —— 同一套 schema 可以哈希出两份不同的规范证据,直接违背
// 证据的确定性承诺(审查修正)。
func attachPartitionParent(table *TableEvidence, parentRef, bound string) {
	if table == nil {
		return
	}
	if table.Partition == nil {
		table.Partition = &Partition{ChildObjects: []string{}}
	}
	table.Partition.ParentObject = parentRef
	table.Partition.Bound = bound
}

// appendPartitionChild 把子分区登记到父表;父表可能尚未被分区父表阶段命中,
// 此时按分区父表建立,方法留空由该阶段补齐。
func appendPartitionChild(table *TableEvidence, childRef string) {
	if table == nil {
		return
	}
	if table.Partition == nil {
		table.Partition = &Partition{Partitioned: true, ChildObjects: []string{}}
	}
	table.Partition.Partitioned = true
	table.Partition.ChildObjects = append(table.Partition.ChildObjects, childRef)
}
