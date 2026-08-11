package onboarding

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestFreshBatchPublishesExecutableHostContractsWithoutRepeatingCandidateFrameworks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Next(root, 1, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if first.CompletionRequestTemplate == nil || first.NextActionContract == nil ||
		first.NextActionContract.Version != machinecontract.CognitionOnboardingNextActionV1 ||
		first.NextActionContract.Action != "submit_authoring_completion" ||
		first.NextActionContract.AutomaticallyRetryable ||
		first.CompletionRequestTemplate.BatchID != first.BatchID ||
		first.CompletionRequestTemplate.SemanticAuthoringDeclaration == nil ||
		first.CompletionRequestTemplate.SemanticAuthoringDeclaration.AuthoringRunID != hostRunIDPlaceholder {
		t.Fatalf("first batch did not expose the complete Host contract: %#v", first)
	}
	if first.CandidateDraftRequest != nil {
		t.Fatalf("non-terminal batch repeated Candidate framework payload: %#v", first)
	}
	completion := hostAuthoredCompletion(session, first)
	if _, err := CompleteTasks(root, completion); err != nil {
		t.Fatal(err)
	}

	for {
		batch, err := Next(root, 1, 8*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Tasks) == 0 {
			if batch.CandidateDraftRequest == nil || len(batch.CandidateDraftRequest.Template.Assets) == 0 ||
				batch.NextActionContract == nil || batch.NextActionContract.Action != "bind_candidate_payload" ||
				batch.NextActionContract.SchemaVersion != machinecontract.CognitionLayoutCandidateV1 ||
				!batch.NextActionContract.AutomaticallyRetryable {
				t.Fatalf("terminal batch did not expose Candidate binding contract: %#v", batch)
			}
			if batch.CandidateDraftRequest.Template.SemanticAuthoringProvenance != nil {
				t.Fatal("Candidate draft template included provenance before binding")
			}
			break
		}
		if batch.CompletionRequestTemplate == nil || batch.CompletionRequestTemplate.SemanticAuthoringDeclaration == nil ||
			batch.CompletionRequestTemplate.SemanticAuthoringDeclaration.AuthoringRunID != completion.SemanticAuthoringDeclaration.AuthoringRunID ||
			batch.CompletionRequestTemplate.SemanticAuthoringDeclaration.Origin != completion.SemanticAuthoringDeclaration.Origin {
			t.Fatalf("later batch did not echo the persisted Host declaration: %#v", batch.CompletionRequestTemplate)
		}
		if _, err := CompleteTasks(root, *batch.CompletionRequestTemplate); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCandidateBindingIsReadOnlyAndRequiresCompleteProvenanceFreePayload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for {
		batch, err := Next(root, 100, 8*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Tasks) == 0 {
			break
		}
		if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
			t.Fatal(err)
		}
	}

	candidate := onboardingCandidate(t, root, session.Plan)
	validReceipt := *candidate.SemanticAuthoringProvenance
	candidate.SemanticAuthoringProvenance = nil
	candidateData, _ := json.Marshal(candidate)
	beforeSession, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := BindCandidate(root, candidateData)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Version != machinecontract.CognitionOnboardingCandidateBindingV1 ||
		binding.CandidatePayloadSHA256 != cognitionplan.CandidatePayloadSHA256(candidate) ||
		binding.ProvenanceTemplate.AuthoringRunID != validReceipt.AuthoringRunID ||
		binding.ProvenanceTemplate.CandidatePayloadSHA256 != binding.CandidatePayloadSHA256 ||
		!binding.HostDeclarationEchoed || binding.SemanticGenerated {
		t.Fatalf("Candidate binding did not preserve the Host trust boundary: %#v", binding)
	}
	afterSession, err := os.ReadFile(SessionPath(root))
	if err != nil || string(afterSession) != string(beforeSession) {
		t.Fatalf("read-only Candidate binding changed Session: err=%v", err)
	}
	for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if _, statErr := os.Lstat(filepath.Join(root, path)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("Candidate binding changed formal asset %s: %v", path, statErr)
		}
	}

	withProvenance := *candidate
	withProvenance.SemanticAuthoringProvenance = &validReceipt
	withProvenanceData, _ := json.Marshal(&withProvenance)
	if _, err := BindCandidate(root, withProvenanceData); err == nil {
		t.Fatal("Candidate binding accepted a prefilled provenance receipt")
	}

	invalid := *candidate
	invalid.Assets = append([]cognitionplan.CandidateAsset{}, candidate.Assets...)
	invalid.Assets[0].Content += "#Project: MODEL_AUTHORING_REQUIRED\n"
	invalidData, _ := json.Marshal(&invalid)
	if _, err := BindCandidate(root, invalidData); err == nil {
		t.Fatal("Candidate binding returned a digest for an otherwise invalid payload")
	}
	afterRejected, err := os.ReadFile(SessionPath(root))
	if err != nil || string(afterRejected) != string(beforeSession) {
		t.Fatalf("rejected Candidate binding changed Session: err=%v", err)
	}

	drifted := *candidate
	drifted.Assets = append([]cognitionplan.CandidateAsset{}, candidate.Assets...)
	drifted.Assets[0].Content += "# bound payload changed\n"
	provenance := binding.ProvenanceTemplate
	drifted.SemanticAuthoringProvenance = &provenance
	driftedData, _ := json.Marshal(&drifted)
	_, err = Preview(root, driftedData, nil)
	var previewErr *ContractError
	if !errors.As(err, &previewErr) || previewErr.Field != "candidate_payload" ||
		!strings.Contains(previewErr.Actual, "semantic_authoring_candidate_mismatch") {
		t.Fatalf("post-binding Candidate drift was not diagnosed precisely: %#v err=%v", previewErr, err)
	}
	afterDrift, readErr := os.ReadFile(SessionPath(root))
	if readErr != nil || string(afterDrift) != string(beforeSession) {
		t.Fatalf("rejected post-binding drift changed Session: err=%v", readErr)
	}

	hostInputs := filepath.Join(root, ".aoci", "onboarding", session.OnboardingSessionID, "host-inputs")
	if err := os.MkdirAll(hostInputs, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"completion.json":                    []byte("{}\n"),
		"candidate-payload.json":             candidateData,
		"semantic-authoring-provenance.json": []byte("{}\n"),
	} {
		if err := os.WriteFile(filepath.Join(hostInputs, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	finalCandidate := *candidate
	finalProvenance := binding.ProvenanceTemplate
	finalCandidate.SemanticAuthoringProvenance = &finalProvenance
	finalData, _ := json.Marshal(&finalCandidate)
	if _, err := Preview(root, finalData, nil); err != nil {
		t.Fatalf("valid Candidate could not reach Preview after Host inputs were saved: %v", err)
	}
	if _, err := Prepare(root); err != nil {
		t.Fatalf("Fresh Host input directory was misclassified as mature governance history: %v", err)
	}
	prepared, err := LoadRequired(root)
	if err != nil || prepared.ApprovalState != "policy_bound_auto" || prepared.NextAction != "auto_apply" {
		t.Fatalf("Fresh Auto eligibility did not survive Host inputs: session=%#v err=%v", prepared, err)
	}
	beforeReplay, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	artifactPaths := []string{prepared.CandidateArtifact, prepared.PreviewArtifact, prepared.EnvelopeArtifact, prepared.ApprovalArtifact}
	artifactBytes := make(map[string][]byte, len(artifactPaths))
	for _, relative := range artifactPaths {
		if relative == "" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		artifactBytes[relative] = data
	}
	_, err = Preview(root, finalData, nil)
	var replayErr *ContractError
	if !errors.As(err, &replayErr) || replayErr.Field != "next_action" || replayErr.CauseCode != "onboarding_preview_phase_invalid" || replayErr.FormalWritesStarted {
		t.Fatalf("prepared Preview replay did not fail at the phase guard: %#v err=%v", replayErr, err)
	}
	afterReplay, readErr := os.ReadFile(SessionPath(root))
	if readErr != nil || string(afterReplay) != string(beforeReplay) {
		t.Fatalf("prepared Preview replay changed Session: %v", readErr)
	}
	for relative, before := range artifactBytes {
		after, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if readErr != nil || string(after) != string(before) {
			t.Fatalf("prepared Preview replay changed artifact %s: %v", relative, readErr)
		}
	}
	for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if _, statErr := os.Lstat(filepath.Join(root, path)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("prepared Preview replay changed formal asset %s: %v", path, statErr)
		}
	}
}

func TestCompletionContractFailuresAreFieldSpecificAndZeroWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	completion := hostAuthoredCompletion(session, batch)
	completion.BatchID = "wrong-batch"
	_, err = CompleteTasks(root, completion)
	var contractErr *ContractError
	if !errors.As(err, &contractErr) || contractErr.Field != "batch_id" || contractErr.CauseCode != "onboarding_completion_batch_mismatch" || contractErr.FormalWritesStarted {
		t.Fatalf("Completion mismatch did not return precise details: %#v err=%v", contractErr, err)
	}
	after, readErr := os.ReadFile(SessionPath(root))
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("rejected Completion changed Session: err=%v", readErr)
	}
}

