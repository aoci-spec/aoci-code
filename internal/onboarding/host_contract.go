package onboarding

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	hostOriginPlaceholder = "<HOST_ASSERT_REQUIRED_ORIGIN>"
	hostRunIDPlaceholder  = "<HOST_ISSUED_AUTHORING_RUN_ID>"
)

// HostCommand is the executable machine command associated with one live
// Onboarding action. Arguments intentionally exclude Executable so a Host can
// invoke the process without parsing DisplayCommand.
type HostCommand struct {
	Executable           string   `json:"executable"`
	Arguments            []string `json:"arguments"`
	DisplayCommand       string   `json:"display_command"`
	SuggestedRequestFile string   `json:"suggested_request_file,omitempty"`
}

// NextActionContract binds one Host action and its applicable action payload
// schema to the active Session, Plan, and, when applicable, immutable
// Authoring Batch.
type NextActionContract struct {
	Version                        string       `json:"version"`
	Action                         string       `json:"action"`
	SchemaVersion                  string       `json:"schema_version"`
	OnboardingSessionID            string       `json:"onboarding_session_id"`
	PlanID                         string       `json:"plan_id"`
	BatchID                        string       `json:"batch_id,omitempty"`
	ExpectedPreimage               string       `json:"expected_preimage"`
	Command                        *HostCommand `json:"command,omitempty"`
	TTYRequired                    bool         `json:"tty_required"`
	AutomaticallyRetryable         bool         `json:"automatically_retryable"`
	TransportSchemaCorrectionLimit int          `json:"transport_schema_correction_limit"`
	SuccessNextAction              string       `json:"success_next_action"`
	FormalWritesStarted            bool         `json:"formal_writes_started"`
}

// CandidateDraftRequest describes the Host-owned Candidate payload that must
// be authored before AOCI can compute its provenance-excluding binding.
type CandidateDraftRequest struct {
	SchemaVersion                   string                        `json:"schema_version"`
	UnknownFieldsForbidden          bool                          `json:"unknown_fields_forbidden"`
	SemanticAuthoringProvenanceMode string                        `json:"semantic_authoring_provenance_mode"`
	PlanArtifact                    string                        `json:"plan_artifact"`
	Template                        cognitionplan.LayoutCandidate `json:"template"`
}

// CandidateBinding is a read-only projection. ProvenanceTemplate echoes the
// declaration already supplied by the Host and adds only the deterministic
// Candidate payload digest; it does not assert model authoring on AOCI's behalf.
type CandidateBinding struct {
	Version                      string                                      `json:"version"`
	OnboardingSessionID          string                                      `json:"onboarding_session_id"`
	PlanID                       string                                      `json:"plan_id"`
	CandidatePayloadSHA256       string                                      `json:"candidate_payload_sha256"`
	SemanticAuthoringRequirement *cognitionplan.SemanticAuthoringRequirement `json:"semantic_authoring_requirement"`
	ProvenanceTemplate           cognitionplan.SemanticAuthoringProvenance   `json:"semantic_authoring_provenance_template"`
	HostDeclarationEchoed        bool                                        `json:"host_declaration_echoed"`
	SemanticGenerated            bool                                        `json:"semantic_generated"`
	NextActionContract           *NextActionContract                         `json:"next_action_contract,omitempty"`
	SessionPreimageSHA256        string                                      `json:"-"`
}

// ContractError preserves the outer cognition_onboarding_invalid envelope
// while providing a precise, zero-write field diagnostic in Details.
type ContractError struct {
	Stage               string   `json:"failed_stage"`
	Field               string   `json:"failed_field,omitempty"`
	CauseCode           string   `json:"cause_code"`
	Expected            string   `json:"expected,omitempty"`
	Actual              string   `json:"actual,omitempty"`
	AllowedFields       []string `json:"allowed_fields,omitempty"`
	FormalWritesStarted bool     `json:"formal_writes_started"`
	Cause               error    `json:"-"`
}

