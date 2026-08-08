package mcptools

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type overviewCognitionStateV2 struct {
	Version                         string   `json:"version"`
	ModelCognitionLevel             int      `json:"model_cognition_level"`
	ModelCognitionLevelState        string   `json:"model_cognition_level_state"`
	DeliveryVerified                bool     `json:"delivery_verified"`
	ModelCognitionUsable            bool     `json:"model_cognition_usable"`
	ModelCognitionUsabilityStatus   string   `json:"model_cognition_usability_status"`
	StrictAttestationVerified       bool     `json:"strict_attestation_verified"`
	StrictAttestationStatus         string   `json:"strict_attestation_status"`
	StrictAttestationFailureReasons []string `json:"strict_attestation_failure_reasons"`
	GovernanceAligned               bool     `json:"governance_aligned"`
	CurrentSystemCognitionReliable  bool     `json:"current_system_cognition_reliable"`
	ProtocolCompleted               bool     `json:"protocol_completed"`
}

func validateOverviewCognitionStateVersion(version string) error {
	if version == "" || version == machinecontract.CognitionStateV2 {
		return nil
	}
	return fmt.Errorf("overview_cognition_state_version_unknown")
}

func cognitionStateV2Requested(input overviewIn) bool {
	return input.CognitionStateVersion == machinecontract.CognitionStateV2
}

func assessOverviewCognitionStateV2(
	indexLoaded, fullScope, governanceAligned, protocolCompleted bool,
	attestation overviewAttestationResult,
) overviewCognitionStateV2 {
	deliveryVerified := attestation.DeliveryIntegrity == deliveryIntegrityConfirmed
	modelUsable := deliveryVerified && attestation.ReportProvided && attestation.EnvelopeValid &&
		attestation.ReportedEntryCountMatch && attestation.ReportedTokensMatch &&
		attestation.CoveragePercent >= attestationCoverageComplete && !attestation.TruncationDetected &&
		attestation.SystemMasteryPercent >= attestationMasteryComplete

	usabilityStatus := machinecontract.CognitionUsabilityNotEstablished
	if modelUsable {
		usabilityStatus = machinecontract.CognitionUsabilityUsable
	} else if deliveryVerified && attestation.ReportProvided {
		usabilityStatus = machinecontract.CognitionUsabilityUnusable
	}

	strictVerified := attestation.ModelAttestation == modelAttestationPass
	strictStatus := strictAttestationStatus(attestation.ModelAttestation)
	reasons := append([]string{}, attestation.StrictFailureReasons...)
	if strictVerified {
		reasons = []string{}
	} else if strictStatus == machinecontract.StrictAttestationNotProvided && len(reasons) == 0 {
		reasons = []string{machinecontract.StrictAttestationReasonNotProvided}
	}

	level := machinecontract.CognitionStateV2LevelNoCognition
	levelState := machinecontract.CognitionStateV2LevelStateNoCognition
	if indexLoaded {
		level = machinecontract.CognitionStateV2LevelIndexLoaded
		levelState = machinecontract.CognitionStateV2LevelStateIndexLoaded
	}
	if indexLoaded && deliveryVerified {
		level = machinecontract.CognitionStateV2LevelDelivery
		levelState = machinecontract.CognitionStateV2LevelStateDelivery
	}
	if indexLoaded && modelUsable {
		level = machinecontract.CognitionStateV2LevelUsable
		levelState = machinecontract.CognitionStateV2LevelStateUsable
	}

	return overviewCognitionStateV2{
		Version:                         machinecontract.CognitionStateV2,
		ModelCognitionLevel:             level,
		ModelCognitionLevelState:        levelState,
		DeliveryVerified:                deliveryVerified,
		ModelCognitionUsable:            modelUsable,
		ModelCognitionUsabilityStatus:   usabilityStatus,
		StrictAttestationVerified:       strictVerified,
		StrictAttestationStatus:         strictStatus,
		StrictAttestationFailureReasons: reasons,
		GovernanceAligned:               governanceAligned,
		CurrentSystemCognitionReliable:  fullScope && modelUsable && strictVerified && governanceAligned,
		ProtocolCompleted:               protocolCompleted,
	}
}

func strictAttestationStatus(modelAttestation string) string {
	switch modelAttestation {
	case modelAttestationPass:
		return machinecontract.StrictAttestationVerified
	case modelAttestationPartial:
		return machinecontract.StrictAttestationPartial
	case modelAttestationFail:
		return machinecontract.StrictAttestationFailed
	default:
		return machinecontract.StrictAttestationNotProvided
	}
}

func overviewContextIsFullScope(ctx overviewRenderContext) bool {
	return ctx.EffectiveScope == cognition.ScopeAll ||
		ctx.EffectiveScope == machinecontract.CognitionScopeRepositoryFull
}
