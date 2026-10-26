package onboarding_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/aoci-spec/aoci-code/textassets"
)

// These tests sit outside the package because they drive the onboarding
// review through the real MCP server, which itself imports onboarding.

func preparedReview(t *testing.T, kind string, configure func(*config.Config)) string {
	t.Helper()
	if kind == "bootstrap" {
		return onboarding.PreparedBootstrapReviewForTest(t, configure)
	}
	return onboarding.PreparedLegacyMigrationForTest(t, configure)
}

// askOnboardingApproval plays a host that answers the review dialog with
// answer; a nil answer is a host that declares no dialogs.
func askOnboardingApproval(t *testing.T, root string, answer *mcp.ElicitResult) (map[string]any, string, []string) {
	t.Helper()
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	server, err := mcptools.NewServer(root, "onboarding-dialog-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	shown := []string{}
	options := &mcp.ClientOptions{}
	if answer != nil {
		options.ElicitationHandler = func(_ context.Context, request *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			shown = append(shown, request.Params.Message)
			return answer, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "codex", Version: "test"}, options).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "aoci_maintain", Arguments: map[string]any{
		"intent":           machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindOnboarding},
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("onboarding approval failed: %s", text)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("result is not JSON: %v %s", err, text)
	}
	return decoded, text, shown
}

func mentions(value any, tokens ...string) bool {
	commands, _ := value.([]any)
	for _, command := range commands {
		text, _ := command.(string)
		all := true
		for _, token := range tokens {
			all = all && strings.Contains(text, token)
		}
		if all {
			return true
		}
	}
	return false
}

func TestOnboardingReviewApprovedInHostDialogApplies(t *testing.T) {
	for _, kind := range []string{"bootstrap", "migration"} {
		t.Run(kind, func(t *testing.T) {
			root := preparedReview(t, kind, nil)
			session, _, err := onboarding.AwaitingApproval(root)
			if err != nil {
				t.Fatal(err)
			}
			_, wantRel := onboarding.HumanApprovalPath(root, session)
			result, text, shown := askOnboardingApproval(t, root,
				&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
			if result["status"] != machinecontract.HumanApprovalStatusApproved || result["approval_file"] != wantRel ||
				result["approval_mechanism"] != machinecontract.ApprovalMechanismHostElicitationConfirmation ||
				!mentions(result["next_commands"], "cognition", "onboard", "apply", wantRel) {
				t.Fatalf("review approval was not recorded: %s", text)
			}
			if len(shown) != 1 || !strings.Contains(shown[0], filepath.Base(root)) || !strings.Contains(shown[0], "aoci.txt") {
				t.Fatalf("dialog did not show what is written: %q", shown)
			}
			if pending, err := onboarding.LoadRequired(root); err != nil || pending.ApprovalState != "interaction_required" || pending.ApprovalArtifact != "" {
				t.Fatalf("approving in the dialog applied something: %#v %v", pending, err)
			}
			applied, err := onboarding.Apply(root, wantRel)
			if err != nil {
				t.Fatalf("dialog approval does not apply: %v", err)
			}
			switch value := applied.(type) {
			case *bootstrapapply.ApplyResult:
				if kind != "bootstrap" || !value.FormalComplete {
					t.Fatalf("bootstrap did not complete: %#v", value)
				}
			case *migrationapply.ApplyResult:
				if kind != "migration" || !value.FormalComplete || value.ActiveLayout != machinecontract.CognitionPlannerVolumes {
					t.Fatalf("migration did not complete: %#v", value)
				}
			default:
				t.Fatalf("unexpected Apply result %T", applied)
			}
			if done, err := onboarding.LoadRequired(root); err != nil || done.ApprovalState != "consumed" || done.TransactionState != "applied" {
				t.Fatalf("session did not record the applied review: %#v %v", done, err)
			}
		})
	}
}

func TestOnboardingReviewRejectedInHostDialogKeepsAbortOpen(t *testing.T) {
	for _, kind := range []string{"bootstrap", "migration"} {
		t.Run(kind, func(t *testing.T) {
			root := preparedReview(t, kind, nil)
			session, _, err := onboarding.AwaitingApproval(root)
			if err != nil {
				t.Fatal(err)
			}
			approvalPath, _ := onboarding.HumanApprovalPath(root, session)
			result, text, shown := askOnboardingApproval(t, root, &mcp.ElicitResult{Action: "decline"})
			if len(shown) != 1 || result["status"] != machinecontract.HumanApprovalStatusRejected ||
				!strings.Contains(result["message"].(string), "onboard abort") || !mentions(result["next_commands"], "onboard", "status") {
				t.Fatalf("rejection did not say what happens next: %s", text)
			}
			if _, err := os.Stat(approvalPath); !os.IsNotExist(err) {
				t.Fatalf("a rejection left an approval behind: %v", err)
			}
			if _, err := onboarding.Abort(root); err != nil {
				t.Fatalf("a rejected review can no longer be dropped: %v", err)
			}
		})
	}
}

func TestOnboardingTTYOnlyChannelRefusesDialogApprovalBeforeRecording(t *testing.T) {
	root := preparedReview(t, "migration", func(cfg *config.Config) {
		if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
			t.Fatal(err)
		}
	})
	result, text, shown := askOnboardingApproval(t, root,
		&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
	if len(shown) != 0 || result["status"] != machinecontract.HumanApprovalStatusHostDialogDisabled ||
		!mentions(result["next_commands"], "cognition", "migration", "approve", "--out-file") ||
		!mentions(result["next_commands"], "cognition", "onboard", "apply") {
		t.Fatalf("tty_only still opened a dialog: %s shown=%q", text, shown)
	}
	session, envelopeData, err := onboarding.AwaitingApproval(root)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := migrationapply.DecodeApplyEnvelope(envelopeData)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := migrationapply.RecordHostDialogApproval(envelope, "host-dialog", "2026-10-27T01:02:03Z")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(approval)
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Apply(root, path); err == nil || !strings.Contains(err.Error(), "migration_host_dialog_approval_disabled") {
		t.Fatalf("Apply admitted a dialog approval under tty_only: %v", err)
	}
	// The refusal came before the session recorded anything, so it can still be dropped.
	if after, err := onboarding.LoadRequired(root); err != nil || after.ApprovalArtifact != "" || after.OnboardingSessionID != session.OnboardingSessionID {
		t.Fatalf("a refused approval was recorded: %#v %v", after, err)
	}
	if _, err := onboarding.Abort(root); err != nil {
		t.Fatalf("a refused approval wedged the session: %v", err)
	}
}
