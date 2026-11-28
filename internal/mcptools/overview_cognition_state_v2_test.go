package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func checkpointV2ForTest(t *testing.T, root string, session *cognitionRefreshSession, owner string, input overviewIn) overviewCheckpointEnvelope {
	t.Helper()
	input.CheckOnly, input.CognitionStateVersion = true, machinecontract.CognitionStateV2
	result := sessionOverview(t, root, session, owner, input)
	var checkpoint overviewCheckpointEnvelope
	if result.IsError {
		t.Fatalf("checkpoint failed: %s", resText(t, result))
	}
	if err := json.Unmarshal([]byte(resText(t, result)), &checkpoint); err != nil || checkpoint.CognitionStateV2 == nil {
		t.Fatalf("checkpoint is missing its v2 projection: %v", err)
	}
	if checkpoint.FullTextIncluded || checkpoint.DeliveryMode != overviewDeliveryCheckpoint || checkpoint.Assessment.OverviewReads != 0 ||
		!checkpoint.CognitionStateV2.ProtocolCompleted {
		t.Fatalf("checkpoint delivered a body or failed to close the check: %+v", checkpoint)
	}
	return checkpoint
}

func TestOverviewCheckpointV2ProjectsOnlyCurrentSessionProof(t *testing.T) {
	for _, change := range []string{"unchanged", "index", "baseline", "compaction", "dirty", "stale", "redelivery", "another_host", "restart"} {
		t.Run(change, func(t *testing.T) {
			root, _ := deltaVolumesFixture(t, 10)
			fixture := transportProofFixtureForRoot(t, root)
			const owner = "original-host"
			fixture.prove(owner, overviewIn{})
			out := fixture.prove(owner, fixture.both)
			if !strings.Contains(out, "cognition_level_state: cognition_governed") {
				t.Fatalf("the fixture must establish reliable cognition: %.700s", out)
			}
			session, host := fixture.session, owner
			wantConfirmed, wantGoverned := false, true
			input := overviewIn{}
			read := func(name string) string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(fixture.root, name))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			switch change {
			case "unchanged":
				wantConfirmed = true
			case "index":
				code := read("aoci.code.txt")
				writeVolumeTestFile(t, fixture.root, "aoci.code.txt", strings.Replace(code, "F:", "F:changed ", 1))
				wantGoverned = false
			case "baseline":
				loaded, fail := loadCognitionCtx(fixture.root)
				if fail != nil {
					t.Fatal(fail)
				}
				data, err := os.ReadFile(loaded.paths.BaselinePath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(loaded.paths.BaselinePath, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			case "compaction":
				input.RefreshReasons = []string{machinecontract.RefreshReasonContextCompaction}
				input.RefreshEventID = "checkpoint-compaction"
			case "dirty":
				writeVolumeTestFile(t, fixture.root, "new.go", "package fixture\n")
				wantConfirmed, wantGoverned = true, false
			case "stale":
				writeVolumeTestFile(t, fixture.root, "main.go", read("main.go")+"func updated() {}\n")
				wantConfirmed, wantGoverned = true, false
			case "redelivery":
				fixture.prove(owner, overviewIn{})
			case "another_host":
				host = "new-host"
				input.Receipt, input.ModelState = &fixture.session.lastReceipt, cognitionStateValid
			case "restart":
				session = newCognitionRefreshSession()
				input.Receipt, input.ModelState = &fixture.session.lastReceipt, cognitionStateValid
			}
			generation, receipt := session.generation, session.lastReceipt
			checkpoint := checkpointV2ForTest(t, fixture.root, session, host, input)
			state := checkpoint.CognitionStateV2
			wantHost, wantStrict := hostDeliveryIncomplete, machinecontract.StrictAttestationNotProvided
			if wantConfirmed {
				wantHost, wantStrict = hostDeliveryConfirmed, machinecontract.StrictAttestationVerified
			}
			if change == "redelivery" {
				wantHost = hostDeliveryUnconfirmed
			}
			wantReliable := change == "unchanged"
			if checkpoint.HostDeliveryStatus != wantHost || state.DeliveryVerified != wantConfirmed ||
				state.StrictAttestationStatus != wantStrict || state.GovernanceAligned != wantGoverned ||
				state.CurrentSystemCognitionReliable != wantReliable || checkpoint.Assessment.Receipt.ModelFullReliable != wantReliable ||
				(checkpoint.Assessment.State == cognitionStateValid) != wantReliable {
				t.Fatalf("checkpoint must combine only matching proof with live governance: host=%s state=%+v assessment=%+v", checkpoint.HostDeliveryStatus, state, checkpoint.Assessment)
			}
			if session.generation != generation || !reflect.DeepEqual(session.lastReceipt, receipt) {
				t.Fatalf("checkpoint changed the session generation or receipt: %d -> %d", generation, session.generation)
			}
			if change == "compaction" {
				again := checkpointV2ForTest(t, fixture.root, session, host, overviewIn{})
				if again.CognitionStateV2.CurrentSystemCognitionReliable || again.Assessment.Receipt.ModelFullReliable ||
					!reflect.DeepEqual(again.Assessment.RefreshReasons, input.RefreshReasons) {
					t.Fatalf("a later checkpoint reused proof while compaction was pending: %+v", again)
				}
				fixture.prove("different-host", overviewIn{})
				fixture.prove("different-host", fixture.both)
				after := checkpointV2ForTest(t, fixture.root, session, owner, overviewIn{})
				if after.CognitionStateV2.CurrentSystemCognitionReliable || after.CognitionStateV2.DeliveryVerified {
					t.Fatalf("another Host's proof revived the pre-compaction delivery: %+v", after)
				}
			}
		})
	}
}

func TestOverviewCheckpointV2PreservesLatestAttestationVerdict(t *testing.T) {
	for _, verdict := range []string{machinecontract.StrictAttestationPartial, machinecontract.StrictAttestationFailed} {
		t.Run(verdict, func(t *testing.T) {
			root, _ := deltaVolumesFixture(t, 10)
			fixture := transportProofFixtureForRoot(t, root)
			const owner = "host"
			fixture.prove(owner, overviewIn{})
			fixture.prove(owner, fixture.both)
			latest := fixture.both
			report := *latest.ModelAttestation
			answers := append([]overviewChallengeAnswer(nil), report.ChallengeAnswers...)
			report.ChallengeAnswers = answers
			latest.ModelAttestation = &report
			wrong := len(answers) - 1
			if verdict == machinecontract.StrictAttestationFailed {
				wrong = len(answers)
			}
			for i := range answers[:wrong] {
				answers[i].CoreF = "unrelated answer"
			}
			fixture.prove(owner, latest)
			checkpoint := checkpointV2ForTest(t, fixture.root, fixture.session, owner, overviewIn{})
			state := checkpoint.CognitionStateV2
			if checkpoint.HostDeliveryStatus != hostDeliveryConfirmed || !state.DeliveryVerified || !state.ModelCognitionUsable ||
				state.StrictAttestationStatus != verdict || state.StrictAttestationVerified ||
				!reflect.DeepEqual(state.StrictAttestationFailureReasons, []string{machinecontract.StrictAttestationReasonCoreFMismatch}) ||
				state.CurrentSystemCognitionReliable || checkpoint.Assessment.Receipt.ModelFullReliable {
				t.Fatalf("checkpoint lost the confirmed delivery or the latest strict verdict: %+v", checkpoint)
			}
		})
	}
}

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
				CoveragePercent: 100, SystemMasteryPercent: 92, TruncationDetected: true,
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
