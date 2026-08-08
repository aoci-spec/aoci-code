package migrationapply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestMigrationSchemaGolden(t *testing.T) {
	schemas, err := JSONSchemas()
	if err != nil {
		t.Fatal(err)
	}
	type identity struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	identities := make([]identity, 0, len(schemas))
	for version, schema := range schemas {
		identities = append(identities, identity{Version: version, SHA256: sha256Hex(schema)})
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].Version < identities[j].Version })
	data, _ := json.MarshalIndent(identities, "", "  ")
	data = append(data, '\n')
	expected, err := os.ReadFile(filepath.Join("testdata", "schemas.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(expected) {
		t.Fatalf("D2-C derived Schema Golden changed without review:\n%s", data)
	}
}

func TestMigrationSchemasExcludeRepairFindingFields(t *testing.T) {
	schemas, err := JSONSchemas()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		version string
		path    []string
	}{
		{machinecontract.CognitionMigrationApplyRequestV2, []string{"properties", "plan", "properties", "warnings", "items"}},
		{machinecontract.CognitionMigrationApplyEnvelopeV2, []string{"properties", "plan", "properties", "warnings", "items"}},
		{machinecontract.CognitionMigrationRecoveryV2, []string{"properties", "envelope", "properties", "plan", "properties", "warnings", "items"}},
		{machinecontract.CognitionMigrationReversalRecoveryV2, []string{"properties", "original_envelope", "properties", "plan", "properties", "warnings", "items"}},
	}
	wantProperties := []string{"asset_id", "code", "line", "message"}
	wantRequired := []string{"code", "message"}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal(schemas[test.version], &schema); err != nil {
				t.Fatal(err)
			}
			warning := schemaObjectAt(t, schema, test.path...)
			if additional, ok := warning["additionalProperties"].(bool); !ok || additional {
				t.Fatalf("Migration Finding must remain closed: %#v", warning["additionalProperties"])
			}
			properties := schemaObjectAt(t, warning, "properties")
			gotProperties := make([]string, 0, len(properties))
			for name := range properties {
				gotProperties = append(gotProperties, name)
			}
			sort.Strings(gotProperties)
			if !reflect.DeepEqual(gotProperties, wantProperties) {
				t.Fatalf("Migration Finding properties leaked RepairFinding fields: got %v want %v", gotProperties, wantProperties)
			}
			gotRequired := schemaStringsAt(t, warning, "required")
			if !reflect.DeepEqual(gotRequired, wantRequired) {
				t.Fatalf("Migration Finding required changed: got %v want %v", gotRequired, wantRequired)
			}
		})
	}
}

func TestFindingJSONContractsAreIsolated(t *testing.T) {
	legacy, err := json.Marshal(cognition.Finding{Code: "legacy_parse_warning", AssetID: "legacy", Line: 7, Message: "warning"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(legacy), `{"code":"legacy_parse_warning","asset_id":"legacy","line":7,"message":"warning"}`; got != want {
		t.Fatalf("Migration Finding JSON changed: got %s want %s", got, want)
	}

	repair, err := json.Marshal(cognition.RepairFinding{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(repair, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"candidate_index", "path", "canonical_object_identity", "domain", "field",
		"rule_code", "expected", "actual", "cause", "safe_repair_action",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("RepairFinding JSON missing %q: %s", name, repair)
		}
	}
}

func schemaObjectAt(t *testing.T, value map[string]any, path ...string) map[string]any {
	t.Helper()
	current := value
	for _, name := range path {
		next, ok := current[name].(map[string]any)
		if !ok {
			t.Fatalf("Schema path %q is not an object: %#v", name, current[name])
		}
		current = next
	}
	return current
}

func schemaStringsAt(t *testing.T, value map[string]any, name string) []string {
	t.Helper()
	raw, ok := value[name].([]any)
	if !ok {
		t.Fatalf("Schema field %q is not an array: %#v", name, value[name])
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("Schema field %q contains a non-string: %#v", name, item)
		}
		result = append(result, text)
	}
	return result
}
