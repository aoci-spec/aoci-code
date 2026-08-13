package dbevidence

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const supportedOpenGaussVersion = "6.0.5"

var catalogVectorIntegerPattern = regexp.MustCompile(`-?[0-9]+`)

func collectOpenGauss(ctx context.Context, tx *sql.Tx, source SourceConfig, inspectOnly bool) (string, []TableEvidence, error) {
	var productVersion, serverVersion, deployment, compatibility, database string
	if err := queryRow(ctx, tx, openGaussFactsSQL).Scan(&productVersion, &serverVersion, &deployment, &compatibility, &database); err != nil {
		return "", nil, wrapCatalogQuery("facts", err)
	}
	productVersion = normalizeCatalogText(productVersion)
	deployment = normalizeCatalogText(deployment)
	compatibility = strings.ToUpper(normalizeCatalogText(compatibility))
	if productVersion != supportedOpenGaussVersion {
		return "", nil, wrapCatalogQuery("facts", &unsupportedCatalogFeature{feature: "opengauss_version"})
	}
	if compatibility != "A" && compatibility != "PG" {
		return "", nil, wrapCatalogQuery("facts", &unsupportedCatalogFeature{feature: "compatibility_mode"})
	}
	if deployment != "OpenSourceCentralized" {
		return "", nil, wrapCatalogQuery("facts", &unsupportedCatalogFeature{feature: "deployment_mode"})
	}
	if database != source.Database {
		return "", nil, wrapCatalogQuery("facts", &unsupportedCatalogFeature{feature: "database_identity"})
	}
	if err := openGaussRejectUnsupported(ctx, tx, source); err != nil {
		return "", nil, wrapCatalogQuery("unsupported", err)
	}
	tables, err := openGaussTables(ctx, tx, source)
	err = wrapCatalogQuery("tables", err)
	if err != nil || inspectOnly {
		return openGaussServerAuditVersion(productVersion, serverVersion, deployment, compatibility), sortedTableValues(tables), err
	}
	for _, loader := range []struct {
		name string
		load func(context.Context, *sql.Tx, SourceConfig, map[string]*TableEvidence) error
	}{
		{"columns", openGaussColumns}, {"keys", openGaussKeys},
		{"foreign_keys", openGaussForeignKeys}, {"checks", openGaussChecks},
		{"indexes", openGaussIndexes},
	} {
		if err := loader.load(ctx, tx, source, tables); err != nil {
			return "", nil, wrapCatalogQuery(loader.name, err)
		}
	}
	return openGaussServerAuditVersion(productVersion, serverVersion, deployment, compatibility), sortedTableValues(tables), nil
}

func openGaussServerAuditVersion(productVersion, serverVersion, deployment, compatibility string) string {
	return productVersion + " (server " + normalizeCatalogText(serverVersion) + "; deployment " + deployment + "; compatibility " + compatibility + ")"
}

