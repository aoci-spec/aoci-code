package migrationapply

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// JSONSchemas derives strict review schemas directly from the Go machine
// contracts. The schemas are documentation/test artifacts; the strict decoder
// and the typed validators remain the executable authority.
func JSONSchemas() (map[string]json.RawMessage, error) {
	contracts := []struct {
		version string
		value   any
	}{
		{machinecontract.CognitionLegacySnapshotV1, LegacySnapshot{}},
		{machinecontract.CognitionMigrationMappingV2, MigrationMapping{}},
		{machinecontract.CognitionMigrationApplyRequestV2, ApplyRequest{}},
		{machinecontract.CognitionMigrationApplyEnvelopeV2, ApplyEnvelope{}},
		{machinecontract.CognitionMigrationApprovalV2, Approval{}},
		{machinecontract.CognitionMigrationRecoveryV2, RecoveryIntent{}},
		{machinecontract.CognitionMigrationReceiptV2, MigrationReceipt{}},
		{machinecontract.CognitionMigrationApplyResultV2, ApplyResult{}},
		{machinecontract.CognitionMigrationTransactionStatusV2, TransactionStatus{}},
		{machinecontract.CognitionMigrationReversalPlanV2, ReversalPlan{}},
		{machinecontract.CognitionMigrationReversalApprovalV2, ReversalApproval{}},
		{machinecontract.CognitionMigrationReversalRecoveryV2, reversalRecovery{}},
	}
	result := make(map[string]json.RawMessage, len(contracts))
	for _, contract := range contracts {
		schema := schemaForType(reflect.TypeOf(contract.value))
		schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		schema["$id"] = "urn:aoci:" + contract.version
		if properties, ok := schema["properties"].(map[string]any); ok {
			if version, ok := properties["version"].(map[string]any); ok {
				version["const"] = contract.version
			}
		}
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return nil, err
		}
		result[contract.version] = append(data, '\n')
	}
	return result, nil
}

func schemaForType(value reflect.Type) map[string]any {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			properties[name] = schemaForType(field.Type)
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaForType(value.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaForType(value.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{"type": "string"}
	}
}
