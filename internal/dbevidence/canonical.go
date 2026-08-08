package dbevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

func CanonicalObjectRef(sourceID, namespace, table string) (string, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return "", fmt.Errorf("invalid source_id")
	}
	if err := validateIdentifier("namespace", namespace); err != nil {
		return "", err
	}
	if err := validateIdentifier("table", table); err != nil {
		return "", err
	}
	return "database://" + sourceID + "/" + url.PathEscape(namespace) + "/" + url.PathEscape(table), nil
}

func CanonicalTable(table TableEvidence) (TableEvidence, []byte, string, ComponentHashes, error) {
	if !allStringsUTF8(reflect.ValueOf(table)) {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("table evidence contains invalid UTF-8")
	}
	if table.Version == "" {
		table.Version = EvidenceVersion
	}
	if table.Version != EvidenceVersion {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("unsupported evidence version %q", table.Version)
	}
	wantRef, err := CanonicalObjectRef(table.SourceID, table.Namespace, table.Name)
	if err != nil {
		return TableEvidence{}, nil, "", ComponentHashes{}, err
	}
	if table.ObjectRef == "" {
		table.ObjectRef = wantRef
	}
	if table.ObjectRef != wantRef {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("object_ref %q does not match canonical identity %q", table.ObjectRef, wantRef)
	}
	if table.Engine != EnginePostgreSQL && table.Engine != EngineMySQL {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("unsupported engine %q", table.Engine)
	}
	if table.Kind != "base_table" {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("database evidence v1 only accepts base_table, got %q", table.Kind)
	}
	if len(table.Columns) == 0 {
		return TableEvidence{}, nil, "", ComponentHashes{}, fmt.Errorf("table %s has no columns", table.ObjectRef)
	}
	if err := canonicalizeTable(&table); err != nil {
		return TableEvidence{}, nil, "", ComponentHashes{}, err
	}
	data, err := marshalCanonical(table)
	if err != nil {
		return TableEvidence{}, nil, "", ComponentHashes{}, err
	}
	hashes, err := componentHashes(table)
	if err != nil {
		return TableEvidence{}, nil, "", ComponentHashes{}, err
	}
	return table, data, sha256Hex(data), hashes, nil
}

func allStringsUTF8(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		return value.IsNil() || allStringsUTF8(value.Elem())
	case reflect.String:
		return utf8.ValidString(value.String())
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if !allStringsUTF8(value.Field(index)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if !allStringsUTF8(value.Index(index)) {
				return false
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if !allStringsUTF8(iterator.Key()) || !allStringsUTF8(iterator.Value()) {
				return false
			}
		}
	}
	return true
}

