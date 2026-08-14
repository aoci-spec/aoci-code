package mcptools

import (
	"sort"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

func repairableImpactFindings(findings []cognition.RepairFinding) bool {
	if len(findings) == 0 {
		return false
	}
	for _, finding := range findings {
		if finding.CandidateIndex <= 0 {
			return false
		}
		switch finding.Code {
		case "impact_candidate_fras_invalid",
			"impact_candidate_tag_dictionary_violation",
			"impact_candidate_tag_not_compact",
			"impact_candidate_relation_not_canonical",
			"impact_candidate_volume_mismatch",
			"impact_object_identity_invalid",
			"impact_candidate_duplicate":
		default:
			return false
		}
	}
	return true
}

// LocalizeRepairFindings completes the presentation-only fields and applies
// the shared machine ordering without changing candidate semantics.
func LocalizeRepairFindings(findings []cognition.RepairFinding) []cognition.RepairFinding {
	result := append([]cognition.RepairFinding{}, findings...)
	for index := range result {
		finding := &result[index]
		if finding.RuleCode == "" {
			finding.RuleCode = finding.Code
		}
		if finding.Cause == "" {
			finding.Cause = finding.Message
		}
		localizeFRASCause(finding)
		finding.Message = finding.Cause
		finding.SafeRepairAction = safeRepairAction(*finding)
	}
	cognition.SortRepairFindings(result)
	return result
}

func localizeFRASCause(finding *cognition.RepairFinding) {
	if finding == nil {
		return
	}
	expected := repairMachineValue(finding.Expected)
	actual := repairMachineValue(finding.Actual)
	switch {
	case strings.HasSuffix(finding.RuleCode, "_too_many_items"):
		finding.Cause = writeMessage("entry.repair.cause.too_many_items", finding.Field, actual, expected)
	case strings.HasSuffix(finding.RuleCode, "_too_long"):
		finding.Cause = writeMessage("entry.repair.cause.too_long", finding.Field, actual, expected)
	case strings.HasSuffix(finding.RuleCode, "_empty"):
		finding.Cause = writeMessage("entry.repair.cause.empty", finding.Field)
	case finding.RuleCode == "fras_structure_invalid":
		finding.Cause = writeMessage("entry.repair.cause.structure")
	case finding.RuleCode == "fras_tag_invalid":
		finding.Cause = writeMessage("entry.repair.cause.tag")
	case finding.RuleCode == "impact_candidate_tag_not_compact":
		finding.Cause = writeMessage("entry.repair.cause.tag_compact", finding.Actual)
	case finding.RuleCode == "entry_field_budget_exceeded":
		finding.Cause = writeMessage("entry.repair.cause.field_budget", finding.Field,
			repairMachineValue(finding.Actual), repairMachineValue(finding.Expected))
	case finding.RuleCode == "impact_candidate_relation_not_canonical":
		finding.Cause = writeMessage("entry.repair.cause.relation_canonical", finding.Actual)
	case finding.RuleCode == "impact_object_identity_invalid":
		finding.Cause = writeMessage("entry.repair.cause.identity")
	case finding.RuleCode == "impact_candidate_volume_mismatch":
		finding.Cause = writeMessage("entry.repair.cause.volume")
	case finding.RuleCode == "impact_candidate_tag_dictionary_violation" && finding.Cause == "":
		finding.Cause = writeMessage("entry.repair.cause.tag_dictionary")
	case finding.RuleCode == "impact_candidate_duplicate":
		finding.Cause = writeMessage("entry.repair.cause.duplicate")
	case finding.RuleCode == "code_candidate_path_mismatch":
		finding.Cause = writeMessage("entry.repair.cause.code_path")
	case finding.RuleCode == "code_candidate_id_mismatch":
		finding.Cause = writeMessage("entry.repair.cause.code_candidate_id")
	case finding.RuleCode == "code_candidate_source_sha256_mismatch":
		finding.Cause = writeMessage("entry.repair.cause.code_source_sha256")
	case finding.RuleCode == "code_candidate_batch_id_mismatch":
		finding.Cause = writeMessage("entry.repair.cause.code_batch_id")
	}
}

func safeRepairAction(finding cognition.RepairFinding) string {
	limit := repairMachineValue(finding.Expected)
	switch finding.RuleCode {
	case "fras_f_too_long":
		return writeMessage("entry.repair.action.f_runes", limit)
	case "fras_r_too_many_items":
		return writeMessage("entry.repair.action.r_items", limit)
	case "fras_r_too_long":
		return writeMessage("entry.repair.action.r_runes", limit)
	case "fras_a_too_many_items":
		return writeMessage("entry.repair.action.a_items", limit)
	case "fras_a_too_long":
		return writeMessage("entry.repair.action.a_runes", limit)
	case "fras_s_too_long":
		return writeMessage("entry.repair.action.s_runes", limit)
	case "fras_structure_invalid", "fras_f_empty", "fras_r_empty", "fras_a_empty", "fras_s_empty":
		return writeMessage("entry.repair.action.structure", finding.Field)
	case "fras_tag_invalid", "impact_candidate_tag_not_compact":
		return writeMessage("entry.repair.action.tag")
	case "impact_candidate_relation_not_canonical":
		return writeMessage("entry.repair.action.r_relation")
	case "entry_field_budget_exceeded":
		return writeMessage("entry.repair.action.field_budget", finding.Field, repairMachineValue(finding.Expected))
	case "impact_object_identity_invalid":
		return writeMessage("entry.repair.action.identity")
	case "impact_candidate_volume_mismatch":
		return writeMessage("entry.repair.action.volume")
	case "impact_candidate_tag_dictionary_violation", "object_tag_dictionary_violation":
		return writeMessage("entry.repair.action.tag")
	case "impact_candidate_duplicate":
		return writeMessage("entry.repair.action.duplicate")
	case "code_candidate_path_mismatch":
		return writeMessage("entry.repair.action.code_path")
	case "code_candidate_id_mismatch":
		return writeMessage("entry.repair.action.code_candidate_id")
	case "code_candidate_source_sha256_mismatch":
		return writeMessage("entry.repair.action.code_source_sha256")
	case "code_candidate_batch_id_mismatch":
		return writeMessage("entry.repair.action.code_batch_id")
	default:
		return writeMessage("entry.repair.action.candidate", finding.Field)
	}
}

func repairMachineValue(value string) int {
	separator := strings.LastIndex(value, "=")
	if separator < 0 || separator == len(value)-1 {
		return 0
	}
	parsed, _ := strconv.Atoi(value[separator+1:])
	return parsed
}

func repairRetryScope(findings []cognition.RepairFinding) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, finding := range findings {
		if finding.Field == "code_batch_id" {
			continue
		}
		identity := finding.CanonicalObjectIdentity
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		result = append(result, identity)
	}
	sort.Strings(result)
	return result
}

// RepairRetryScope returns the deterministic canonical object identities that
// may be repaired before the caller resubmits the unchanged complete batch.
func RepairRetryScope(findings []cognition.RepairFinding) []string {
	return repairRetryScope(findings)
}