func (e *ContractError) Error() string {
	if e == nil {
		return "onboarding_contract_invalid"
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.CauseCode
}

// BuildHostCommand keeps the argv contract authoritative and treats the
// display form as presentation only.
func BuildHostCommand(executable string, arguments []string, suggestedRequestFile string) *HostCommand {
	args := append([]string{}, arguments...)
	return &HostCommand{
		Executable: executable, Arguments: args,
		DisplayCommand:       displayHostCommand(runtime.GOOS, executable, args),
		SuggestedRequestFile: suggestedRequestFile,
	}
}

// BuildOnboardingNextActionContract derives a resumable command from the
// persisted Session without modifying or upgrading it.
func BuildOnboardingNextActionContract(root, executable string, session *Session) *NextActionContract {
	if session == nil || session.Version != SessionVersion {
		return nil
	}
	action := session.NextAction
	args := []string{"--repo", root, "cognition", "onboard", "status", "--json"}
	success := session.NextAction
	schema := session.Version
	switch session.NextAction {
	case "authoring_next":
		maxObjects, maxEvidenceBytes := effectiveAuthoringLimits(session.ActiveAuthoringBatch, 0, 0)
		action = "authoring_next"
		args = []string{"--repo", root, "cognition", "onboard", "next", "--max-objects", fmt.Sprintf("%d", maxObjects),
			"--max-evidence-bytes", fmt.Sprintf("%d", maxEvidenceBytes), "--json"}
		schema = BatchVersion
		success = "submit_authoring_completion"
	case "preview":
		action = "candidate_request"
		args = []string{"--repo", root, "cognition", "onboard", "next", "--json"}
		schema = machinecontract.CognitionLayoutCandidateV1
		success = "bind_candidate_payload"
	case "prepare":
		if EffectiveAutomationPolicy(session).Mode == config.AutomationModeReview {
			action = "prepare"
			args = []string{"--repo", root, "cognition", "onboard", "prepare", "--json"}
			schema = machinecontract.HostInteractionV1
			success = "human_tty_digest_confirmation"
		} else {
			action = "resume"
			args = []string{"--repo", root, "cognition", "onboard", "resume", "--json"}
			success = "none"
		}
	case "human_tty_digest_confirmation":
		action = "human_tty_digest_confirmation"
		args = []string{"--repo", root, "cognition", "onboard", "prepare", "--json"}
		schema = machinecontract.HostInteractionV1
		success = "human_tty_digest_confirmation"
	case "auto_apply":
		action = "resume"
		args = []string{"--repo", root, "cognition", "onboard", "resume", "--json"}
		success = "none"
	case "none", "aborted":
		args = nil
		success = "none"
	}
	// apply_pending is the durable transaction phase. It outranks a stale
	// NextAction projection left by Host termination between transaction work
	// and Session persistence, and always routes through the idempotent resume
	// kernel.
	if session.TransactionState == "apply_pending" {
		action = "resume"
		args = []string{"--repo", root, "cognition", "onboard", "resume", "--json"}
		schema = session.Version
		success = "none"
	}
	contract := &NextActionContract{
		Version: machinecontract.CognitionOnboardingNextActionV1,
		Action:  action, SchemaVersion: schema,
		OnboardingSessionID: session.OnboardingSessionID, PlanID: session.PlanID,
		ExpectedPreimage: session.PreimageSHA256,
		TTYRequired:      false, AutomaticallyRetryable: false,
		TransportSchemaCorrectionLimit: 1, SuccessNextAction: success,
		FormalWritesStarted: session.TransactionState == "applied",
	}
	if session.ActiveAuthoringBatch != nil {
		contract.BatchID = session.ActiveAuthoringBatch.BatchID
	}
	if len(args) != 0 {
		contract.Command = BuildHostCommand(executable, args, "")
	}
	return contract
}

func completionTemplate(session *Session, batchID string, tasks []cognitionplan.AuthoringTask, requirement *cognitionplan.SemanticAuthoringRequirement) *Completion {
	if session == nil || requirement == nil || len(tasks) == 0 {
		return nil
	}
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	declaration := &cognitionplan.SemanticAuthoringDeclaration{
		Version: machinecontract.SemanticAuthoringProvenanceV1,
		Origin:  hostOriginPlaceholder, AuthoringRunID: hostRunIDPlaceholder,
		DiscoveryPlanID:       requirement.DiscoveryPlanID,
		EvidenceBindingSHA256: requirement.EvidenceBindingSHA256,
	}
	if session.SemanticAuthoringDeclaration != nil {
		copy := *session.SemanticAuthoringDeclaration
		declaration = &copy
	}
	return &Completion{
		Version: CompletionVersion, SessionID: session.OnboardingSessionID,
		BatchID: batchID, CompletedTasks: taskIDs,
		SemanticAuthoringDeclaration: declaration,
	}
}

func candidateDraftRequest(session *Session, plan *cognitionplan.Plan) *CandidateDraftRequest {
	if session == nil || plan == nil {
		return nil
	}
	assets := make([]cognitionplan.CandidateAsset, 0, len(plan.CandidateFrameworks))
	for _, framework := range plan.CandidateFrameworks {
		assets = append(assets, cognitionplan.CandidateAsset{AssetID: framework.AssetID, Path: framework.Path, Content: framework.Framework})
	}
	return &CandidateDraftRequest{
		SchemaVersion:                   machinecontract.CognitionLayoutCandidateV1,
		UnknownFieldsForbidden:          true,
		SemanticAuthoringProvenanceMode: "omit_until_candidate_binding",
		PlanArtifact:                    session.PlanArtifact,
		Template: cognitionplan.LayoutCandidate{
			Version: machinecontract.CognitionLayoutCandidateV1,
			PlanID:  plan.PlanID, Assets: assets,
			MappingResolutions: []cognitionplan.MappingResolution{},
		},
	}
}

// BindCandidate validates the complete Host-authored payload with the exact
// persisted Host declaration, but does not save the Candidate, Preview, or
// Session. The temporary receipt exists only to validate every non-provenance
// candidate rule before the Host explicitly submits that receipt.
func BindCandidate(root string, candidateData []byte) (*CandidateBinding, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if session.Version != SessionVersion {
		return nil, contractFailure("bind_candidate_payload", "version", "onboarding_candidate_binding_requires_v2", SessionVersion, session.Version, nil)
	}
	if len(session.PendingAuthoringTargets) != 0 || session.SemanticAuthoringDeclaration == nil {
		return nil, contractFailure("bind_candidate_payload", "completed_task_ids", "onboarding_authoring_incomplete", "all authoring tasks completed", fmt.Sprintf("pending=%d", len(session.PendingAuthoringTargets)), nil)
	}
	plan, err := loadPlan(root, session)
	if err != nil {
		return nil, err
	}
	candidate, err := cognitionplan.DecodeCandidate(candidateData)
	if err != nil {
		return nil, contractFailure("bind_candidate_payload", "candidate_payload", "onboarding_candidate_payload_invalid", machinecontract.CognitionLayoutCandidateV1, err.Error(), err)
	}
	if candidate.SemanticAuthoringProvenance != nil {
		return nil, contractFailure("bind_candidate_payload", "semantic_authoring_provenance", "onboarding_candidate_payload_provenance_present", "field omitted until binding", "present", nil)
	}
	if candidate.PlanID != plan.PlanID {
		return nil, contractFailure("bind_candidate_payload", "plan_id", "onboarding_candidate_plan_mismatch", plan.PlanID, candidate.PlanID, nil)
	}
	payloadSHA := cognitionplan.CandidatePayloadSHA256(candidate)
	declaration := session.SemanticAuthoringDeclaration
	provenance := cognitionplan.SemanticAuthoringProvenance{
		Version: declaration.Version, Origin: declaration.Origin,
		AuthoringRunID: declaration.AuthoringRunID, PlanID: declaration.DiscoveryPlanID,
		EvidenceBindingSHA256:  declaration.EvidenceBindingSHA256,
		CandidatePayloadSHA256: payloadSHA,
	}
	validationCandidate := *candidate
	validationCandidate.SemanticAuthoringProvenance = &provenance
	preview, err := cognitionplan.ValidateCandidate(root, plan, &validationCandidate)
	if err != nil {
		return nil, contractFailure("bind_candidate_payload", "candidate_payload", "onboarding_candidate_payload_validation_failed", "valid candidate payload", err.Error(), err)
	}
	if preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil || len(preview.Risks) != 0 {
		riskData, _ := json.Marshal(preview.Risks)
		return nil, contractFailure("bind_candidate_payload", "candidate_payload", "onboarding_candidate_payload_not_ready", "zero non-provenance risks", string(riskData), nil)
	}
	return &CandidateBinding{
		Version:             machinecontract.CognitionOnboardingCandidateBindingV1,
		OnboardingSessionID: session.OnboardingSessionID, PlanID: plan.PlanID,
		CandidatePayloadSHA256:       payloadSHA,
		SemanticAuthoringRequirement: cognitionplan.SemanticAuthoringRequirementForPlan(plan, candidate),
		ProvenanceTemplate:           provenance, HostDeclarationEchoed: true, SemanticGenerated: false,
		SessionPreimageSHA256: session.PreimageSHA256,
	}, nil
}

func contractFailure(stage, field, code, expected, actual string, cause error) *ContractError {
	return &ContractError{Stage: stage, Field: field, CauseCode: code, Expected: expected, Actual: actual, FormalWritesStarted: false, Cause: cause}
}

func displayHostCommand(goos, executable string, arguments []string) string {
	parts := make([]string, 0, len(arguments)+1)
	if goos == "windows" {
		parts = append(parts, "& "+quotePowerShell(executable))
		for _, argument := range arguments {
			parts = append(parts, quotePowerShell(argument))
		}
		return strings.Join(parts, " ")
	}
	parts = append(parts, quotePOSIX(executable))
	for _, argument := range arguments {
		parts = append(parts, quotePOSIX(argument))
	}
	return strings.Join(parts, " ")
}

func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
