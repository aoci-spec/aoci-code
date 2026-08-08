package cognitionplan

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// SemanticAuthoringEvidenceBindingSHA256 binds the exact source/evidence
// identities and ordered authoring task contract without copying source bytes
// into the Candidate receipt.
func SemanticAuthoringEvidenceBindingSHA256(plan *Plan) string {
	identity := newIdentity("semantic-authoring-evidence")
	identity.field("inventory_identity", plan.InventoryIdentity)
	identity.field("source_evidence_identity", plan.SourceEvidenceIdentity)
	for _, task := range plan.AuthoringTasks {
		identity.field("task_id", task.TaskID)
		identity.field("asset_id", task.AssetID)
		identity.field("object_kind", task.ObjectKind)
		identity.field("object_ref", task.ObjectRef)
		for _, evidenceRef := range task.EvidenceRefs {
			identity.field("evidence_ref", evidenceRef)
		}
		for _, field := range task.RequiredSemantic {
			identity.field("required_semantic_field", field)
		}
	}
	return identity.sum()
}

// SemanticAuthoringRequirementForPlan exposes only deterministic validation
// inputs. The Host still supplies the authoring run, origin, and final receipt.
func SemanticAuthoringRequirementForPlan(plan *Plan, candidate *LayoutCandidate) *SemanticAuthoringRequirement {
	requirement := &SemanticAuthoringRequirement{
		Version:                  machinecontract.SemanticAuthoringRequirementV1,
		RequiredOrigin:           machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunIDRequired:   true,
		DiscoveryPlanID:          plan.PlanID,
		EvidenceBindingSHA256:    SemanticAuthoringEvidenceBindingSHA256(plan),
		CandidatePayloadRequired: true,
	}
	if candidate != nil {
		requirement.CandidatePayloadSHA256 = CandidatePayloadSHA256(candidate)
	}
	return requirement
}

// ValidateSemanticAuthoringDeclaration validates a Host-provided declaration
// without converting it into a Candidate receipt.
func ValidateSemanticAuthoringDeclaration(plan *Plan, declaration *SemanticAuthoringDeclaration) error {
	if declaration == nil {
		return fmt.Errorf("semantic_authoring_declaration_missing")
	}
	if declaration.Version != machinecontract.SemanticAuthoringProvenanceV1 {
		return fmt.Errorf("semantic_authoring_declaration_version_invalid")
	}
	if declaration.Origin != machinecontract.SemanticAuthoringOriginHostModel {
		return fmt.Errorf("semantic_authoring_declaration_origin_invalid")
	}
	if !validAuthoringRunID(declaration.AuthoringRunID) {
		return fmt.Errorf("semantic_authoring_declaration_run_id_invalid")
	}
	if declaration.DiscoveryPlanID != plan.PlanID {
		return fmt.Errorf("semantic_authoring_declaration_plan_mismatch")
	}
	if !validSHA256(declaration.EvidenceBindingSHA256) || declaration.EvidenceBindingSHA256 != SemanticAuthoringEvidenceBindingSHA256(plan) {
		return fmt.Errorf("semantic_authoring_declaration_evidence_mismatch")
	}
	return nil
}

// SemanticAuthoringDeclarationMatchesReceipt requires the final Candidate
// receipt to preserve the declaration persisted during authoring Completion.
func SemanticAuthoringDeclarationMatchesReceipt(declaration *SemanticAuthoringDeclaration, receipt *SemanticAuthoringProvenance) bool {
	return declaration != nil && receipt != nil &&
		declaration.Version == receipt.Version && declaration.Origin == receipt.Origin &&
		declaration.AuthoringRunID == receipt.AuthoringRunID && declaration.DiscoveryPlanID == receipt.PlanID &&
		declaration.EvidenceBindingSHA256 == receipt.EvidenceBindingSHA256
}

// CandidatePayloadSHA256 is the legacy Candidate v1 identity input excluding
// provenance. Keeping this computation separate avoids a self-referential
// receipt while binding every exact candidate asset and mapping resolution.
func CandidatePayloadSHA256(candidate *LayoutCandidate) string {
	return candidatePayloadEncoder(candidate).sum()
}

