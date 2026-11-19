package dbevidence

import (
	"os"
	"reflect"
	"testing"
)

func TestCanonicalMySQLDefaultValuesPreserveCatalogBytes(t *testing.T) {
	for _, value := range []string{"  active  ", "\t", "first\r\nsecond", "", "active", "CURRENT_TIMESTAMP"} {
		t.Run(value, func(t *testing.T) {
			table := fixtureTable("users")
			table.Engine = EngineMySQL
			table.Columns[0].DefaultExpression = &value
			canonical, data, digest, components, err := CanonicalTable(table)
			if err != nil {
				t.Fatal(err)
			}
			got := canonical.Columns[0].DefaultExpression
			if got == nil {
				t.Fatal("catalog default disappeared during canonicalization")
			}
			if *got != value {
				t.Fatalf("catalog default changed during canonicalization: got %q, want %q", *got, value)
			}
			var decoded TableEvidence
			if err := decodeStrict(data, &decoded); err != nil {
				t.Fatal(err)
			}
			_, again, againDigest, againComponents, err := CanonicalTable(decoded)
			if err != nil || !reflect.DeepEqual(data, again) || digest != againDigest || components != againComponents {
				t.Fatalf("MySQL default evidence is not canonical after reload: %v", err)
			}
		})
	}
}

func TestCanonicalMySQLDefaultKeepsExistingEvidence(t *testing.T) {
	for _, path := range []string{"testdata/real/mysql_accounts.json", "testdata/real/mysql57_accounts.json"} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var table TableEvidence
			if err := decodeStrict(data, &table); err != nil {
				t.Fatal(err)
			}
			_, again, digest, _, err := CanonicalTable(table)
			if err != nil || !reflect.DeepEqual(data, again) || digest != sha256Hex(data) {
				t.Fatalf("existing MySQL evidence bytes changed: %v", err)
			}
		})
	}
}

func TestCanonicalOtherDefaultExpressionsStillNormalize(t *testing.T) {
	for _, engine := range []Engine{EnginePostgreSQL, EngineOpenGauss, EngineSQLite} {
		t.Run(string(engine), func(t *testing.T) {
			table := fixtureTable("users")
			table.Engine = engine
			value := "  1\r\n+ 2  "
			table.Columns[0].DefaultExpression = &value
			canonical, _, _, _, err := CanonicalTable(table)
			if err != nil {
				t.Fatal(err)
			}
			if got := canonical.Columns[0].DefaultExpression; got == nil || *got != "1\n+ 2" {
				t.Fatalf("existing SQL expression normalization changed: %v", got)
			}
		})
	}
}

func TestCanonicalMySQLDefaultWhitespaceChangesFingerprint(t *testing.T) {
	for _, pair := range [][2]string{{" active ", "active"}, {"first\r\nsecond", "first\nsecond"}, {"\t", ""}} {
		t.Run(pair[0], func(t *testing.T) {
			var digests [2]string
			var components [2]ComponentHashes
			for i, value := range pair {
				table := fixtureTable("users")
				table.Engine = EngineMySQL
				table.Columns[0].DefaultExpression = &value
				_, _, digest, hashes, err := CanonicalTable(table)
				if err != nil {
					t.Fatal(err)
				}
				digests[i], components[i] = digest, hashes
			}
			if digests[0] == digests[1] || !reflect.DeepEqual(changedComponents(components[0], components[1]), []string{"columns"}) {
				t.Fatalf("distinct MySQL default values collapsed: %q and %q", pair[0], pair[1])
			}
		})
	}
}
