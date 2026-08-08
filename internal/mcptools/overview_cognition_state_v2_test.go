package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestCognitionStateV2ProjectionKeepsProofAndGovernanceOutOfLevel(t *testing.T) {
	usableStrictFailure := overviewAttestationResult{
		DeliveryIntegrity:       deliveryIntegrityConfirmed,
		ModelAttestation:        modelAttestationFail,
		ReportProvided:          true,
		EnvelopeValid:           true,
		ReportedEntryCountMatch: true,
		ReportedTokensMatch:     true,
		CoveragePercent:         100,
		SystemMasteryPercent:    92,
		StrictFailureReasons:    []string{machinecontract.StrictAttestationReasonObjectIdentityMismatch},
		TruncationDetected:      false,
	}
	state := assessOverviewCognitionStateV2(true, true, true, true, usableStrictFailure)
	if state.ModelCognitionLevel != machinecontract.CognitionStateV2LevelUsable ||
		state.ModelCognitionLevelState != machinecontract.CognitionStateV2LevelStateUsable ||
		!state.DeliveryVerified || !state.ModelCognitionUsable ||
		state.StrictAttestationVerified || state.StrictAttestationStatus != machinecontract.StrictAttestationFailed ||
		!state.GovernanceAligned || state.CurrentSystemCognitionReliable || !state.ProtocolCompleted {
		t.Fatalf("strict proof failure was coupled back into the v2 cognition Level: %+v", state)
	}
	if len(state.StrictAttestationFailureReasons) != 1 ||
		state.StrictAttestationFailureReasons[0] != machinecontract.StrictAttestationReasonObjectIdentityMismatch {
		t.Fatalf("strict failure reason projection changed: %+v", state.StrictAttestationFailureReasons)
	}

	strictPassDirty := usableStrictFailure
	strictPassDirty.ModelAttestation = modelAttestationPass
	strictPassDirty.StrictFailureReasons = nil
	state = assessOverviewCognitionStateV2(true, true, false, true, strictPassDirty)
	if state.ModelCognitionLevel != machinecontract.CognitionStateV2LevelUsable ||
		!state.ModelCognitionUsable || !state.StrictAttestationVerified ||
		state.GovernanceAligned || state.CurrentSystemCognitionReliable {
		t.Fatalf("governance was coupled back into the v2 cognition Level: %+v", state)
	}
}

func TestCognitionStateV2ProjectionLevelsOnlyAvailability(t *testing.T) {
	tests := []struct {
		name        string
		indexLoaded bool
		attestation overviewAttestationResult
		wantLevel   int
		wantState   string
		wantStatus  string
	}{
		{
			name: "no cognition", wantLevel: machinecontract.CognitionStateV2LevelNoCognition,
			wantState:  machinecontract.CognitionStateV2LevelStateNoCognition,
			wantStatus: machinecontract.CognitionUsabilityNotEstablished,
		},
		{
			name: "index loaded", indexLoaded: true,
			attestation: overviewAttestationResult{DeliveryIntegrity: deliveryIntegrityUnconfirmed},
			wantLevel:   machinecontract.CognitionStateV2LevelIndexLoaded,
			wantState:   machinecontract.CognitionStateV2LevelStateIndexLoaded,
			wantStatus:  machinecontract.CognitionUsabilityNotEstablished,
		},
		{
			name: "delivery verified", indexLoaded: true,
			attestation: overviewAttestationResult{DeliveryIntegrity: deliveryIntegrityConfirmed},
			wantLevel:   machinecontract.CognitionStateV2LevelDelivery,
			wantState:   machinecontract.CognitionStateV2LevelStateDelivery,
			wantStatus:  machinecontract.CognitionUsabilityNotEstablished,
		},
		{
			name: "report unusable", indexLoaded: true,
			attestation: overviewAttestationResult{
				DeliveryIntegrity: deliveryIntegrityConfirmed, ReportProvided: true,
				EnvelopeValid: true, ReportedEntryCountMatch: true, ReportedTokensMatch: true,
				CoveragePercent: 94, SystemMasteryPercent: 92,
			},
			wantLevel:  machinecontract.CognitionStateV2LevelDelivery,
			wantState:  machinecontract.CognitionStateV2LevelStateDelivery,
			wantStatus: machinecontract.CognitionUsabilityUnusable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := assessOverviewCognitionStateV2(test.indexLoaded, true, true, false, test.attestation)
			if state.ModelCognitionLevel != test.wantLevel ||
				state.ModelCognitionLevelState != test.wantState ||
				state.ModelCognitionUsabilityStatus != test.wantStatus {
				t.Fatalf("unexpected projection: %+v", state)
			}
		})
	}
}