func openGaussRejectUnsupported(ctx context.Context, tx *sql.Tx, source SourceConfig) error {
	rows, err := queryRows(ctx, tx, openGaussUnsupportedSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, feature string
		if err := rows.Scan(&namespace, &tableName, &feature); err != nil {
			return err
		}
		if Included(source, namespace, tableName) {
			return &unsupportedCatalogFeature{feature: feature}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	partitionRows, err := queryRows(ctx, tx, openGaussPartitionsSQL)
	if err != nil {
		return err
	}
	defer partitionRows.Close()
	for partitionRows.Next() {
		var namespace, tableName, partType, strategy string
		if err := partitionRows.Scan(&namespace, &tableName, &partType, &strategy); err != nil {
			return err
		}
		if Included(source, namespace, tableName) {
			feature := "pg_partition"
			if partType == "s" {
				feature = "subpartition"
			}
			return &unsupportedCatalogFeature{feature: feature}
		}
	}
	return partitionRows.Err()
}

func openGaussTables(ctx context.Context, tx *sql.Tx, source SourceConfig) (map[string]*TableEvidence, error) {
	rows, err := queryRows(ctx, tx, openGaussTablesSQL)
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

func openGaussColumns(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, openGaussColumnsSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, tableName, columnName, nativeType, canonicalType, generated string
		var ordinal int
		var nullable, serial bool
		var defaultExpression, generationExpression sql.NullString
		if err := rows.Scan(&namespace, &tableName, &ordinal, &columnName, &nativeType, &canonicalType,
			&nullable, &defaultExpression, &serial, &generated, &generationExpression); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		if generated == "NEVER" {
			generated = ""
		}
		table.Columns = append(table.Columns, Column{
			Ordinal: ordinal, Name: columnName, NativeType: nativeType, CanonicalType: canonicalType,
			Nullable: nullable, DefaultExpression: nullableString(defaultExpression), Serial: serial,
			Generated: generated, GenerationExpression: generationExpression.String,
		})
	}
	return rows.Err()
}

type openGaussKeyState struct {
	table   *TableEvidence
	kind    string
	key     KeyConstraint
	columns map[int]string
}

func openGaussKeys(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, openGaussKeysSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	states := map[string]*openGaussKeyState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, constraintName, kind, columnName, vector string
		if err := rows.Scan(&namespace, &tableName, &constraintName, &kind, &columnName, &vector); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		ordinals, err := catalogVectorOrdinals(vector)
		if err != nil {
			return err
		}
		ordinal, exists := ordinalForColumn(table, columnName)
		if !exists {
			return fmt.Errorf("constraint %s references unknown column %s", constraintName, columnName)
		}
		position, exists := positionInCatalogVector(ordinals, ordinal)
		if !exists {
			return fmt.Errorf("constraint %s has inconsistent catalog vector", constraintName)
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + constraintName
		state := states[stateKey]
		if state == nil {
			state = &openGaussKeyState{table: table, kind: kind, key: KeyConstraint{Name: constraintName}, columns: map[int]string{}}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		state.columns[position] = columnName
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

type openGaussFKState struct {
	table             *TableEvidence
	fk                ForeignKey
	columns           map[int]string
	referencedColumns map[int]string
}

func openGaussForeignKeys(ctx context.Context, tx *sql.Tx, source SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, openGaussForeignKeysSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	states := map[string]*openGaussFKState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, name, column, refNamespace, refTable, refColumn, updateAction, deleteAction, localVector, refVector string
		var localAttnum, refAttnum int
		if err := rows.Scan(&namespace, &tableName, &name, &localAttnum, &column, &refNamespace, &refTable, &refAttnum, &refColumn,
			&updateAction, &deleteAction, &localVector, &refVector); err != nil {
			return err
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		localOrdinal, exists := ordinalForColumn(table, column)
		if !exists || localOrdinal != localAttnum {
			return fmt.Errorf("foreign key %s references unknown local column %s", name, column)
		}
		localOrdinals, err := catalogVectorOrdinals(localVector)
		if err != nil {
			return err
		}
		position, exists := positionInCatalogVector(localOrdinals, localOrdinal)
		if !exists {
			return fmt.Errorf("foreign key %s has inconsistent local catalog vector", name)
		}
		refOrdinals, err := catalogVectorOrdinals(refVector)
		if err != nil {
			return fmt.Errorf("foreign key %s has inconsistent referenced catalog vector", name)
		}
		refPosition, exists := positionInCatalogVector(refOrdinals, refAttnum)
		if !exists {
			return fmt.Errorf("foreign key %s has inconsistent referenced catalog vector", name)
		}
		// The catalog joins local and referenced attributes independently. Keep
		// only the pair occupying the same constraint-vector position.
		if refPosition != position {
			continue
		}
		ref, err := CanonicalObjectRef(source.SourceID, refNamespace, refTable)
		if err != nil {
			return err
		}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			state = &openGaussFKState{
				table: table, fk: ForeignKey{Name: name, ReferencedObject: ref, UpdateAction: updateAction, DeleteAction: deleteAction},
				columns: map[int]string{}, referencedColumns: map[int]string{},
			}
			states[stateKey] = state
			order = append(order, stateKey)
		}
		state.columns[position] = column
		state.referencedColumns[position] = refColumn
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, stateKey := range order {
		state := states[stateKey]
		state.fk.Columns = orderedColumns(state.columns)
		state.fk.ReferencedColumns = orderedColumns(state.referencedColumns)
		state.table.ForeignKeys = append(state.table.ForeignKeys, state.fk)
	}
	return nil
}

func openGaussChecks(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, openGaussChecksSQL)
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

type openGaussIndexState struct {
	table *TableEvidence
	index Index
}

func openGaussIndexes(ctx context.Context, tx *sql.Tx, _ SourceConfig, tables map[string]*TableEvidence) error {
	rows, err := queryRows(ctx, tx, openGaussIndexesSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	states := map[string]*openGaussIndexState{}
	order := []string{}
	for rows.Next() {
		var namespace, tableName, name, method, vector, definition, optionVector string
		var column, predicate sql.NullString
		var unique, primary, visible, usable, valid, ready, exclusion, immediate bool
		var keyCount, totalCount int
		if err := rows.Scan(&namespace, &tableName, &name, &unique, &primary, &method, &column,
			&vector, &definition, &predicate, &keyCount, &totalCount, &optionVector, &visible,
			&usable, &valid, &ready, &exclusion, &immediate); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		table := tables[tableKey(namespace, tableName)]
		if table == nil {
			continue
		}
		if keyCount != totalCount {
			return &unsupportedCatalogFeature{feature: "included_index_columns"}
		}
		if !usable || !valid || !ready || exclusion || !immediate {
			return &unsupportedCatalogFeature{feature: "index_state"}
		}
		if strings.Contains(strings.ToUpper(definition), " LOCAL") || strings.Contains(strings.ToUpper(definition), " GLOBAL") {
			return &unsupportedCatalogFeature{feature: "partition_index"}
		}
		if strings.Contains(strings.ToUpper(definition), " NULLS ") {
			return &unsupportedCatalogFeature{feature: "index_null_order"}
		}
		ordinals, err := catalogVectorOrdinals(vector)
		if err != nil {
			return err
		}
		for _, ordinal := range ordinals {
			if ordinal == 0 {
				return &unsupportedCatalogFeature{feature: "expression_index"}
			}
		}
		options, err := catalogVectorOrdinals(optionVector)
		if err != nil || len(options) != len(ordinals) {
			return fmt.Errorf("index %s has inconsistent option vector", name)
		}
		for _, option := range options {
			// openGauss stores ASC NULLS LAST as 0 and DESC NULLS FIRST as
			// DESC|NULLS_FIRST (3). The opposite NULL placement cannot be
			// represented by Evidence v1, even when pg_get_indexdef omits the
			// spelling that produced the catalog bits.
			if option != 0 && option != 3 {
				return &unsupportedCatalogFeature{feature: "index_null_order"}
			}
		}
		if !column.Valid {
			return fmt.Errorf("index %s references an unknown column", name)
		}
		ordinal, exists := ordinalForColumn(table, column.String)
		if !exists {
			return fmt.Errorf("index %s references unknown column %s", name, column.String)
		}
		position, exists := positionInCatalogVector(ordinals, ordinal)
		if !exists {
			return fmt.Errorf("index %s has inconsistent catalog vector", name)
		}
		columnCopy := column.String
		element := IndexElement{Position: position, Column: &columnCopy, Descending: options[position-1]&1 != 0}
		stateKey := tableKey(namespace, tableName) + "\x00" + name
		state := states[stateKey]
		if state == nil {
			visibleCopy := visible
			state = &openGaussIndexState{table: table, index: Index{Name: name, Unique: unique, Primary: primary, Visible: &visibleCopy, Method: method, Predicate: predicate.String, Elements: []IndexElement{}}}
			states[stateKey] = state
			order = append(order, stateKey)
		} else if state.index.Unique != unique || state.index.Primary != primary || state.index.Method != method || state.index.Predicate != predicate.String || state.index.Visible == nil || *state.index.Visible != visible {
			return fmt.Errorf("index %s has inconsistent catalog rows", name)
		}
		state.index.Elements = append(state.index.Elements, element)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, stateKey := range order {
		state := states[stateKey]
		sort.Slice(state.index.Elements, func(i, j int) bool { return state.index.Elements[i].Position < state.index.Elements[j].Position })
		state.table.Indexes = append(state.table.Indexes, state.index)
	}
	return nil
}

func catalogVectorOrdinals(value string) ([]int, error) {
	matches := catalogVectorIntegerPattern.FindAllString(value, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("catalog vector is empty")
	}
	result := make([]int, len(matches))
	for index, match := range matches {
		ordinal, err := strconv.Atoi(match)
		if err != nil {
			return nil, fmt.Errorf("catalog vector is invalid")
		}
		result[index] = ordinal
	}
	return result, nil
}

func ordinalForColumn(table *TableEvidence, column string) (int, bool) {
	for _, candidate := range table.Columns {
		if candidate.Name == column {
			return candidate.Ordinal, true
		}
	}
	return 0, false
}

func positionInCatalogVector(vector []int, ordinal int) (int, bool) {
	for index, candidate := range vector {
		if candidate == ordinal {
			return index + 1, true
		}
	}
	return 0, false
}
