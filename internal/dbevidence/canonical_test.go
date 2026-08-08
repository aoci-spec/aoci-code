package dbevidence

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCanonicalObjectRefPreservesCaseAndEscapesSegments(t *testing.T) {
	ref, err := CanonicalObjectRef("primary", "Sales/北", `Order Items`)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "database://primary/Sales%2F%E5%8C%97/Order%20Items" {
		t.Fatalf("unexpected canonical ref %q", ref)
	}
}

func TestCanonicalTableIsOrderAndLineEndingIndependent(t *testing.T) {
	first := fixtureTable("users")
	first.UniqueConstraints = []KeyConstraint{
		{Name: "users_email_key", Columns: []string{"email"}},
		{Name: "users_name_key", Columns: []string{"name"}},
	}
	first.Checks = []CheckConstraint{{Name: "z_check", Expression: "CHECK (name <> ''\r\n)"}, {Name: "a_check", Expression: "CHECK (id > 0)"}}
	second := first
	second.Columns = append([]Column{}, first.Columns...)
	second.Columns[0], second.Columns[1] = second.Columns[1], second.Columns[0]
	second.UniqueConstraints = []KeyConstraint{first.UniqueConstraints[1], first.UniqueConstraints[0]}
	second.Checks = []CheckConstraint{first.Checks[1], first.Checks[0]}
	_, firstBytes, firstHash, _, err := CanonicalTable(first)
	if err != nil {
		t.Fatal(err)
	}
	_, secondBytes, secondHash, _, err := CanonicalTable(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash || string(firstBytes) != string(secondBytes) {
		t.Fatalf("canonicalization drifted\nfirst=%s\nsecond=%s", firstBytes, secondBytes)
	}
	if strings.Contains(string(firstBytes), "\r") {
		t.Fatal("canonical evidence retained CR bytes")
	}
}

func TestSnapshotOneTableChangeKeepsUnrelatedFingerprint(t *testing.T) {
	manifest := fixtureManifest()
	users := fixtureTable("users")
	orders := fixtureTable("orders")
	first, _, err := BuildSnapshot(manifest, []TableEvidence{orders, users})
	if err != nil {
		t.Fatal(err)
	}
	changedUsers := users
	changedUsers.Columns = append([]Column{}, users.Columns...)
	changedUsers.Columns[1].Nullable = !changedUsers.Columns[1].Nullable
	second, _, err := BuildSnapshot(manifest, []TableEvidence{changedUsers, orders})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceSnapshotSHA256 == second.SourceSnapshotSHA256 {
		t.Fatal("source snapshot did not change")
	}
	firstHashes := snapshotHashes(first)
	secondHashes := snapshotHashes(second)
	if firstHashes[orders.ObjectRef] != secondHashes[orders.ObjectRef] {
		t.Fatal("unrelated table fingerprint changed")
	}
	if firstHashes[users.ObjectRef] == secondHashes[users.ObjectRef] {
		t.Fatal("changed table fingerprint stayed equal")
	}
}

func TestSnapshotIgnoresServerAuditVersionAndInputOrder(t *testing.T) {
	firstManifest := fixtureManifest()
	firstManifest.ServerVersion = "18.1"
	secondManifest := fixtureManifest()
	secondManifest.ServerVersion = "18.9"
	users := fixtureTable("users")
	orders := fixtureTable("orders")
	first, firstFiles, err := BuildSnapshot(firstManifest, []TableEvidence{users, orders})
	if err != nil {
		t.Fatal(err)
	}
	second, secondFiles, err := BuildSnapshot(secondManifest, []TableEvidence{orders, users})
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := CanonicalJSON(first)
	secondBytes, _ := CanonicalJSON(second)
	if first.SourceSnapshotSHA256 != second.SourceSnapshotSHA256 || string(firstBytes) != string(secondBytes) {
		t.Fatal("server audit version or input order changed the canonical snapshot")
	}
	for objectRef, firstFile := range firstFiles {
		if string(firstFile) != string(secondFiles[objectRef]) {
			t.Fatalf("repeated evidence bytes changed for %s", objectRef)
		}
	}
}

func TestSnapshotBindsSelectionRulesWithoutChangingTableFingerprint(t *testing.T) {
	table := fixtureTable("users")
	first, _, err := BuildSnapshot(fixtureManifest(), []TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	changedManifest := fixtureManifest()
	changedManifest.IncludeTables = []string{"u*"}
	second, _, err := BuildSnapshot(changedManifest, []TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceSnapshotSHA256 == second.SourceSnapshotSHA256 {
		t.Fatal("source selection rule was not bound by the snapshot")
	}
	if first.Tables[0].TableEvidenceSHA256 != second.Tables[0].TableEvidenceSHA256 {
		t.Fatal("source selection rule polluted the table fingerprint")
	}
}

func TestOrderedCatalogColumnsUseOrdinalsNotReturnOrder(t *testing.T) {
	got := orderedColumns(map[int]string{3: "third", 1: "first", 2: "second"})
	if strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("catalog order leaked into evidence: %v", got)
	}
}

func TestEmptySnapshotDiffersFromAbsent(t *testing.T) {
	snapshot, files, err := BuildSnapshot(fixtureManifest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "present_empty" || snapshot.SourceSnapshotSHA256 == "" || len(files) != 0 {
		t.Fatalf("bad empty snapshot: %+v files=%d", snapshot, len(files))
	}
}

func TestCanonicalKnownCrossPlatformSHA(t *testing.T) {
	_, _, digest, _, err := CanonicalTable(fixtureTable("users"))
	if err != nil {
		t.Fatal(err)
	}
	const expected = "a277fcf2ee772c84aae2076bfd46bbfba39f3d140d8d76f487f1e372dd33cfbb"
	if digest != expected {
		t.Fatalf("canonical SHA changed: got %s want %s", digest, expected)
	}
}

func TestCanonicalEvidenceRejectsInvalidUTF8(t *testing.T) {
	table := fixtureTable("users")
	table.Columns[0].NativeType = string([]byte{0xff})
	if _, _, _, _, err := CanonicalTable(table); err == nil {
		t.Fatal("invalid UTF-8 was silently rewritten")
	}
}

func TestCanonicalScaleFixtures(t *testing.T) {
	for _, count := range []int{1, 100, 1000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			tables := make([]TableEvidence, 0, count)
			for index := 0; index < count; index++ {
				table := fixtureTable(fmt.Sprintf("table_%05d", index))
				tables = append(tables, table)
			}
			var before, after runtime.MemStats
			tableFingerprintStarted := time.Now()
			if _, _, _, _, err := CanonicalTable(tables[0]); err != nil {
				t.Fatal(err)
			}
			tableFingerprintDuration := time.Since(tableFingerprintStarted)
			runtime.ReadMemStats(&before)
			started := time.Now()
			snapshot, _, err := BuildSnapshot(fixtureManifest(), tables)
			duration := time.Since(started)
			runtime.ReadMemStats(&after)
			if err != nil || len(snapshot.Tables) != count {
				t.Fatalf("count=%d err=%v snapshot=%d", count, err, len(snapshot.Tables))
			}
			snapshotHashStarted := time.Now()
			identity, err := snapshotIdentityBytes(snapshot)
			if err != nil || sha256Hex(identity) != snapshot.SourceSnapshotSHA256 {
				t.Fatalf("snapshot hash verification failed: %v", err)
			}
			snapshotHashDuration := time.Since(snapshotHashStarted)
			t.Logf("tables=%d canonicalize_and_snapshot=%s allocated_bytes=%d table_fingerprint=%s source_snapshot_hash=%s snapshot_sha=%s", count, duration, after.TotalAlloc-before.TotalAlloc, tableFingerprintDuration, snapshotHashDuration, snapshot.SourceSnapshotSHA256)
		})
	}
}

func fixtureManifest() SourceManifest {
	return SourceManifest{
		Version: SourceManifestVersion, SourceID: "primary", Engine: EnginePostgreSQL,
		Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{},
		ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics:    CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"},
		BusinessDataRead: false,
	}
}

func fixtureTable(name string) TableEvidence {
	ref, _ := CanonicalObjectRef("primary", "public", name)
	defaultValue := "nextval('sequence'::regclass)"
	return TableEvidence{
		Version: EvidenceVersion, ObjectRef: ref, Engine: EnginePostgreSQL,
		SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table",
		Columns: []Column{
			{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false, DefaultExpression: &defaultValue, Identity: "BY DEFAULT"},
			{Ordinal: 2, Name: "email", NativeType: "text", CanonicalType: "text", Nullable: false},
			{Ordinal: 3, Name: "name", NativeType: "text", CanonicalType: "text", Nullable: true},
		},
		PrimaryKey:        &KeyConstraint{Name: name + "_pkey", Columns: []string{"id"}},
		UniqueConstraints: []KeyConstraint{}, ForeignKeys: []ForeignKey{}, Checks: []CheckConstraint{},
		Indexes: []Index{{Name: name + "_email_idx", Unique: false, Method: "btree", Elements: []IndexElement{{Position: 1, Column: stringPointer("email")}}}},
	}
}

func stringPointer(value string) *string { return &value }

func snapshotHashes(snapshot Snapshot) map[string]string {
	result := map[string]string{}
	for _, table := range snapshot.Tables {
		result[table.ObjectRef] = table.TableEvidenceSHA256
	}
	return result
}
