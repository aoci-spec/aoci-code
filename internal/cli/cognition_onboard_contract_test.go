package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func TestOnboardingBatchContractCarriesExecutableArgvAndConcreteRequestFile(t *testing.T) {
	root := t.TempDir()
	batch := &onboarding.AuthoringBatch{
		Version: onboarding.BatchVersion, OnboardingSessionID: "onboard-test", BatchID: "batch-test",
		NextActionContract: &onboarding.NextActionContract{
			Version: machinecontract.CognitionOnboardingNextActionV1,
			Action:  "submit_authoring_completion", SchemaVersion: onboarding.CompletionVersion,
		},
	}
	if err := decorateOnboardingBatchContract(root, batch, 2, 4096); err != nil {
		t.Fatal(err)
	}
	command := batch.NextActionContract.Command
	if command == nil || !filepath.IsAbs(command.Executable) || !filepath.IsAbs(command.SuggestedRequestFile) || command.DisplayCommand == "" {
		t.Fatalf("Host command identity is incomplete: %#v", command)
	}
	if strings.Contains(filepath.ToSlash(command.SuggestedRequestFile), "/.aoci/drafts/") ||
		!strings.Contains(filepath.ToSlash(command.SuggestedRequestFile), "/.aoci/onboarding/onboard-test/host-inputs/") {
		t.Fatalf("Fresh Host input was placed in mature draft history: %q", command.SuggestedRequestFile)
	}
	want := []string{"--repo", root, "cognition", "onboard", "next", "--completion-file", command.SuggestedRequestFile,
		"--max-objects", "2", "--max-evidence-bytes", "4096", "--json"}
	if len(command.Arguments) != len(want) {
		t.Fatalf("unexpected argv: %#v", command.Arguments)
	}
	for index := range want {
		if command.Arguments[index] != want[index] {
			t.Fatalf("argv[%d]=%q want %q", index, command.Arguments[index], want[index])
		}
	}
}

