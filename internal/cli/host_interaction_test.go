package cli

import (
	"errors"
	"os"
	"strings"
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

	err = requireHumanPhrase(&cobra.Command{}, "APPROVE X abc", "prompt", "'aoci' 'approve'", "abc", "write_set_count=2", "apply")
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

func TestGuideNextActionContractCarriesAgentSchemaAndPreimage(t *testing.T) {
	guide := &agentGuide{Agent: "codex", Plan: &agentPlan{
		PlanID: "plan-1", Stage: agentPlanStageEntriesRequired, NextAction: agentPlanActionStageEntries,
		RepositoryRoot: "/repo", IndexSHA256: strings.Repeat("a", 64),
	}, Commands: agentGuideCommands{EntriesStage: "'aoci' index agent entries stage", Guide: "'aoci' index agent guide"}}
	populateAgentNextActionContract(guide)
	action := guide.NextActionContract
	if action.Agent != "codex" || action.RequiredParameters["agent"] != "codex" ||
		action.SchemaVersion != "agent-entries-stage-request/v1" || action.RequestFile == "" ||
		action.ExpectedPreimage != strings.Repeat("a", 64) || action.PlanOrRunIdentity != "plan-1" ||
		action.TransportCorrectionLimit != 1 || !action.AutomaticallyRetryable || action.SuccessNextAction == "" {
		t.Fatalf("incomplete next action contract: %#v", action)
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
