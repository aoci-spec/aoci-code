package cli

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestRequireHumanPhraseReturnsExecutableInteractionContractWithoutTTY(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()
	defer writeEnd.Close()
	oldStdin := os.Stdin
	os.Stdin = readEnd
	t.Cleanup(func() { os.Stdin = oldStdin })

	err = requireHumanConfirmation(&cobra.Command{}, humanConfirmation{Phrase: "APPROVE X abc", Prompt: "prompt",
		ExactCommand: "'aoci' 'approve'", Digest: "abc", SafeSummary: "write_set_count=2", ResumeAction: "apply"})
	var interaction *hostInteractionError
	if !errors.As(err, &interaction) {
		t.Fatalf("expected interaction contract, got %v", err)
	}
	state := interaction.State
	if !state.InteractionRequired || state.InteractionKind != "human_tty_digest_confirmation" ||
		state.Digest != "abc" || state.ConfirmationPhrase != "APPROVE X abc" || state.ExactCommand == "" ||
		state.ModelSelfApproval || state.FormalWritesStarted {
		t.Fatalf("unsafe or incomplete interaction state: %#v", state)
	}
}

func TestOnboardingTransportSchemaRejectsBeforeWorkflowWrite(t *testing.T) {
	for _, data := range []string{
		`{"version":"cognition-onboarding-completion/v1","onboarding_session_id":"x","completed_task_ids":[],"unknown":true}`,
		`{"version":"cognition-onboarding-completion/v1","version":"cognition-onboarding-completion/v1","onboarding_session_id":"x","completed_task_ids":[]}`,
		`{"version":"cognition-onboarding-completion/v1","onboarding_session_id":"x","completed_task_ids":[]} {}`,
	} {
		var value struct {
			Version string   `json:"version"`
			Session string   `json:"onboarding_session_id"`
			Tasks   []string `json:"completed_task_ids"`
		}
		if err := decodeOnboardingCLI([]byte(data), &value); err == nil {
			t.Fatalf("invalid transport JSON was accepted: %s", data)
		}
	}
}
