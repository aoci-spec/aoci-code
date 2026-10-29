package dbevidence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlitedriver "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

func sqliteFixture(t *testing.T, ddl string) (string, SourceConfig) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "catalog #%.db")
	db, err := sqlitedriver.Open(path, fts5.Register)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return root, SourceConfig{SourceID: "local", Engine: EngineSQLite, Path: filepath.Base(path), Enabled: true}
}

func TestSQLiteSnapshotCatalogOnlyAndDeterministic(t *testing.T) {
	root, source := sqliteFixture(t, `
CREATE TABLE Parent (tenant TEXT, id INTEGER, PRIMARY KEY (id, tenant), UNIQUE (tenant, id));
CREATE TABLE child (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER, tenant TEXT,
  note TEXT DEFAULT 'CHECK and AUTOINCREMENT', payload,
  FOREIGN KEY (parent_id, tenant) REFERENCES parent ON DELETE CASCADE ON UPDATE RESTRICT);
CREATE INDEX child_order ON child(tenant DESC, parent_id);
CREATE VIEW ignored_view AS SELECT * FROM child;
INSERT INTO child(note) VALUES ('business_row_sentinel');`)
	before, err := os.ReadFile(filepath.Join(root, source.Path))
	if err != nil {
		t.Fatal(err)
	}
	collector := NewCollectorAtRoot(root)
	collector.credentialProvider = NewEnvironmentCredentialProvider(func(string) string { t.Fatal("SQLite resolved credentials"); return "" })
	manifest, snapshot, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	_, second, again, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SourceSnapshotSHA256 != second.SourceSnapshotSHA256 || !reflect.DeepEqual(files, again) || len(files) != 2 ||
		manifest.Database != source.Path || manifest.BusinessDataRead || snapshot.BusinessDataRead {
		t.Fatalf("bad or unstable snapshot: %+v %+v", manifest, snapshot)
	}
	if err := NormalizeSource(&source); err != nil {
		t.Fatal(err)
	}
	if !SourceConfigMatchesManifest(source, manifest) {
		t.Fatal("SQLite path identity lost")
	}
	source.Path = "other.db"
	if SourceConfigMatchesManifest(source, manifest) {
		t.Fatal("SQLite path change reused Evidence")
	}
	source.Path = manifest.Database
	var child, parent TableEvidence
	if err := decodeStrict(files["database://local/main/child"], &child); err != nil {
		t.Fatal(err)
	}
	if err := decodeStrict(files["database://local/main/Parent"], &parent); err != nil {
		t.Fatal(err)
	}
	if child.Columns[0].Nullable || !child.Columns[0].AutoIncrement || child.Columns[4].NativeType != "" || child.Columns[4].CanonicalType != "blob" ||
		child.Columns[3].DefaultExpression == nil || *child.Columns[3].DefaultExpression != "'CHECK and AUTOINCREMENT'" ||
		!reflect.DeepEqual(child.PrimaryKey.Columns, []string{"id"}) || !reflect.DeepEqual(parent.PrimaryKey.Columns, []string{"id", "tenant"}) ||
		len(parent.UniqueConstraints) != 1 || len(child.ForeignKeys) != 1 || len(child.Indexes) != 1 || !child.Indexes[0].Elements[0].Descending {
		t.Fatalf("catalog facts lost: child=%+v parent=%+v", child, parent)
	}
	fk := child.ForeignKeys[0]
	if fk.ReferencedObject != parent.ObjectRef || !reflect.DeepEqual(fk.ReferencedColumns, []string{"id", "tenant"}) ||
		fk.DeleteAction != "cascade" || fk.UpdateAction != "restrict" {
		t.Fatalf("bad FK: %+v", fk)
	}
	for _, data := range files {
		if strings.Contains(string(data), "business_row_sentinel") {
			t.Fatal("business rows leaked")
		}
	}
	if err := WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	_, saved, exists, err := LoadSnapshot(root, source.SourceID)
	if err != nil || !exists || saved.SourceSnapshotSHA256 != snapshot.SourceSnapshotSHA256 {
		t.Fatalf("Evidence roundtrip failed: %v", err)
	}
	if err := AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	baseline, _, err := LoadBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if report := CompareSnapshot(snapshot, baseline, true); report.Summary.Unchanged != 2 {
		t.Fatalf("bad drift: %+v", report)
	}
	after, err := os.ReadFile(filepath.Join(root, source.Path))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("database bytes changed")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(filepath.Join(root, source.Path) + suffix); !os.IsNotExist(err) {
			t.Fatalf("sidecar created: %s (%v)", suffix, err)
		}
	}
}

