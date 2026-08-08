package dbevidence

import "testing"

func TestDriftLifecycleAndComponentSummary(t *testing.T) {
	manifest := fixtureManifest()
	users := fixtureTable("users")
	orders := fixtureTable("orders")
	accepted, _, err := BuildSnapshot(manifest, []TableEvidence{users, orders})
	if err != nil {
		t.Fatal(err)
	}
	baseline := baselineFromSnapshot(accepted)
	unchanged := CompareSnapshot(accepted, baseline, true)
	if unchanged.Summary.Unchanged != 2 || unchanged.Summary.New+unchanged.Summary.Changed+unchanged.Summary.Removed != 0 {
		t.Fatalf("bad unchanged report: %+v", unchanged)
	}
	changedUsers := users
	changedUsers.Columns = append([]Column{}, users.Columns...)
	changedUsers.Columns[1].Nullable = true
	products := fixtureTable("products")
	current, _, err := BuildSnapshot(manifest, []TableEvidence{changedUsers, products})
	if err != nil {
		t.Fatal(err)
	}
	report := CompareSnapshot(current, baseline, true)
	if report.Summary.Changed != 1 || report.Summary.New != 1 || report.Summary.Removed != 1 {
		t.Fatalf("bad drift summary: %+v", report.Summary)
	}
	for _, item := range report.Items {
		if item.ObjectRef == users.ObjectRef && (len(item.ChangedComponents) != 1 || item.ChangedComponents[0] != "columns") {
			t.Fatalf("bad component summary: %+v", item)
		}
	}
}

func TestVerifyComparisonDoesNotAdvanceBaseline(t *testing.T) {
	snapshot, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	baseline := baselineFromSnapshot(snapshot)
	before, _ := CanonicalJSON(baseline)
	_ = CompareSnapshot(snapshot, baseline, true)
	after, _ := CanonicalJSON(baseline)
	if string(before) != string(after) {
		t.Fatal("drift verification mutated the evidence baseline")
	}
}

func TestEmptyUnbaselinedSourceAndSourceIdentityChangeRemainDrift(t *testing.T) {
	empty, _, err := BuildSnapshot(fixtureManifest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	otherBaseline := Baseline{Version: BaselineVersion, Sources: []BaselineSource{{SourceID: "other"}}}
	report := CompareSnapshot(empty, otherBaseline, true)
	if report.BaselinePresent {
		t.Fatal("a Baseline for another source accepted an empty current source")
	}

	accepted := baselineFromSnapshot(empty)
	changed := empty
	caseMode := 1
	changed.CaseSemantics = CaseSemantics{IdentifierCase: "server_lower_case_table_names", LowerCaseTableNames: &caseMode}
	identity, err := snapshotIdentityBytes(changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.SourceSnapshotSHA256 = sha256Hex(identity)
	report = CompareSnapshot(changed, accepted, true)
	if !report.BaselinePresent || !report.SourceIdentityChanged {
		t.Fatalf("source identity drift was suppressed: %+v", report)
	}
}

func TestDriftNamesEveryStructuralComponent(t *testing.T) {
	tests := []struct {
		name      string
		component string
		mutate    func(*TableEvidence)
	}{
		{"column", "columns", func(table *TableEvidence) { table.Columns[1].Nullable = true }},
		{"primary key", "primary_key", func(table *TableEvidence) { table.PrimaryKey.Name = "renamed_pk" }},
		{"unique", "unique_constraints", func(table *TableEvidence) { table.UniqueConstraints[0].Name = "renamed_unique" }},
		{"foreign key", "foreign_keys", func(table *TableEvidence) { table.ForeignKeys[0].DeleteAction = "cascade" }},
		{"check", "checks", func(table *TableEvidence) { table.Checks[0].Expression = "CHECK (id >= 0)" }},
		{"index", "indexes", func(table *TableEvidence) { table.Indexes[0].Unique = true }},
		{"partition", "partition", func(table *TableEvidence) { table.Partition.Expression = "RANGE (id, email)" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := componentRichTable()
			accepted, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{base})
			if err != nil {
				t.Fatal(err)
			}
			currentTable := componentRichTable()
			test.mutate(&currentTable)
			current, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{currentTable})
			if err != nil {
				t.Fatal(err)
			}
			report := CompareSnapshot(current, baselineFromSnapshot(accepted), true)
			if report.Summary.Changed != 1 || len(report.Items) != 1 || len(report.Items[0].ChangedComponents) != 1 || report.Items[0].ChangedComponents[0] != test.component {
				t.Fatalf("bad component drift: %+v", report)
			}
		})
	}
}