func candidatePayloadEncoder(candidate *LayoutCandidate) *identityEncoder {
	identity := newIdentity("layout-candidate")
	identity.field("version", candidate.Version)
	identity.field("plan_id", candidate.PlanID)
	for _, asset := range candidate.Assets {
		identity.field("asset_id", asset.AssetID)
		identity.field("asset_path", asset.Path)
		identity.field("asset_sha256", hashBytes([]byte(asset.Content)))
	}
	for _, resolution := range candidate.MappingResolutions {
		identity.field("mapping_unit", resolution.UnitID)
		identity.field("mapping_target_asset", resolution.TargetAsset)
		identity.field("mapping_target_ref", resolution.TargetRef)
		identity.field("mapping_reviewer", resolution.Reviewer)
		identity.field("mapping_reviewed", fmt.Sprintf("%t", resolution.SemanticReviewed))
	}
	return identity
}

func candidateIdentity(candidate *LayoutCandidate) string {
	identity := candidatePayloadEncoder(candidate)
	if candidate.SemanticAuthoringProvenance != nil {
		identity.field("semantic_authoring_provenance_sha256", semanticAuthoringReceiptSHA256(candidate.SemanticAuthoringProvenance))
	}
	return identity.sum()
}

func semanticAuthoringReceiptSHA256(value *SemanticAuthoringProvenance) string {
	identity := newIdentity("semantic-authoring-provenance")
	identity.field("version", value.Version)
	identity.field("origin", value.Origin)
	identity.field("authoring_run_id", value.AuthoringRunID)
	identity.field("plan_id", value.PlanID)
	identity.field("evidence_binding_sha256", value.EvidenceBindingSHA256)
	identity.field("candidate_payload_sha256", value.CandidatePayloadSHA256)
	return identity.sum()
}

func validateSemanticAuthoringProvenance(plan *Plan, candidate *LayoutCandidate) (*SemanticAuthoringVerification, []Risk) {
	if plan.Operation != OperationBootstrap {
		return nil, nil
	}
	receipt := candidate.SemanticAuthoringProvenance
	if receipt == nil {
		return nil, []Risk{{Code: "semantic_authoring_provenance_missing"}}
	}

	risks := make([]Risk, 0, 6)
	if receipt.Version != machinecontract.SemanticAuthoringProvenanceV1 {
		risks = append(risks, Risk{Code: "semantic_authoring_provenance_version_invalid"})
	}
	if receipt.Origin != machinecontract.SemanticAuthoringOriginHostModel {
		risks = append(risks, Risk{Code: "semantic_authoring_origin_invalid"})
	}
	if !validAuthoringRunID(receipt.AuthoringRunID) {
		risks = append(risks, Risk{Code: "semantic_authoring_run_id_invalid"})
	}
	if receipt.PlanID != plan.PlanID {
		risks = append(risks, Risk{Code: "semantic_authoring_plan_mismatch"})
	}
	evidenceBinding := SemanticAuthoringEvidenceBindingSHA256(plan)
	if !validSHA256(receipt.EvidenceBindingSHA256) || receipt.EvidenceBindingSHA256 != evidenceBinding {
		risks = append(risks, Risk{Code: "semantic_authoring_evidence_mismatch"})
	}
	candidatePayload := CandidatePayloadSHA256(candidate)
	if !validSHA256(receipt.CandidatePayloadSHA256) || receipt.CandidatePayloadSHA256 != candidatePayload {
		risks = append(risks, Risk{Code: "semantic_authoring_candidate_mismatch"})
	}
	if len(risks) != 0 {
		return nil, risks
	}
	return &SemanticAuthoringVerification{
		Version: receipt.Version, Status: machinecontract.SemanticAuthoringStatusVerified,
		Origin: receipt.Origin, AuthoringRunID: receipt.AuthoringRunID, DiscoveryPlanID: receipt.PlanID,
		EvidenceBindingSHA256: evidenceBinding, CandidatePayloadSHA256: candidatePayload,
		ReceiptSHA256: semanticAuthoringReceiptSHA256(receipt),
	}, risks
}

func validAuthoringRunID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