func TestPersistedV1SessionDoesNotReceiveV2HostContract(t *testing.T) {
	session := &Session{Version: LegacySessionVersion, OnboardingSessionID: "legacy", PlanID: "plan", NextAction: "authoring_next"}
	if contract := BuildOnboardingNextActionContract("/repo", "/bin/aoci", session); contract != nil {
		t.Fatalf("persisted v1 Session received a v2 Host contract: %#v", contract)
	}
}

func TestApplyPendingSessionAlwaysRoutesToResumeWithoutClaimingFormalWrites(t *testing.T) {
	session := &Session{Version: SessionVersion, OnboardingSessionID: "fresh", PlanID: "plan", NextAction: "human_tty_digest_confirmation",
		TransactionState: "apply_pending", PreimageSHA256: "preimage"}
	contract := BuildOnboardingNextActionContract("/repo", "/bin/aoci", session)
	if contract == nil || contract.Action != "resume" || contract.Command == nil || contract.SuccessNextAction != "none" ||
		contract.FormalWritesStarted || !contains(contract.Command.Arguments, "resume") {
		t.Fatalf("apply-pending crash did not route to idempotent resume: %#v", contract)
	}
}

func TestPersistedActiveBatchWithoutBudgetsFallsBackToCurrentDefaults(t *testing.T) {
	session := &Session{Version: SessionVersion, OnboardingSessionID: "fresh", PlanID: "plan", NextAction: "authoring_next",
		ActiveAuthoringBatch: &ActiveAuthoringBatch{BatchID: "batch", TaskIDs: []string{"task"}, EvidenceBytes: 1}}
	contract := BuildOnboardingNextActionContract("/repo", "/bin/aoci", session)
	if contract == nil || contract.Command == nil {
		t.Fatalf("old active batch lost its continuation: %#v", contract)
	}
	want := []string{"--repo", "/repo", "cognition", "onboard", "next", "--max-objects", "25",
		"--max-evidence-bytes", "262144", "--json"}
	if !equalStrings(contract.Command.Arguments, want) {
		t.Fatalf("old active batch did not use current defaults: %#v", contract.Command.Arguments)
	}
}

func TestWindowsDisplayHostCommandQuotesSpacesAndSingleQuotes(t *testing.T) {
	command := displayHostCommand(
		"windows",
		`C:\Program Files\AOCI's\aoci.exe`,
		[]string{"--repo", `D:\Project Space\owner's shop`, "cognition", "onboard", "next", "--json"},
	)
	want := `& 'C:\Program Files\AOCI''s\aoci.exe' '--repo' 'D:\Project Space\owner''s shop' 'cognition' 'onboard' 'next' '--json'`
	if command != want {
		t.Fatalf("PowerShell display command = %q, want %q", command, want)
	}
}