func TestSQLiteConnectionDeniesBusinessReadsAndWrites(t *testing.T) {
	root, source := sqliteFixture(t, `CREATE TABLE private_rows (id INTEGER); INSERT INTO private_rows VALUES (42);`)
	if err := NormalizeSource(&source); err != nil {
		t.Fatal(err)
	}
	dsn, err := sqliteDataSource(root, source)
	if err != nil {
		t.Fatal(err)
	}
	db, err := openSQLiteDatabase(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("read-only open: %v (dsn=%s)", err, dsn)
	}
	for _, query := range []string{
		"SELECT * FROM private_rows", "SELECT count(*) FROM private_rows", "INSERT INTO private_rows VALUES (43)",
		"CREATE TABLE bad (id)", "PRAGMA query_only=off", "PRAGMA writable_schema=on", "ATTACH ':memory:' AS other",
		"SELECT load_extension('anything')", "PRAGMA user_version=1",
	} {
		if rows, err := db.Query(query); err == nil {
			rows.Close()
			t.Fatalf("catalog boundary accepted %q", query)
		}
	}
}

func TestSQLiteUnsupportedCatalogFailsClosed(t *testing.T) {
	for name, ddl := range map[string]string{
		"virtual":         `CREATE VIRTUAL TABLE bad USING fts5(body);`,
		"expression":      `CREATE TABLE bad (id INTEGER); CREATE INDEX expr ON bad ((id+1));`,
		"partial":         `CREATE TABLE bad (id INTEGER); CREATE INDEX part ON bad (id) WHERE id > 1;`,
		"generated":       `CREATE TABLE bad (id INTEGER, derived INTEGER GENERATED ALWAYS AS (id+1));`,
		"check":           `CREATE TABLE bad (id INTEGER CHECK(id > 0));`,
		"collation":       `CREATE TABLE bad (id TEXT COLLATE NOCASE);`,
		"index collation": `CREATE TABLE bad (id TEXT); CREATE INDEX coll ON bad(id COLLATE NOCASE);`,
		"deferred":        `CREATE TABLE bad (id INTEGER REFERENCES bad(id) DEFERRABLE INITIALLY DEFERRED);`,
		"strict":          `CREATE TABLE bad (id ANY) STRICT;`,
		"conflict":        `CREATE TABLE bad (id INT UNIQUE ON CONFLICT REPLACE);`,
		"without rowid":   `CREATE TABLE bad (id TEXT PRIMARY KEY) WITHOUT ROWID;`,
	} {
		t.Run(name, func(t *testing.T) {
			root, source := sqliteFixture(t, ddl)
			_, snapshot, files, err := NewCollectorAtRoot(root).Snapshot(context.Background(), source)
			var sourceErr *SourceError
			if !errors.As(err, &sourceErr) || sourceErr.Code != "unsupported_catalog_feature" || files != nil || snapshot.SourceSnapshotSHA256 != "" {
				t.Fatalf("unsupported catalog returned success or partial Evidence: %v %+v", err, snapshot)
			}
		})
	}
}

func TestSQLiteSelectionQuotedNamesAndPrimaryKeyNullability(t *testing.T) {
	root, source := sqliteFixture(t, `
CREATE TABLE "odd'); DROP TABLE keep; --" ("check" TEXT DEFAULT 'DEFERRABLE', "a""b" INT);
CREATE TABLE keep (a TEXT PRIMARY KEY);
CREATE TABLE desc_pk (a INTEGER PRIMARY KEY DESC);
CREATE TABLE excluded (id INT CHECK(id > 0));`)
	source.ExcludeTables = []string{"excluded"}
	_, snapshot, files, err := NewCollectorAtRoot(root).Snapshot(context.Background(), source)
	if err != nil || len(snapshot.Tables) != 3 {
		t.Fatalf("selected catalog: %v %+v", err, snapshot)
	}
	for name, nullable := range map[string]bool{"keep": true, "desc_pk": true} {
		var table TableEvidence
		if err := decodeStrict(files["database://local/main/"+name], &table); err != nil {
			t.Fatal(err)
		}
		if table.Columns[0].Nullable != nullable {
			t.Fatalf("%s: nullability=%t", name, table.Columns[0].Nullable)
		}
	}
}