func canonicalizeTable(table *TableEvidence) error {
	for index := range table.Columns {
		column := &table.Columns[index]
		if column.Ordinal < 1 {
			return fmt.Errorf("table %s has invalid column ordinal %d", table.ObjectRef, column.Ordinal)
		}
		if err := validateIdentifier("column", column.Name); err != nil {
			return err
		}
		column.NativeType = normalizeCatalogText(column.NativeType)
		column.CanonicalType = strings.ToLower(normalizeCatalogText(column.CanonicalType))
		if column.NativeType == "" || column.CanonicalType == "" {
			return fmt.Errorf("column %s has an empty type", column.Name)
		}
		if column.DefaultExpression != nil {
			normalized := normalizeCatalogText(*column.DefaultExpression)
			column.DefaultExpression = &normalized
		}
		column.Identity = normalizeCatalogText(column.Identity)
		column.Generated = normalizeCatalogText(column.Generated)
		column.GenerationExpression = normalizeCatalogText(column.GenerationExpression)
	}
	sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].Ordinal < table.Columns[j].Ordinal })
	seenOrdinals := map[int]struct{}{}
	seenColumns := map[string]struct{}{}
	for _, column := range table.Columns {
		if _, exists := seenOrdinals[column.Ordinal]; exists {
			return fmt.Errorf("table %s has duplicate column ordinal %d", table.ObjectRef, column.Ordinal)
		}
		if _, exists := seenColumns[column.Name]; exists {
			return fmt.Errorf("table %s has duplicate column name %q", table.ObjectRef, column.Name)
		}
		seenOrdinals[column.Ordinal] = struct{}{}
		seenColumns[column.Name] = struct{}{}
	}
	if table.PrimaryKey != nil {
		if err := canonicalizeKey(table.PrimaryKey, seenColumns); err != nil {
			return fmt.Errorf("primary key: %w", err)
		}
	}
	for index := range table.UniqueConstraints {
		if err := canonicalizeKey(&table.UniqueConstraints[index], seenColumns); err != nil {
			return fmt.Errorf("unique constraint: %w", err)
		}
	}
	sort.Slice(table.UniqueConstraints, func(i, j int) bool { return table.UniqueConstraints[i].Name < table.UniqueConstraints[j].Name })
	if duplicateNamedKey(table.UniqueConstraints) {
		return fmt.Errorf("table %s has duplicate unique constraint names", table.ObjectRef)
	}
	for index := range table.ForeignKeys {
		foreign := &table.ForeignKeys[index]
		foreign.Name = normalizeCatalogText(foreign.Name)
		foreign.UpdateAction = strings.ToLower(normalizeCatalogText(foreign.UpdateAction))
		foreign.DeleteAction = strings.ToLower(normalizeCatalogText(foreign.DeleteAction))
		if foreign.Name == "" || !canonicalReferenceForSource(foreign.ReferencedObject, table.SourceID) || len(foreign.Columns) == 0 || len(foreign.Columns) != len(foreign.ReferencedColumns) || !validReferentialAction(foreign.UpdateAction) || !validReferentialAction(foreign.DeleteAction) {
			return fmt.Errorf("foreign key %q is incomplete", foreign.Name)
		}
		for _, column := range foreign.Columns {
			if _, exists := seenColumns[column]; !exists {
				return fmt.Errorf("foreign key %q references unknown local column %q", foreign.Name, column)
			}
		}
	}
	sort.Slice(table.ForeignKeys, func(i, j int) bool { return table.ForeignKeys[i].Name < table.ForeignKeys[j].Name })
	if duplicateForeignKeyName(table.ForeignKeys) {
		return fmt.Errorf("table %s has duplicate foreign key names", table.ObjectRef)
	}
	for index := range table.Checks {
		table.Checks[index].Name = normalizeCatalogText(table.Checks[index].Name)
		table.Checks[index].Expression = normalizeCatalogText(table.Checks[index].Expression)
		if table.Checks[index].Name == "" || table.Checks[index].Expression == "" {
			return fmt.Errorf("check constraint is incomplete")
		}
	}
	sort.Slice(table.Checks, func(i, j int) bool { return table.Checks[i].Name < table.Checks[j].Name })
	if duplicateCheckName(table.Checks) {
		return fmt.Errorf("table %s has duplicate check constraint names", table.ObjectRef)
	}
	for index := range table.Indexes {
		idx := &table.Indexes[index]
		idx.Name = normalizeCatalogText(idx.Name)
		idx.Method = strings.ToLower(normalizeCatalogText(idx.Method))
		idx.Predicate = normalizeCatalogText(idx.Predicate)
		if idx.Name == "" || len(idx.Elements) == 0 {
			return fmt.Errorf("index %q is incomplete", idx.Name)
		}
		sort.Slice(idx.Elements, func(i, j int) bool { return idx.Elements[i].Position < idx.Elements[j].Position })
		for elementIndex := range idx.Elements {
			element := &idx.Elements[elementIndex]
			if element.Position < 1 || (elementIndex > 0 && idx.Elements[elementIndex-1].Position == element.Position) || (element.Column == nil) == (element.Expression == nil) || (element.PrefixLength != nil && *element.PrefixLength < 1) {
				return fmt.Errorf("index %q has invalid element %d", idx.Name, element.Position)
			}
			if element.Column != nil {
				column := normalizeCatalogText(*element.Column)
				element.Column = &column
			}
			if element.Expression != nil {
				expression := normalizeCatalogText(*element.Expression)
				element.Expression = &expression
			}
		}
	}
	sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].Name < table.Indexes[j].Name })
	if duplicateIndexName(table.Indexes) {
		return fmt.Errorf("table %s has duplicate index names", table.ObjectRef)
	}
	if table.Partition != nil {
		table.Partition.Method = strings.ToLower(normalizeCatalogText(table.Partition.Method))
		table.Partition.Expression = normalizeCatalogText(table.Partition.Expression)
		table.Partition.Bound = normalizeCatalogText(table.Partition.Bound)
		sort.Strings(table.Partition.ChildObjects)
		for index, child := range table.Partition.ChildObjects {
			if !canonicalReferenceForSource(child, table.SourceID) || (index > 0 && table.Partition.ChildObjects[index-1] == child) {
				return fmt.Errorf("table %s has invalid partition child identity", table.ObjectRef)
			}
		}
		for index := range table.Partition.Definitions {
			table.Partition.Definitions[index].Name = normalizeCatalogText(table.Partition.Definitions[index].Name)
			table.Partition.Definitions[index].Description = normalizeCatalogText(table.Partition.Definitions[index].Description)
		}
		sort.Slice(table.Partition.Definitions, func(i, j int) bool {
			return table.Partition.Definitions[i].Ordinal < table.Partition.Definitions[j].Ordinal
		})
		seenDefinitionNames := map[string]struct{}{}
		for index, definition := range table.Partition.Definitions {
			if definition.Name == "" || definition.Ordinal < 1 || (index > 0 && table.Partition.Definitions[index-1].Ordinal == definition.Ordinal) {
				return fmt.Errorf("table %s has invalid partition definition", table.ObjectRef)
			}
			if _, exists := seenDefinitionNames[definition.Name]; exists {
				return fmt.Errorf("table %s has duplicate partition definition names", table.ObjectRef)
			}
			seenDefinitionNames[definition.Name] = struct{}{}
		}
		if table.Partition.Partitioned {
			if table.Partition.Method == "" {
				return fmt.Errorf("table %s has incomplete partition method", table.ObjectRef)
			}
		} else if !canonicalReferenceForSource(table.Partition.ParentObject, table.SourceID) || table.Partition.Bound == "" {
			return fmt.Errorf("table %s has incomplete partition parent evidence", table.ObjectRef)
		}
	}
	if table.UniqueConstraints == nil {
		table.UniqueConstraints = []KeyConstraint{}
	}
	if table.ForeignKeys == nil {
		table.ForeignKeys = []ForeignKey{}
	}
	if table.Checks == nil {
		table.Checks = []CheckConstraint{}
	}
	if table.Indexes == nil {
		table.Indexes = []Index{}
	}
	return nil
}