func TestOverviewCognitionStateV2IsStrictlyOptInAndLeavesLegacyFieldsUnchanged(t *testing.T) {
	root := buildRepo(t)
	session := connectMCPClient(t, root)

	legacy, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	legacyText := resText(t, legacy)
	if strings.Contains(legacyText, "cognition_state_v2") {
		t.Fatalf("legacy Overview unexpectedly emitted the v2 projection:\n%s", legacyText)
	}

	projected, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{
			"cognition_state_version": machinecontract.CognitionStateV2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectedText := resText(t, projected)
	initialState := cognitionStateV2MetadataForTest(t, projectedText)
	if initialState.ModelCognitionLevel != machinecontract.CognitionStateV2LevelIndexLoaded ||
		initialState.DeliveryVerified || initialState.ModelCognitionUsable || initialState.ProtocolCompleted {
		t.Fatalf("initial projected Overview state is wrong: %+v", initialState)
	}
	for _, legacyField := range []string{
		"cognition_level: 1", "cognition_level_state: index_loaded",
		"model_full_cognition_reliable: false", "cognition_receipt:",
	} {
		if !strings.Contains(projectedText, legacyField) {
			t.Fatalf("projected Overview changed or removed legacy field %q:\n%s", legacyField, projectedText)
		}
	}

	confirmation := hostConfirmationFromOverview(t, projectedText)
	failed, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "aoci_overview", Arguments: map[string]any{
			"cognition_state_version":     machinecontract.CognitionStateV2,
			"host_delivery_confirmation":  confirmation,
			"model_cognition_attestation": legacyAttestationMapWithWrongAnswers(t, root, 1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	failedText := resText(t, failed)
	failedState := cognitionStateV2MetadataForTest(t, failedText)
	if failedState.ModelCognitionLevel != machinecontract.CognitionStateV2LevelUsable ||
		!failedState.ModelCognitionUsable || failedState.StrictAttestationVerified ||
		failedState.CurrentSystemCognitionReliable || !failedState.ProtocolCompleted {
		t.Fatalf("strict Challenge failure did not remain independent: %+v", failedState)
	}
	for _, legacyField := range []string{
		"model_attestation: fail", "cognition_level: 2",
		"cognition_level_state: delivery_verified", "model_full_cognition_reliable: false",
	} {
		if !strings.Contains(failedText, legacyField) {
			t.Fatalf("v2 projection changed legacy failure semantics %q:\n%s", legacyField, failedText)
		}
	}
}

func TestOverviewCognitionStateV2RejectsUnknownVersion(t *testing.T) {
	if validateOverviewCognitionStateVersion("") != nil ||
		validateOverviewCognitionStateVersion(machinecontract.CognitionStateV2) != nil {
		t.Fatal("supported Overview projection versions were rejected")
	}
	if validateOverviewCognitionStateVersion("cognition-state/v3") == nil {
		t.Fatal("unknown Overview projection version was accepted")
	}
}

func TestOverviewFinalChunkUsesV2AttestationInterpretationOnlyWhenRequested(t *testing.T) {
	legacyPrompt, legacyState := finalChunkCognitionPromptForTest(t, "")
	if legacyPrompt != attestationPrompt() || legacyState != nil {
		t.Fatalf("Legacy final Chunk changed without v2 opt-in: prompt=%q state=%+v", legacyPrompt, legacyState)
	}
	if strings.Contains(legacyPrompt, machinecontract.CognitionStateV2) ||
		strings.Contains(legacyPrompt, machinecontract.CognitionStateV2LevelStateUsable) {
		t.Fatalf("Legacy Attestation Prompt leaked v2 interpretation: %q", legacyPrompt)
	}

	v2Prompt, v2State := finalChunkCognitionPromptForTest(t, machinecontract.CognitionStateV2)
	if v2State == nil || v2State.Version != machinecontract.CognitionStateV2 ||
		!strings.HasPrefix(v2Prompt, legacyPrompt+" ") ||
		!strings.Contains(v2Prompt, machinecontract.CognitionStateV2) ||
		!strings.Contains(v2Prompt, machinecontract.CognitionStateV2LevelStateUsable) {
		t.Fatalf("v2 final Chunk did not add the projection-specific interpretation: prompt=%q state=%+v", v2Prompt, v2State)
	}
}

func finalChunkCognitionPromptForTest(
	t *testing.T,
	cognitionStateVersion string,
) (string, *overviewCognitionStateV2) {
	t.Helper()
	content, document := largeOverviewFixture(t)
	cursor := ""
	for calls := 0; calls < 100; calls++ {
		rendered := renderLegacyOverviewForTest(
			t, "/repo", "test", content, document,
			overviewIn{Cursor: cursor, CognitionStateVersion: cognitionStateVersion},
			machinecontract.OverviewChunkTokensDefault,
		)
		parts := strings.SplitN(rendered.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("Chunk body marker missing: %s", rendered.Output)
		}
		var metadata struct {
			Completed      bool                      `json:"completed"`
			NextCursor     string                    `json:"next_cursor"`
			Attestation    string                    `json:"attestation_prompt"`
			CognitionState *overviewCognitionStateV2 `json:"cognition_state_v2"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Completed {
			return metadata.Attestation, metadata.CognitionState
		}
		cursor = metadata.NextCursor
	}
	t.Fatal("Overview Chunk chain did not complete")
	return "", nil
}

func cognitionStateV2MetadataForTest(t *testing.T, output string) overviewCognitionStateV2 {
	t.Helper()
	encoded := overviewMetadataValue(t, output, "cognition_state_v2")
	var state overviewCognitionStateV2
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		t.Fatalf("decode cognition_state_v2: %v\n%s", err, output)
	}
	return state
}
