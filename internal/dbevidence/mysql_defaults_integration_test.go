//go:build integration

package dbevidence

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

func TestMySQLDefaultValuesPreserveCatalogBytesAndDrift(t *testing.T) {
	adminDSN := mysqlAdminDSN(t)
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	credential := randomCredential(t)
	readerDSN := mysqlUserDSN(t, adminDSN, "aoci_defaults_reader", credential)
	execIntegrationSQL(t, admin, []string{
		`DROP TABLE IF EXISTS aoci_default_values`,
		`CREATE TABLE aoci_default_values (id integer PRIMARY KEY, content_value varchar(100))`,
		`DROP USER IF EXISTS 'aoci_defaults_reader'@'%'`,
		`CREATE USER 'aoci_defaults_reader'@'%' IDENTIFIED BY '` + credential + `'`,
		`GRANT SELECT ON aoci_test.aoci_default_values TO 'aoci_defaults_reader'@'%'`,
	})
	defer execIntegrationSQL(t, admin, []string{`DROP TABLE IF EXISTS aoci_default_values`, `DROP USER IF EXISTS 'aoci_defaults_reader'@'%'`})
	t.Setenv("AOCI_TEST_DEFAULTS_DSN", readerDSN)
	source := SourceConfig{SourceID: "defaults", Engine: EngineMySQL, Database: "aoci_test", Namespaces: []string{"aoci_test"}, IncludeTables: []string{"aoci_default_values"}, CredentialEnv: "AOCI_TEST_DEFAULTS_DSN", Enabled: true}
	collector := NewCollector()
	for _, pair := range [][2]string{{" active ", "active"}, {"first\r\nsecond", "first\nsecond"}, {"\t", ""}} {
		t.Run(pair[0], func(t *testing.T) {
			// These fixed literals use actual whitespace, independent of the
			// server's NO_BACKSLASH_ESCAPES mode.
			execIntegrationSQL(t, admin, []string{"ALTER TABLE aoci_default_values ALTER COLUMN content_value SET DEFAULT '" + pair[0] + "'"})
			var catalogDefault string
			if err := admin.QueryRow(`SELECT COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = 'aoci_test' AND TABLE_NAME = 'aoci_default_values' AND COLUMN_NAME = 'content_value'`).Scan(&catalogDefault); err != nil || catalogDefault != pair[0] {
				t.Fatalf("unexpected real catalog default: got %q, want %q, error %v", catalogDefault, pair[0], err)
			}
			manifest, first, files, err := collector.Snapshot(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			var table TableEvidence
			if err := decodeStrict(files["database://defaults/aoci_test/aoci_default_values"], &table); err != nil {
				t.Fatal(err)
			}
			got := table.Columns[1].DefaultExpression
			if got == nil {
				t.Fatal("catalog default disappeared during canonicalization")
			}
			if *got != catalogDefault {
				t.Fatalf("catalog default changed during canonicalization: got %q, want %q", *got, catalogDefault)
			}
			_, repeated, repeatedFiles, err := collector.Snapshot(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			assertRepeatedSnapshotBytes(t, first, files, repeated, repeatedFiles)
			root := t.TempDir()
			baseline, baselineBytes := acceptRealSnapshot(t, root, manifest, first, files)
			if report := CompareSnapshot(repeated, baseline, true); report.Summary.Unchanged != 1 || report.Summary.Changed != 0 {
				t.Fatalf("unchanged default produced drift: %+v", report)
			}
			execIntegrationSQL(t, admin, []string{"ALTER TABLE aoci_default_values ALTER COLUMN content_value SET DEFAULT '" + pair[1] + "'"})
			_, changed, _, err := collector.Snapshot(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			report := CompareSnapshot(changed, baseline, true)
			if report.Summary.Changed != 1 || report.Summary.Unchanged != 0 || len(report.Items) != 1 || !reflect.DeepEqual(report.Items[0].ChangedComponents, []string{"columns"}) {
				t.Fatalf("default-only change was not detected: %+v", report)
			}
			if first.SourceSnapshotSHA256 == changed.SourceSnapshotSHA256 {
				t.Fatal("default-only change retained the source snapshot identity")
			}
			assertBaselineUnchanged(t, root, baselineBytes)
		})
	}
}
