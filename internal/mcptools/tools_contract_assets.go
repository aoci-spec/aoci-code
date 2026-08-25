package mcptools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

type mcpToolDescriptions map[textassets.ID]string
type mcpInputSchemas map[string]any

var mcpInputSchemaFields = map[string][]string{
	"aoci_rules": {"module_path"},
	"aoci_overview": {
		"scope", "cognition_state_version", "cognition_receipt", "model_cognition_state", "scope_covered", "check_only",
		"refresh_reasons", "refresh_event_id", "stable_checkpoint", "host_delivery_confirmation", "model_cognition_attestation",
		"model_cognition_attestation.index_sha256", "model_cognition_attestation.entry_sequence_sha256", "model_cognition_attestation.entry_count",
		"model_cognition_attestation.system_mastery_percent", "model_cognition_attestation.challenge_answers[].ordinal",
		"model_cognition_attestation.challenge_answers[].object_identity", "cursor",
		"probe", "probe_answers",
	},
	"aoci_get_entries": {"paths", "dir", "volume_id", "object_refs"},
	"aoci_search":      {"keyword", "tag_filter", "scope"},
	"aoci_update_entry": {
		"path", "object_ref", "new_entry", "source_sha256", "candidate_id", "batch_id", "code_batch_id", "entries",
		"entries[].path", "entries[].object_ref", "entries[].new_entry", "entries[].source_sha256", "entries[].candidate_id",
	},
	"aoci_maintain":     {"scope", "intent", "object_refs"},
	"aoci_report":       {"path", "note"},
	"aoci_remove_entry": {"path"},
}

func loadMCPToolDescriptions() (mcpToolDescriptions, error) {
	ids := []textassets.ID{
		textassets.ContractMCPRulesDescription,
		textassets.ContractMCPOverviewDescription,
		textassets.ContractMCPGetEntriesDescription,
		textassets.ContractMCPSearchDescription,
		textassets.ContractMCPHeaderDescription,
		textassets.ContractMCPUpdateDescription,
		textassets.ContractMCPReportDescription,
		textassets.ContractMCPRemoveDescription,
		textassets.ContractMaintainToolDescription,
	}
	descriptions := make(mcpToolDescriptions, len(ids))
	for _, id := range ids {
		value, err := textassets.RenderScalar(textassets.ActiveLocale(), id, nil)
		if err != nil {
			return nil, fmt.Errorf("%s", mcpMessage(
				"mcp.asset.tool_description_failed",
				id,
				localeSafeMCPDetail(err.Error()),
			))
		}
		descriptions[id] = value
	}

	return descriptions, nil
}

func loadMCPInputSchemas() (mcpInputSchemas, error) {
	content, err := textassets.Load(
		textassets.ActiveLocale(),
		textassets.ContractMCPInputSchemaDescriptions,
	)
	if err != nil {
		return nil, fmt.Errorf("%s", mcpMessage(
			"mcp.asset.input_schema_load_failed",
			localeSafeMCPDetail(err.Error()),
		))
	}
	if err := jsonstrict.RejectDuplicateKeys([]byte(content)); err != nil {
		return nil, fmt.Errorf("%s", mcpMessage(
			"mcp.asset.input_schema_decode_failed",
			localeSafeMCPDetail(err.Error()),
		))
	}
	var descriptions map[string]map[string]string
	if err := json.Unmarshal([]byte(content), &descriptions); err != nil {
		return nil, fmt.Errorf("%s", mcpMessage(
			"mcp.asset.input_schema_decode_failed",
			localeSafeMCPDetail(err.Error()),
		))
	}
	if err := validateMCPInputSchemaDescriptions(descriptions); err != nil {
		return nil, err
	}

	schemas := make(mcpInputSchemas, len(descriptions))
	for toolName, values := range descriptions {
		schema, buildErr := localizedMCPSchema(toolName, values)
		if buildErr != nil {
			return nil, fmt.Errorf("%s", mcpMessage(
				"mcp.asset.input_schema_build_failed",
				toolName,
				localeSafeMCPDetail(buildErr.Error()),
			))
		}
		schemas[toolName] = schema
	}
	return schemas, nil
}

func validateMCPInputSchemaDescriptions(values map[string]map[string]string) error {
	if len(values) != len(mcpInputSchemaFields) {
		return fmt.Errorf("%s", mcpMessage("mcp.asset.input_schema_toolset_incomplete"))
	}
	for toolName, fields := range mcpInputSchemaFields {
		toolValues, exists := values[toolName]
		if !exists || len(toolValues) != len(fields) {
			return fmt.Errorf("%s", mcpMessage("mcp.asset.input_schema_tool_incomplete", toolName))
		}
		for _, field := range fields {
			if strings.TrimSpace(toolValues[field]) == "" {
				return fmt.Errorf("%s", mcpMessage("mcp.asset.input_schema_field_missing", toolName, field))
			}
		}
	}
	for toolName := range values {
		if _, exists := mcpInputSchemaFields[toolName]; !exists {
			return fmt.Errorf("%s", mcpMessage("mcp.asset.input_schema_unknown_tool", toolName))
		}
	}
	return nil
}