func TestColumnShapeDriftMatrix(t *testing.T) {
	defaultValue := "0"
	tests := []struct {
		name   string
		mutate func(*TableEvidence)
	}{
		{"add", func(table *TableEvidence) {
			table.Columns = append(table.Columns, Column{Ordinal: 4, Name: "added", NativeType: "integer", CanonicalType: "integer"})
		}},
		{"remove", func(table *TableEvidence) { table.Columns = table.Columns[:2] }},
		{"rename", func(table *TableEvidence) { table.Columns[2].Name = "renamed" }},
		{"native type", func(table *TableEvidence) { table.Columns[2].NativeType = "character varying(200)" }},
		{"canonical type", func(table *TableEvidence) { table.Columns[2].CanonicalType = "character varying" }},
		{"nullability", func(table *TableEvidence) { table.Columns[2].Nullable = false }},
		{"default", func(table *TableEvidence) { table.Columns[2].DefaultExpression = &defaultValue }},
	}
	accepted, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentTable := fixtureTable("users")
			test.mutate(&currentTable)
			current, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{currentTable})
			if err != nil {
				t.Fatal(err)
			}
			report := CompareSnapshot(current, baselineFromSnapshot(accepted), true)
			if report.Summary.Changed != 1 || len(report.Items[0].ChangedComponents) != 1 || report.Items[0].ChangedComponents[0] != "columns" {
				t.Fatalf("bad %s drift: %+v", test.name, report)
			}
		})
	}
}

func componentRichTable() TableEvidence {
	table := fixtureTable("users")
	table.UniqueConstraints = []KeyConstraint{{Name: "users_email_key", Columns: []string{"email"}}}
	ref, _ := CanonicalObjectRef("primary", "public", "accounts")
	table.ForeignKeys = []ForeignKey{{Name: "users_account_fk", Columns: []string{"id"}, ReferencedObject: ref, ReferencedColumns: []string{"id"}, UpdateAction: "no action", DeleteAction: "restrict"}}
	table.Checks = []CheckConstraint{{Name: "users_id_check", Expression: "CHECK (id > 0)"}}
	table.Partition = &Partition{Partitioned: true, Method: "range", Expression: "RANGE (id)", ChildObjects: []string{}}
	return table
}

func baselineFromSnapshot(snapshot Snapshot) Baseline {
	tables := make([]BaselineTable, len(snapshot.Tables))
	for index, table := range snapshot.Tables {
		tables[index] = BaselineTable{ObjectRef: table.ObjectRef, TableEvidenceSHA256: table.TableEvidenceSHA256, ComponentHashes: table.ComponentHashes}
	}
	return Baseline{Version: BaselineVersion, Sources: []BaselineSource{{
		SourceID: snapshot.SourceID, Engine: snapshot.Engine, Database: snapshot.Database,
		Namespaces: snapshot.Namespaces, CaseSemantics: snapshot.CaseSemantics,
		IncludeNamespaces: snapshot.IncludeNamespaces, ExcludeNamespaces: snapshot.ExcludeNamespaces,
		IncludeTables: snapshot.IncludeTables, ExcludeTables: snapshot.ExcludeTables,
		EvidenceVersion: snapshot.EvidenceVersion, SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256, Tables: tables,
	}}}
}