func TestOnboardingNextCLIPropagatesBatchLimitsIntoContinuationContract(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 12, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "cognition", "onboard", "next",
		"--max-objects", "2", "--max-evidence-bytes", "4096"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("onboard next failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var batch onboarding.AuthoringBatch
	if err := json.Unmarshal(stdout.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Tasks) > 2 || batch.NextActionContract == nil || batch.NextActionContract.Command == nil {
		t.Fatalf("batch limits or continuation contract missing: %#v", batch)
	}
	persisted, err := onboarding.LoadRequired(root)
	if err != nil || persisted.ActiveAuthoringBatch == nil || persisted.ActiveAuthoringBatch.MaxObjects != 2 ||
		persisted.ActiveAuthoringBatch.MaxEvidenceBytes != 4096 {
		t.Fatalf("batch limits were not persisted for response-loss recovery: session=%#v err=%v", persisted, err)
	}
	wantParts := []string{"--max-objects", "2", "--max-evidence-bytes", "4096"}
	for _, part := range wantParts {
		found := false
		for _, argument := range batch.NextActionContract.Command.Arguments {
			if argument == part {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("continuation argv lost %q: %#v", part, batch.NextActionContract.Command.Arguments)
		}
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "--json", "cognition", "onboard", "status"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("onboard status failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var status onboardingSessionView
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil || status.NextActionContract == nil || status.NextActionContract.Command == nil {
		t.Fatalf("status did not recover exact continuation: status=%#v err=%v", status, err)
	}
	for _, part := range wantParts {
		found := false
		for _, argument := range status.NextActionContract.Command.Arguments {
			if argument == part {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("status continuation lost %q: %#v", part, status.NextActionContract.Command.Arguments)
		}
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCLI(status.NextActionContract.Command.Arguments, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("recovered exact next command failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var replay onboarding.AuthoringBatch
	if err := json.Unmarshal(stdout.Bytes(), &replay); err != nil || replay.BatchID != batch.BatchID || len(replay.Tasks) > 2 {
		t.Fatalf("response-loss replay changed active batch or budget: replay=%#v err=%v", replay, err)
	}
}

func TestCandidateBindingContractNavigatesToSplitPreview(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "candidate-payload.json")
	binding := &onboarding.CandidateBinding{
		Version:             machinecontract.CognitionOnboardingCandidateBindingV1,
		OnboardingSessionID: "onboard-test", PlanID: "plan-test", CandidatePayloadSHA256: "payload-test",
		SessionPreimageSHA256: "session-preimage",
		ProvenanceTemplate:    cognitionplan.SemanticAuthoringProvenance{Version: machinecontract.SemanticAuthoringProvenanceV1},
	}
	if err := decorateCandidateBindingContract(root, payload, binding); err != nil {
		t.Fatal(err)
	}
	contract := binding.NextActionContract
	if contract == nil || contract.Action != "preview" || contract.ExpectedPreimage != "session-preimage" || contract.Command == nil {
		t.Fatalf("Candidate binding navigation missing: %#v", contract)
	}
	if contract.AutomaticallyRetryable {
		t.Fatal("state-changing Preview was advertised as automatically retryable")
	}
	if strings.Contains(filepath.ToSlash(contract.Command.SuggestedRequestFile), "/.aoci/drafts/") ||
		!strings.Contains(filepath.ToSlash(contract.Command.SuggestedRequestFile), "/.aoci/onboarding/onboard-test/host-inputs/") {
		t.Fatalf("Fresh provenance input was placed in mature draft history: %q", contract.Command.SuggestedRequestFile)
	}
	args := contract.Command.Arguments
	wantParts := []string{"preview", "--candidate-payload-file", payload, "--provenance-file"}
	for _, part := range wantParts {
		found := false
		for _, value := range args {
			if value == part {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("preview argv missing %q: %#v", part, args)
		}
	}
}

func TestOnboardingStrictTransportDiagnosticsFromRealCLIArePreciseAndZeroWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 12, 5, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Next(root, 2, 4096); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		body        string
		causeCode   string
		failedField string
	}{
		{name: "unknown", body: `{"unexpected":true}`, causeCode: "onboarding_transport_unknown_field", failedField: "completion_file.unexpected"},
		{name: "duplicate", body: `{"semantic_authoring_declaration":{"origin":"one","origin":"two"}}`, causeCode: "onboarding_transport_duplicate_key", failedField: "completion_file.semantic_authoring_declaration.origin"},
		{name: "syntax", body: `{"version":`, causeCode: "onboarding_transport_syntax_invalid", failedField: "completion_file"},
		{name: "trailing", body: `{} {}`, causeCode: "onboarding_transport_trailing_json", failedField: "completion_file"},
		{name: "type", body: `{"semantic_authoring_declaration":{"authoring_run_id":17}}`, causeCode: "onboarding_transport_type_mismatch", failedField: "completion_file.semantic_authoring_declaration.authoring_run_id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestFile := filepath.Join(t.TempDir(), "completion.json")
			if err := os.WriteFile(requestFile, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(onboarding.SessionPath(root))
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := executeCLI([]string{"--repo", root, "--json", "cognition", "onboard", "next", "--completion-file", requestFile,
				"--max-objects", "2", "--max-evidence-bytes", "4096"}, &stdout, &stderr)
			if code != ExitInvalid {
				t.Fatalf("invalid strict payload exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			var envelope struct {
				ErrorCode string                   `json:"error_code"`
				Details   onboarding.ContractError `json:"details"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid JSON failure envelope: %v stdout=%s", err, stdout.String())
			}
			if envelope.ErrorCode != "cognition_onboarding_invalid" || envelope.Details.CauseCode != test.causeCode ||
				envelope.Details.Field != test.failedField || envelope.Details.FormalWritesStarted {
				t.Fatalf("unexpected strict diagnostic: %#v", envelope)
			}
			after, err := os.ReadFile(onboarding.SessionPath(root))
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("invalid transport changed Session: %v", err)
			}
			for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
				if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
					t.Fatalf("invalid transport changed formal asset %s: %v", path, err)
				}
			}
		})
	}
}

func TestCandidateDecodeDiagnosticsFromRealCLIArePreciseAndZeroWrite(t *testing.T) {
	root := readyCandidateTransportRoot(t)
	tests := []struct {
		name        string
		body        string
		causeCode   string
		failedField string
	}{
		{name: "unknown", body: `{"unexpected":true}`, causeCode: "onboarding_transport_unknown_field", failedField: "candidate_payload_file.unexpected"},
		{name: "duplicate", body: `{"assets":[{"asset_id":"one","asset_id":"two"}]}`, causeCode: "onboarding_transport_duplicate_key", failedField: "candidate_payload_file.assets[0].asset_id"},
		{name: "syntax", body: `{"version":`, causeCode: "onboarding_transport_syntax_invalid", failedField: "candidate_payload_file"},
		{name: "trailing", body: `{} {}`, causeCode: "onboarding_transport_trailing_json", failedField: "candidate_payload_file"},
		{name: "type", body: `{"assets":[{"content":17}]}`, causeCode: "onboarding_transport_type_mismatch", failedField: "candidate_payload_file.assets.content"},
	}
	for _, endpoint := range []struct {
		name string
		args func(string, string) []string
	}{
		{name: "bind", args: func(payload, _ string) []string {
			return []string{"--repo", root, "--json", "cognition", "onboard", "candidate", "bind", "--candidate-payload-file", payload}
		}},
		{name: "split_preview", args: func(payload, provenance string) []string {
			return []string{"--repo", root, "--json", "cognition", "onboard", "preview", "--candidate-payload-file", payload,
				"--provenance-file", provenance}
		}},
	} {
		for _, test := range tests {
			t.Run(endpoint.name+"_"+test.name, func(t *testing.T) {
				inputDir := t.TempDir()
				payloadFile := filepath.Join(inputDir, "candidate.json")
				provenanceFile := filepath.Join(inputDir, "provenance.json")
				if err := os.WriteFile(payloadFile, []byte(test.body), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(provenanceFile, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(onboarding.SessionPath(root))
				if err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				code := executeCLI(endpoint.args(payloadFile, provenanceFile), &stdout, &stderr)
				if code != ExitInvalid {
					t.Fatalf("invalid Candidate exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
				}
				var envelope struct {
					ErrorCode string                   `json:"error_code"`
					Details   onboarding.ContractError `json:"details"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatalf("invalid Candidate failure envelope: %v stdout=%s", err, stdout.String())
				}
				if envelope.ErrorCode != "cognition_onboarding_invalid" || envelope.Details.CauseCode != test.causeCode ||
					envelope.Details.Field != test.failedField || envelope.Details.FormalWritesStarted {
					t.Fatalf("unexpected Candidate diagnostic: %#v", envelope)
				}
				after, err := os.ReadFile(onboarding.SessionPath(root))
				if err != nil || !bytes.Equal(after, before) {
					t.Fatalf("invalid Candidate changed Session: %v", err)
				}
				for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
					if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
						t.Fatalf("invalid Candidate changed formal asset %s: %v", path, err)
					}
				}
			})
		}
	}
}

func readyCandidateTransportRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for {
		batch, err := onboarding.Next(root, 100, 8*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Tasks) == 0 {
			break
		}
		completion := *batch.CompletionRequestTemplate
		declaration := *completion.SemanticAuthoringDeclaration
		declaration.Origin = machinecontract.SemanticAuthoringOriginHostModel
		declaration.AuthoringRunID = "candidate-transport-cli-test"
		completion.SemanticAuthoringDeclaration = &declaration
		if _, err := onboarding.CompleteTasks(root, completion); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