func duplicateNamedKey(values []KeyConstraint) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1].Name == values[index].Name {
			return true
		}
	}
	return false
}

func duplicateForeignKeyName(values []ForeignKey) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1].Name == values[index].Name {
			return true
		}
	}
	return false
}

func duplicateCheckName(values []CheckConstraint) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1].Name == values[index].Name {
			return true
		}
	}
	return false
}

func duplicateIndexName(values []Index) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1].Name == values[index].Name {
			return true
		}
	}
	return false
}

func validReferentialAction(value string) bool {
	switch value {
	case "no action", "restrict", "cascade", "set null", "set default":
		return true
	default:
		return false
	}
}

func canonicalReferenceForSource(value, sourceID string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "database" || parsed.Host != sourceID || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(segments) != 2 {
		return false
	}
	namespace, err := url.PathUnescape(segments[0])
	if err != nil {
		return false
	}
	table, err := url.PathUnescape(segments[1])
	if err != nil {
		return false
	}
	want, err := CanonicalObjectRef(sourceID, namespace, table)
	return err == nil && want == value
}

func canonicalizeKey(key *KeyConstraint, columns map[string]struct{}) error {
	key.Name = normalizeCatalogText(key.Name)
	if key.Name == "" || len(key.Columns) == 0 {
		return fmt.Errorf("constraint is incomplete")
	}
	seen := map[string]struct{}{}
	for _, column := range key.Columns {
		if _, exists := columns[column]; !exists {
			return fmt.Errorf("constraint %q references unknown column %q", key.Name, column)
		}
		if _, exists := seen[column]; exists {
			return fmt.Errorf("constraint %q repeats column %q", key.Name, column)
		}
		seen[column] = struct{}{}
	}
	return nil
}

