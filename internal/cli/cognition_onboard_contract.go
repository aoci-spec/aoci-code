package cli

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func decorateOnboardingBatchContract(root string, batch *onboarding.AuthoringBatch, maxObjects int, maxEvidenceBytes int64) error {
	if batch == nil || batch.NextActionContract == nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if absolute, absoluteErr := filepath.Abs(executable); absoluteErr == nil {
		executable = absolute
	}
	if session, loadErr := onboarding.LoadRequired(root); loadErr == nil && session.ActiveAuthoringBatch != nil &&
		session.ActiveAuthoringBatch.BatchID == batch.BatchID {
		if session.ActiveAuthoringBatch.MaxObjects > 0 {
			maxObjects = session.ActiveAuthoringBatch.MaxObjects
		} else {
			maxObjects = 25
		}
		if session.ActiveAuthoringBatch.MaxEvidenceBytes > 0 {
			maxEvidenceBytes = session.ActiveAuthoringBatch.MaxEvidenceBytes
		} else {
			maxEvidenceBytes = 256 * 1024
		}
	}
	var arguments []string
	var requestFile string
	switch batch.NextActionContract.Action {
	case "submit_authoring_completion":
		requestFile = filepath.Join(root, ".aoci", "onboarding", batch.OnboardingSessionID, "host-inputs", "completion-"+batch.BatchID+".json")
		arguments = []string{"--repo", root, "cognition", "onboard", "next", "--completion-file", requestFile,
			"--max-objects", strconv.Itoa(maxObjects), "--max-evidence-bytes", strconv.FormatInt(maxEvidenceBytes, 10), "--json"}
	case "bind_candidate_payload":
		requestFile = filepath.Join(root, ".aoci", "onboarding", batch.OnboardingSessionID, "host-inputs", "candidate-payload.json")
		arguments = []string{"--repo", root, "cognition", "onboard", "candidate", "bind", "--candidate-payload-file", requestFile, "--json"}
	}
	if len(arguments) != 0 {
		batch.NextActionContract.Command = onboarding.BuildHostCommand(executable, arguments, requestFile)
	}
	return nil
}

func decorateCandidateBindingContract(root, candidatePayloadFile string, binding *onboarding.CandidateBinding) error {
	if binding == nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if absolute, absoluteErr := filepath.Abs(executable); absoluteErr == nil {
		executable = absolute
	}
	provenanceFile := filepath.Join(root, ".aoci", "onboarding", binding.OnboardingSessionID, "host-inputs", "semantic-authoring-provenance.json")
	arguments := []string{"--repo", root, "cognition", "onboard", "preview", "--candidate-payload-file", candidatePayloadFile, "--provenance-file", provenanceFile, "--json"}
	binding.NextActionContract = &onboarding.NextActionContract{
		Version: machinecontract.CognitionOnboardingNextActionV1,
		Action:  "preview", SchemaVersion: machinecontract.SemanticAuthoringProvenanceV1,
		OnboardingSessionID: binding.OnboardingSessionID, PlanID: binding.PlanID,
		ExpectedPreimage: binding.SessionPreimageSHA256,
		Command:          onboarding.BuildHostCommand(executable, arguments, provenanceFile),
		TTYRequired:      false, AutomaticallyRetryable: false,
		TransportSchemaCorrectionLimit: 1, SuccessNextAction: "resume",
		FormalWritesStarted: false,
	}
	return nil
}

func newOnboardingSessionView(root string, session *onboarding.Session) (*onboardingSessionView, error) {
	if session == nil {
		return &onboardingSessionView{}, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if absolute, absoluteErr := filepath.Abs(executable); absoluteErr == nil {
		executable = absolute
	}
	return &onboardingSessionView{Session: session, NextActionContract: onboarding.BuildOnboardingNextActionContract(root, executable, session)}, nil
}

type onboardingSessionView struct {
	*onboarding.Session
	NextActionContract *onboarding.NextActionContract `json:"next_action_contract,omitempty"`
}