func localizedMCPSchema(toolName string, descriptions map[string]string) (map[string]any, error) {
	stringProperty := func(key string) map[string]any {
		return map[string]any{"type": "string", "description": descriptions[key]}
	}
	booleanProperty := func(key string) map[string]any {
		return map[string]any{"type": "boolean", "description": descriptions[key]}
	}
	scopeProperty := func(key string) map[string]any {
		return map[string]any{
			"type": "string", "description": descriptions[key],
			"enum": []string{cognition.ScopeAll, cognition.ScopeProject, cognition.ScopeMeta, cognition.ScopeCode, cognition.ScopeDatabase},
		}
	}
	object := func(properties map[string]any, required ...string) map[string]any {
		result := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           properties,
		}
		if len(required) > 0 {
			result["required"] = required
		}
		return result
	}

	switch toolName {
	case "aoci_rules":
		modulePath := stringProperty("module_path")
		modulePath["minLength"] = 1
		return object(map[string]any{"module_path": modulePath}), nil

	case "aoci_get_entries":
		paths := map[string]any{
			"type":        []string{"null", "array"},
			"description": descriptions["paths"],
			"items":       map[string]any{"type": "string"},
		}
		objectRefs := map[string]any{
			"type": []string{"null", "array"}, "description": descriptions["object_refs"],
			"items": map[string]any{"type": "string"},
		}
		return object(map[string]any{
			"paths": paths, "dir": stringProperty("dir"),
			"volume_id": map[string]any{"type": "string", "enum": []string{"code", "database"}, "description": descriptions["volume_id"]}, "object_refs": objectRefs,
		}), nil

	case "aoci_search":
		return object(map[string]any{
			"keyword":    stringProperty("keyword"),
			"tag_filter": stringProperty("tag_filter"),
			"scope":      scopeProperty("scope"),
		}), nil

	case "aoci_report":
		return object(map[string]any{
			"path": stringProperty("path"),
			"note": stringProperty("note"),
		}, "path", "note"), nil

	case "aoci_remove_entry":
		return object(map[string]any{"path": stringProperty("path")}, "path"), nil

	case "aoci_update_entry":
		item := object(map[string]any{
			"path":          stringProperty("entries[].path"),
			"object_ref":    stringProperty("entries[].object_ref"),
			"new_entry":     stringProperty("entries[].new_entry"),
			"source_sha256": stringProperty("entries[].source_sha256"),
			"candidate_id":  stringProperty("entries[].candidate_id"),
		}, "new_entry")
		item["oneOf"] = []any{
			map[string]any{"required": []string{"path", "source_sha256"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"object_ref"}}, map[string]any{"required": []string{"candidate_id"}}}}},
			map[string]any{"required": []string{"path", "source_sha256", "candidate_id"}, "not": map[string]any{"required": []string{"object_ref"}}},
			map[string]any{"required": []string{"object_ref", "source_sha256"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"path"}}, map[string]any{"required": []string{"candidate_id"}}}}},
			map[string]any{"required": []string{"object_ref", "candidate_id"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"path"}}, map[string]any{"required": []string{"source_sha256"}}}}},
		}
		entries := map[string]any{
			"type":        []string{"null", "array"},
			"description": descriptions["entries"],
			"items":       item,
		}
		return object(map[string]any{
			"path":          stringProperty("path"),
			"object_ref":    stringProperty("object_ref"),
			"new_entry":     stringProperty("new_entry"),
			"source_sha256": stringProperty("source_sha256"),
			"candidate_id":  stringProperty("candidate_id"),
			"batch_id":      stringProperty("batch_id"),
			"code_batch_id": stringProperty("code_batch_id"),
			"entries":       entries,
		}), nil

	case "aoci_maintain":
		return object(map[string]any{
			"scope": map[string]any{"type": "string", "description": descriptions["scope"], "enum": []string{cognition.ScopeCode, cognition.ScopeDatabase, cognition.ScopeAll}},
			"intent": map[string]any{
				"type": "string", "description": descriptions["intent"],
				"enum": []string{"cognition_optimization"},
			},
			"object_refs": map[string]any{
				"type": "array", "description": descriptions["object_refs"],
				"items": map[string]any{"type": "string", "pattern": `^code:.+`},
			},
		}), nil

	case "aoci_overview":
		volumeReceipt := object(map[string]any{
			"id": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"},
			"path": map[string]any{"type": "string"}, "asset_state": map[string]any{"type": "string"},
			"sha256": map[string]any{"type": "string"}, "object_count": map[string]any{"type": "integer"},
		}, "id", "kind", "path", "asset_state", "object_count")
		refreshReasonProperty := map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string", "enum": machinecontract.RefreshReasons()},
		}
		receiptV1Properties := map[string]any{
			"version":                       map[string]any{"type": "integer"},
			"runtime_repository_root":       map[string]any{"type": "string"},
			"index_sha256":                  map[string]any{"type": "string"},
			"mcp_service_version":           map[string]any{"type": "string"},
			"cognition_scope":               map[string]any{"type": "string"},
			"cognition_state":               map[string]any{"type": "string"},
			"state_owner":                   map[string]any{"type": "string"},
			"model_full_cognition_reliable": map[string]any{"type": "boolean"},
			"refresh_generation":            map[string]any{"type": "integer"},
			"last_refresh_event_id":         map[string]any{"type": "string"},
			"pending_refresh_reasons":       refreshReasonProperty,
		}
		receiptV2Properties := map[string]any{
			"version":                        map[string]any{"type": "integer"},
			"runtime_repository_root":        map[string]any{"type": "string"},
			"mcp_service_version":            map[string]any{"type": "string"},
			"cognition_scope":                map[string]any{"type": "string"},
			"cognition_state":                map[string]any{"type": "string"},
			"state_owner":                    map[string]any{"type": "string"},
			"model_full_cognition_reliable":  map[string]any{"type": "boolean"},
			"refresh_generation":             map[string]any{"type": "integer"},
			"last_refresh_event_id":          map[string]any{"type": "string"},
			"pending_refresh_reasons":        refreshReasonProperty,
			"layout_mode":                    map[string]any{"type": "string"},
			"requested_scope":                map[string]any{"type": "string"},
			"effective_scope":                map[string]any{"type": "string"},
			"scope_available":                map[string]any{"type": "boolean"},
			"asset_state":                    map[string]any{"type": "string", "enum": []string{cognition.AssetAbsent, cognition.AssetPresent, cognition.AssetInvalid}},
			"root_sha256":                    map[string]any{"type": "string"},
			"meta_sha256":                    map[string]any{"type": "string"},
			"delivered_volumes":              map[string]any{"type": "array", "items": volumeReceipt},
			"scope_object_count":             map[string]any{"type": "integer"},
			"scope_identity":                 map[string]any{"type": "string"},
			"composite_identity":             map[string]any{"type": "string"},
			"model_scope_cognition_reliable": map[string]any{"type": "boolean"},
		}
		receiptV1 := object(receiptV1Properties,
			"version", "runtime_repository_root", "index_sha256", "mcp_service_version",
			"cognition_scope", "cognition_state", "state_owner", "model_full_cognition_reliable",
		)
		receiptV2 := object(receiptV2Properties,
			"version", "runtime_repository_root", "mcp_service_version",
			"cognition_scope", "cognition_state", "state_owner", "layout_mode",
			"requested_scope", "effective_scope", "scope_available", "asset_state", "root_sha256", "meta_sha256",
			"delivered_volumes", "scope_object_count", "scope_identity", "composite_identity",
			"model_scope_cognition_reliable", "model_full_cognition_reliable",
			"refresh_generation", "pending_refresh_reasons",
		)
		receipt := map[string]any{
			"description": descriptions["cognition_receipt"],
			"anyOf":       []any{map[string]any{"type": "null"}, receiptV1, receiptV2},
		}
		scopeCovered := map[string]any{
			"type":        []string{"null", "boolean"},
			"description": descriptions["scope_covered"],
		}
		refreshReasons := map[string]any{
			"type":        "array",
			"description": descriptions["refresh_reasons"],
			"items": map[string]any{
				"type": "string",
				"enum": []string{
					machinecontract.RefreshReasonContextCompaction,
					machinecontract.RefreshReasonPhaseTransition,
				},
			},
			"uniqueItems": true,
		}
		stableCheckpoint := map[string]any{
			"type":        []string{"null", "boolean"},
			"description": descriptions["stable_checkpoint"],
		}
		return object(map[string]any{
			"scope": scopeProperty("scope"),
			"cognition_state_version": map[string]any{
				"type": "string", "const": machinecontract.CognitionStateV2,
				"description": descriptions["cognition_state_version"],
			},
			"cognition_receipt":     receipt,
			"model_cognition_state": stringProperty("model_cognition_state"),
			"scope_covered":         scopeCovered,
			"check_only":            booleanProperty("check_only"),
			"refresh_reasons":       refreshReasons,
			"refresh_event_id":      stringProperty("refresh_event_id"),
			"stable_checkpoint":     stableCheckpoint,
			"host_delivery_confirmation": object(map[string]any{
				"version": map[string]any{"type": "string"}, "body_sha256": map[string]any{"type": "string"},
				"body_bytes": map[string]any{"type": "integer"}, "end_marker_observed": map[string]any{"type": "boolean"},
			}, "version", "body_sha256", "body_bytes", "end_marker_observed"),
			"model_cognition_attestation": object(map[string]any{
				"version":                   map[string]any{"type": "string", "const": modelCognitionAttestationV1},
				"index_sha256":              map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$", "description": descriptions["model_cognition_attestation.index_sha256"]},
				"entry_sequence_sha256":     map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$", "description": descriptions["model_cognition_attestation.entry_sequence_sha256"]},
				"entry_count":               map[string]any{"type": "integer", "minimum": 0, "description": descriptions["model_cognition_attestation.entry_count"]},
				"challenge_digest":          map[string]any{"type": "string", "maxLength": 128},
				"reported_entry_count":      map[string]any{"type": "integer", "minimum": 0},
				"reported_estimated_tokens": map[string]any{"type": "integer", "minimum": 0},
				"coverage_percent":          percentSchema(),
				"system_mastery_percent": map[string]any{
					"type": "number", "minimum": 0, "maximum": 100,
					"description": descriptions["model_cognition_attestation.system_mastery_percent"],
				},
				"confidence_percent":  percentSchema(),
				"truncation_detected": map[string]any{"type": "boolean"},
				"unseen_sections":     stringArraySchema(),
				"uncertainty_reasons": stringArraySchema(),
				"challenge_answers": map[string]any{
					"type": "array", "maxItems": 12,
					"items": object(map[string]any{
						"ordinal":         map[string]any{"type": "integer", "minimum": 1, "description": descriptions["model_cognition_attestation.challenge_answers[].ordinal"]},
						"object_identity": map[string]any{"type": "string", "maxLength": 4096, "description": descriptions["model_cognition_attestation.challenge_answers[].object_identity"]},
						"tag":             map[string]any{"type": "string", "maxLength": 256},
						"core_f":          map[string]any{"type": "string", "maxLength": 16384},
					}, "ordinal", "object_identity", "tag", "core_f"),
				},
			}, "version", "index_sha256", "entry_sequence_sha256", "entry_count", "challenge_digest", "reported_entry_count", "reported_estimated_tokens", "coverage_percent", "system_mastery_percent", "confidence_percent", "truncation_detected", "unseen_sections", "uncertainty_reasons", "challenge_answers"),
			"cursor": stringProperty("cursor"),
			"probe":  booleanProperty("probe"),
			"probe_answers": object(map[string]any{
				"version": map[string]any{"type": "string", "const": cognitionProbeV1},
				"digest":  map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
				"answers": map[string]any{
					"type": "array", "maxItems": 4,
					"items": object(map[string]any{
						"ordinal":         map[string]any{"type": "integer", "minimum": 1},
						"object_identity": map[string]any{"type": "string", "maxLength": 4096},
						"tag":             map[string]any{"type": "string", "maxLength": 256},
						"core_f":          map[string]any{"type": "string", "maxLength": 16384},
					}, "ordinal", "object_identity", "tag", "core_f"),
				},
			}, "version", "digest", "answers"),
		}), nil
	}

	return nil, fmt.Errorf("%s", mcpMessage("mcp.asset.input_schema_unknown_tool", toolName))
}

func percentSchema() map[string]any {
	return map[string]any{"type": "number", "minimum": 0, "maximum": 100}
}

func stringArraySchema() map[string]any {
	return map[string]any{
		"type": "array", "maxItems": 64,
		"items": map[string]any{"type": "string", "maxLength": 1024},
	}
}

// mcpContract按真正使用点加载一个标量宿主合同。生产路径必须在任何副作用
// 之前调用validateMCPContracts；本函数自身仍不panic，也不回退到旧正文。
func mcpContract(id textassets.ID) string {
	value, err := textassets.RenderScalar(textassets.ActiveLocale(), id, nil)
	if err != nil {
		return mcpMessage("mcp.asset.load_failed", id, localeSafeMCPDetail(err.Error()))
	}

	return value
}

func validateMCPContracts(ids ...textassets.ID) error {
	for _, id := range ids {
		if _, err := textassets.RenderScalar(textassets.ActiveLocale(), id, nil); err != nil {
			return fmt.Errorf("%s", mcpMessage("mcp.asset.load_failed", id, localeSafeMCPDetail(err.Error())))
		}
	}

	return nil
}