func componentHashes(table TableEvidence) (ComponentHashes, error) {
	values := []any{table.Columns, table.PrimaryKey, table.UniqueConstraints, table.ForeignKeys, table.Checks, table.Indexes, table.Partition}
	hashes := make([]string, len(values))
	for index, value := range values {
		data, err := marshalCanonical(value)
		if err != nil {
			return ComponentHashes{}, err
		}
		hashes[index] = sha256Hex(data)
	}
	return ComponentHashes{
		Columns: hashes[0], PrimaryKey: hashes[1], UniqueConstraints: hashes[2],
		ForeignKeys: hashes[3], Checks: hashes[4], Indexes: hashes[5], Partition: hashes[6],
	}, nil
}

func BuildSnapshot(manifest SourceManifest, tables []TableEvidence) (Snapshot, map[string][]byte, error) {
	manifest.Namespaces = sortedCopy(manifest.Namespaces)
	records := make([]SnapshotTable, 0, len(tables))
	files := make(map[string][]byte, len(tables))
	seen := map[string]struct{}{}
	for _, table := range tables {
		canonical, data, digest, components, err := CanonicalTable(table)
		if err != nil {
			return Snapshot{}, nil, err
		}
		if canonical.SourceID != manifest.SourceID || canonical.Engine != manifest.Engine || canonical.Database != manifest.Database {
			return Snapshot{}, nil, fmt.Errorf("table %s does not belong to source manifest", canonical.ObjectRef)
		}
		if _, exists := seen[canonical.ObjectRef]; exists {
			return Snapshot{}, nil, fmt.Errorf("duplicate table identity %s", canonical.ObjectRef)
		}
		seen[canonical.ObjectRef] = struct{}{}
		ref := manifest.SourceID + "/tables/" + digest + ".json"
		records = append(records, SnapshotTable{ObjectRef: canonical.ObjectRef, TableEvidenceSHA256: digest, ComponentHashes: components, EvidenceRef: ref})
		files[canonical.ObjectRef] = data
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ObjectRef < records[j].ObjectRef })
	state := "present"
	if len(records) == 0 {
		state = "present_empty"
	}
	snapshot := Snapshot{
		Version: SnapshotVersion, EvidenceVersion: EvidenceVersion, SourceID: manifest.SourceID,
		Engine: manifest.Engine, Database: manifest.Database, Namespaces: manifest.Namespaces,
		IncludeNamespaces: sortedCopy(manifest.IncludeNamespaces), ExcludeNamespaces: sortedCopy(manifest.ExcludeNamespaces),
		IncludeTables: sortedCopy(manifest.IncludeTables), ExcludeTables: sortedCopy(manifest.ExcludeTables),
		CaseSemantics: manifest.CaseSemantics, State: state, Tables: records, BusinessDataRead: false,
	}
	identityBytes, err := snapshotIdentityBytes(snapshot)
	if err != nil {
		return Snapshot{}, nil, err
	}
	snapshot.SourceSnapshotSHA256 = sha256Hex(identityBytes)
	return snapshot, files, nil
}

func snapshotIdentityBytes(snapshot Snapshot) ([]byte, error) {
	identity := struct {
		Version           string          `json:"version"`
		EvidenceVersion   string          `json:"evidence_version"`
		SourceID          string          `json:"source_id"`
		Engine            Engine          `json:"engine"`
		Database          string          `json:"database"`
		Namespaces        []string        `json:"namespaces"`
		IncludeNamespaces []string        `json:"include_namespaces"`
		ExcludeNamespaces []string        `json:"exclude_namespaces"`
		IncludeTables     []string        `json:"include_tables"`
		ExcludeTables     []string        `json:"exclude_tables"`
		CaseSemantics     CaseSemantics   `json:"case_semantics"`
		State             string          `json:"state"`
		Tables            []SnapshotTable `json:"tables"`
	}{snapshot.Version, snapshot.EvidenceVersion, snapshot.SourceID, snapshot.Engine, snapshot.Database, snapshot.Namespaces,
		snapshot.IncludeNamespaces, snapshot.ExcludeNamespaces, snapshot.IncludeTables, snapshot.ExcludeTables,
		snapshot.CaseSemantics, snapshot.State, snapshot.Tables}
	return marshalCanonical(identity)
}

func CanonicalJSON(value any) ([]byte, error) {
	return marshalCanonical(value)
}

func marshalCanonical(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func normalizeCatalogText(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func sortedCopy(values []string) []string {
	copyValues := append([]string{}, values...)
	sort.Strings(copyValues)
	if copyValues == nil {
		copyValues = []string{}
	}
	return copyValues
}