func TestSQLiteMissingEmptyAndInvalidFilesDoNotBecomeDatabases(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string][]byte{"missing.db": nil, "empty.db": {}, "invalid.db": []byte("credential_sentinel")} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name)
			if data != nil {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := SourceConfig{SourceID: "local", Engine: EngineSQLite, Path: name, Enabled: true}
			_, _, _, err := NewCollectorAtRoot(root).Snapshot(context.Background(), source)
			if err == nil || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "credential_sentinel") {
				t.Fatalf("bad file error: %v", err)
			}
			after, readErr := os.ReadFile(path)
			if data == nil && !os.IsNotExist(readErr) || data != nil && (readErr != nil || !reflect.DeepEqual(data, after)) {
				t.Fatal("file created or modified")
			}
		})
	}
}

func TestSQLitePragmaNamesCannotReadBusinessRows(t *testing.T) {
	root, source := sqliteFixture(t, `
CREATE TABLE pragma_table_list ("schema" TEXT, name TEXT, type TEXT, wr INT, strict INT);
INSERT INTO pragma_table_list VALUES ('main', 'business_row_sentinel', 'table', 0, 0);
CREATE TABLE pragma_table_xinfo (id INT);
CREATE TABLE pragma_index_list (id INT);
CREATE TABLE pragma_index_xinfo (id INT);
CREATE TABLE pragma_foreign_key_list (id INT);`)
	_, snapshot, files, err := NewCollectorAtRoot(root).Snapshot(context.Background(), source)
	if err != nil || len(snapshot.Tables) != 5 {
		t.Fatalf("catalog names were shadowed: %v %+v", err, snapshot)
	}
	for _, data := range files {
		if strings.Contains(string(data), "business_row_sentinel") {
			t.Fatal("pragma shadow leaked business rows")
		}
	}
	if err := NormalizeSource(&source); err != nil {
		t.Fatal(err)
	}
	dsn, err := sqliteDataSource(root, source)
	if err != nil {
		t.Fatal(err)
	}
	db, err := openSQLiteDatabase(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	if rows, err := db.Query("SELECT * FROM pragma_table_list"); err == nil {
		rows.Close()
		t.Fatal("authorizer accepted shadow table read")
	}
}

func TestSQLiteWALSeesCommittedCatalogWithoutChangingDatabase(t *testing.T) {
	root, source := sqliteFixture(t, `CREATE TABLE original (id INTEGER PRIMARY KEY);`)
	path := filepath.Join(root, source.Path)
	writer, err := sqlitedriver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE in_wal (id INTEGER);`); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, suffix := range []string{"", "-wal"} {
		before[suffix], err = os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, snapshot, _, err := NewCollectorAtRoot(root).Snapshot(context.Background(), source)
	if err != nil || len(snapshot.Tables) != 2 {
		t.Fatalf("committed WAL catalog was missed: %v %+v", err, snapshot)
	}
	for suffix, data := range before {
		after, err := os.ReadFile(path + suffix)
		if err != nil || !reflect.DeepEqual(data, after) {
			t.Fatalf("collector changed database%s: %v", suffix, err)
		}
	}
}

func TestSQLiteCancellationAndDisabledSource(t *testing.T) {
	root, source := sqliteFixture(t, `CREATE TABLE t (id INTEGER);`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := NewCollectorAtRoot(root).Snapshot(ctx, source)
	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || sourceErr.Code != "cancelled" {
		t.Fatalf("cancellation: %v", err)
	}
	source.Enabled = false
	source.Path = "missing.db"
	_, _, _, err = NewCollectorAtRoot(root).Snapshot(context.Background(), source)
	if !errors.As(err, &sourceErr) || sourceErr.Code != "source_disabled" {
		t.Fatalf("disabled source opened: %v", err)
	}
}

func TestSQLiteLockedCatalogHonorsDeadline(t *testing.T) {
	root, source := sqliteFixture(t, `CREATE TABLE t (id INTEGER);`)
	writer, err := sqlitedriver.Open(filepath.Join(root, source.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer writer.Exec("ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, _, err = NewCollectorAtRoot(root).Snapshot(ctx, source)
	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || sourceErr.Code != "timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("locked catalog deadline: %v (%s)", err, time.Since(start))
	}
}

func TestSQLiteAffinityUsesASCIIAndSQLitePrecedence(t *testing.T) {
	for native, want := range map[string]string{"CHARINT": "integer", "FLOATING POINT": "integer", "STRING": "numeric", "charınt": "text", "ınt": "numeric", "": "blob", "DOUBLE": "real"} {
		if got := sqliteAffinity(native); got != want {
			t.Errorf("%q affinity=%s want=%s", native, got, want)
		}
	}
}
