package mcptools

import "github.com/aoci-spec/aoci-code/internal/machinecontract"

type overviewCognitionLevel struct {
	Level   int
	State   string
	Message string
}

func noOverviewCognitionLevel() overviewCognitionLevel {
	return newOverviewCognitionLevel(
		machinecontract.CognitionLevelNoCognition,
		machinecontract.CognitionLevelStateNoCognition,
	)
}

func assessOverviewCognitionLevel(
	indexLoaded bool,
	deliveryIntegrity, modelAttestation string,
	governanceAligned bool,
) overviewCognitionLevel {
	if !indexLoaded {
		return noOverviewCognitionLevel()
	}
	level := machinecontract.CognitionLevelIndexLoaded
	state := machinecontract.CognitionLevelStateIndexLoaded
	if deliveryIntegrity == deliveryIntegrityConfirmed {
		level = machinecontract.CognitionLevelDeliveryVerified
		state = machinecontract.CognitionLevelStateDeliveryVerified
	}
	if level >= machinecontract.CognitionLevelDeliveryVerified &&
		modelAttestation == modelAttestationPass {
		level = machinecontract.CognitionLevelVerified
		state = machinecontract.CognitionLevelStateVerified
		if governanceAligned {
			level = machinecontract.CognitionLevelGoverned
			state = machinecontract.CognitionLevelStateGoverned
		}
	}
	return newOverviewCognitionLevel(level, state)
}

func newOverviewCognitionLevel(level int, state string) overviewCognitionLevel {
	return overviewCognitionLevel{
		Level: level, State: state,
		Message: overviewCognitionLevelMessage(state),
	}
}

func overviewCognitionLevelMessage(state string) string {
	switch state {
	case machinecontract.CognitionLevelStateNoCognition:
		return mcpMessage("overview.cognition_level.no_cognition.message")
	case machinecontract.CognitionLevelStateIndexLoaded:
		return mcpMessage("overview.cognition_level.index_loaded.message")
	case machinecontract.CognitionLevelStateDeliveryVerified:
		return mcpMessage("overview.cognition_level.delivery_verified.message")
	case machinecontract.CognitionLevelStateVerified:
		return mcpMessage("overview.cognition_level.cognition_verified.message")
	case machinecontract.CognitionLevelStateGoverned:
		return mcpMessage("overview.cognition_level.cognition_governed.message")
	default:
		return mcpMessage("overview.cognition_level.no_cognition.message")
	}
}
