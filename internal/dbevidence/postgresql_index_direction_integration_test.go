//go:build integration

package dbevidence

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

func TestPostgreSQLRealCatalogCollectionAndDriftIndexDirections(t *testing.T) {
	adminDSN := postgresqlAdminDSN(t)
	credential := randomCredential(t)
	readerDSN := postgresqlUserDSN(t, adminDSN, "aoci_direction_reader", credential)
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	execIntegrationSQL(t, admin, []string{
		`DROP SCHEMA IF EXISTS aoci_direction CASCADE`,
		`CREATE SCHEMA aoci_direction`,
		`CREATE TABLE aoci_direction.items (id integer, rank integer, label text, payload jsonb)`,
		`CREATE TABLE aoci_direction.unchanged (id integer)`,
		`CREATE INDEX mixed ON aoci_direction.items (id ASC, rank DESC) INCLUDE (label)`,
		`CREATE INDEX expression ON aoci_direction.items ((lower(label)) DESC)`,
		`CREATE INDEX unordered ON aoci_direction.items USING gin (payload)`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='aoci_direction_reader') THEN CREATE ROLE aoci_direction_reader LOGIN PASSWORD '` + credential + `'; ELSE ALTER ROLE aoci_direction_reader PASSWORD '` + credential + `'; END IF; END $$`,
		`GRANT CONNECT ON DATABASE postgres TO aoci_direction_reader`,
		`GRANT USAGE ON SCHEMA aoci_direction TO aoci_direction_reader`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA aoci_direction TO aoci_direction_reader`,
	})
	defer execIntegrationSQL(t, admin, []string{
		`DROP SCHEMA IF EXISTS aoci_direction CASCADE`,
		`REVOKE CONNECT ON DATABASE postgres FROM aoci_direction_reader`,
		`DROP ROLE IF EXISTS aoci_direction_reader`,
	})
	t.Setenv("AOCI_TEST_POSTGRES_DIRECTION_DSN", readerDSN)
	source := SourceConfig{SourceID: "pgdirection", Engine: EnginePostgreSQL, Database: "postgres", Namespaces: []string{"aoci_direction"}, CredentialEnv: "AOCI_TEST_POSTGRES_DIRECTION_DSN", Enabled: true}
	collector := NewCollector()
	manifest, first, files, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationEvidence(t, manifest, first, files, 2, credential)
	const object = "database://pgdirection/aoci_direction/items"
	var table TableEvidence
	if err := decodeStrict(files[object], &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Indexes) != 3 {
		t.Fatalf("indexes lost: %+v", table.Indexes)
	}
	for _, index := range table.Indexes {
		want := []bool{false}
		switch index.Name {
		case "mixed":
			want = []bool{false, true, false}
			if len(index.Elements) != 3 || !index.Elements[2].Included || index.Elements[2].Column == nil || *index.Elements[2].Column != "label" {
				t.Fatalf("INCLUDE column lost: %+v", index)
			}
		case "expression":
			want = []bool{true}
			if len(index.Elements) != 1 || index.Elements[0].Expression == nil || *index.Elements[0].Expression != "lower(label)" {
				t.Fatalf("expression key lost: %+v", index)
			}
		case "unordered":
			if index.Method != "gin" {
				t.Fatalf("unordered method changed: %+v", index)
			}
		default:
			t.Fatalf("unexpected index: %s", index.Name)
		}
		got := make([]bool, len(index.Elements))
		for i, element := range index.Elements {
			got[i] = element.Descending
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("index %s directions: got %v, want %v", index.Name, got, want)
		}
	}
	_, repeated, repeatedFiles, err := collector.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertRepeatedSnapshotBytes(t, first, files, repeated, repeatedFiles)
	root := t.TempDir()
	baseline, baselineBytes := acceptRealSnapshot(t, root, manifest, first, files)
	if report := CompareSnapshot(repeated, baseline, true); report.Summary != (DriftSummary{Unchanged: 2}) {
		t.Fatalf("unchanged direction snapshot drifted: %+v", report)
	}
	for _, change := range []struct {
		name string
		ddl  []string
	}{
		{"column", []string{`DROP INDEX aoci_direction.mixed`, `CREATE INDEX mixed ON aoci_direction.items (id ASC, rank ASC) INCLUDE (label)`}},
		{"expression", []string{`DROP INDEX aoci_direction.expression`, `CREATE INDEX expression ON aoci_direction.items ((lower(label)) ASC)`}},
	} {
		t.Run(change.name, func(t *testing.T) {
			execIntegrationSQL(t, admin, change.ddl)
			_, next, nextFiles, err := collector.Snapshot(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			report := CompareSnapshot(next, baseline, true)
			if report.Summary != (DriftSummary{Unchanged: 1, Changed: 1}) || report.SourceIdentityChanged || report.SourceSnapshotSHA256 == first.SourceSnapshotSHA256 {
				t.Fatalf("direction-only change was not detected: %+v", report)
			}
			for _, item := range report.Items {
				if item.ObjectRef == object && !reflect.DeepEqual(item.ChangedComponents, []string{"indexes"}) {
					t.Fatalf("direction-only change touched other components: %+v", item)
				}
			}
			assertUnrelatedTableStable(t, first, next, "database://pgdirection/aoci_direction/unchanged")
			assertBaselineUnchanged(t, root, baselineBytes)
			assertNoIntegrationSentinels(t, manifest, next, nextFiles, credential)
			// Each change starts from an explicitly accepted snapshot, so the
			// expression case proves its own direction-only transition.
			baseline, baselineBytes = acceptRealSnapshot(t, root, manifest, next, nextFiles)
			first = next
		})
	}
}
