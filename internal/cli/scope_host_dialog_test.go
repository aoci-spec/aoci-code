package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/aoci-spec/aoci-code/textassets"
)

// askInHostDialog plays an MCP host in front of the repository's own server:
// it calls aoci_maintain with arguments and answers any dialog with answer.
// A nil answer is a host that declares no dialogs at all. It returns the
// decoded result, whether it was a tool error, and every dialog message the
// person was shown.
func askInHostDialog(t *testing.T, root string, arguments map[string]any, answer *mcp.ElicitResult) (map[string]any, string, bool, []string) {
	t.Helper()
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	server, err := mcptools.NewServer(root, "host-dialog-test")
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
	client := mcp.NewClient(&mcp.Implementation{Name: "Claude Code", Version: "test"}, options)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "aoci_maintain", Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	decoded := map[string]any{}
	if !result.IsError {
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Fatalf("human_approval result is not JSON: %v %s", err, text)
		}
	}
	return decoded, text, result.IsError, shown
}

// pausedActivation tightens the posture so activation stops for a person and
// returns the saved continuation.
func pausedActivation(t *testing.T, root string) scopeActivationApproval {
	t.Helper()
	if _, err := runScopeCLI(t, root, "approval-mode", "review"); err != nil {
		t.Fatal(err)
	}
	code, data := activateJSON(t, root)
	var response struct {
		ErrorCode string                  `json:"error_code"`
		Message   string                  `json:"message"`
		Details   scopeActivationApproval `json:"details"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if code != 2 || response.ErrorCode != "managed_scope_human_approval_required" {
		t.Fatalf("activation did not pause for a person: code=%d %s", code, data)
	}
	return response.Details
}

func commandsMention(value any, tokens ...string) bool {
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

func TestScopeActivationApprovedInHostDialogApplies(t *testing.T) {
	root := scopeActivationFixture(t)
	details := pausedActivation(t, root)
	request := details.HostDialogRequest
	if request == nil || request.Tool != "aoci_maintain" || request.Arguments["intent"] != machinecontract.MaintainIntentHumanApproval {
		t.Fatalf("a paused activation offers no dialog route: %+v", details)
	}
	before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
	raw, err := os.ReadFile(details.PreviewFile)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := scopechange.DecodePreview(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, text, isError, shown := askInHostDialog(t, root, request.Arguments,
		&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
	approvalFile := filepath.Join(filepath.Dir(details.PreviewFile), "approval.json")
	wantRel, _ := filepath.Rel(root, approvalFile)
	if isError || result["status"] != machinecontract.HumanApprovalStatusApproved ||
		result["approval_mechanism"] != machinecontract.ApprovalMechanismHostElicitationConfirmation ||
		result["approval_file"] != filepath.ToSlash(wantRel) || result["formal_writes_started"] != false ||
		result["next_action"] != machinecontract.HumanApprovalActionApply || !commandsMention(result["next_commands"], "scope", "apply", "--approval-file") {
		t.Fatalf("approval in the dialog was not recorded: %s", text)
	}
	// The person saw the change as AOCI read it, not as a caller described it.
	if len(shown) != 1 || !strings.Contains(shown[0], filepath.Base(root)) || !strings.Contains(shown[0], preview.Plan.PlanID) ||
		!strings.Contains(shown[0], scopechange.ApprovalEffectSummary(preview)) {
		t.Fatalf("dialog did not show the change: %q", shown)
	}
	// Approving records an approval; it applies nothing.
	assertActivationFiles(t, root, before)
	recorded, err := os.ReadFile(approvalFile)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := scopechange.DecodeApproval(recorded)
	if err != nil || approval.Mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation || approval.Actor != "host-dialog@Claude-Code" {
		t.Fatalf("recorded approval: %#v %v", approval, err)
	}
	if runtime.GOOS != "windows" {
		if info, statErr := os.Stat(approvalFile); statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("approval readable beyond its operator: %v %v", info, statErr)
		}
	}
	again, _, _, shownAgain := askInHostDialog(t, root, request.Arguments,
		&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
	if again["status"] != machinecontract.HumanApprovalStatusAlreadyApproved || len(shownAgain) != 0 {
		t.Fatalf("a recorded approval asked the person again: %v shown=%q", again, shownAgain)
	}
	if out, _, err := runScopeSplit(t, root, nil, true, "apply", "--preview-file", details.PreviewFile, "--approval-file", approvalFile); err != nil {
		t.Fatalf("dialog approval does not apply: %v %s", err, out)
	}
	state, _, err := baseline.Load(root)
	if err != nil || state.ManagedScope.ApplyAuthorizationMode != machinecontract.ApplyAuthorizationReview {
		t.Fatalf("activation did not take effect: %#v %v", state.ManagedScope, err)
	}
}

func TestScopeActivationRejectedInHostDialogChangesNothing(t *testing.T) {
	for name, answer := range map[string]*mcp.ElicitResult{
		"reject-choice": {Action: "accept", Content: map[string]any{"decision": "reject", "note": "  keep the auto posture  "}},
		"decline":       {Action: "decline"},
	} {
		t.Run(name, func(t *testing.T) {
			root := scopeActivationFixture(t)
			details := pausedActivation(t, root)
			before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
			result, text, isError, shown := askInHostDialog(t, root, details.HostDialogRequest.Arguments, answer)
			if isError || len(shown) != 1 || result["status"] != machinecontract.HumanApprovalStatusRejected ||
				result["next_action"] != machinecontract.HumanApprovalActionAskPerson || result["approval_file"] != nil ||
				!strings.Contains(result["message"].(string), "scope rule remove") || !commandsMention(result["next_commands"], "scope", "status") {
				t.Fatalf("rejection did not say what happens next: %s", text)
			}
			if name == "reject-choice" && result["human_note"] != "keep the auto posture" {
				t.Fatalf("the person's note did not reach the agent: %s", text)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(details.PreviewFile), "approval.json")); !os.IsNotExist(err) {
				t.Fatalf("a rejection left an approval behind: %v", err)
			}
			assertActivationFiles(t, root, before)
		})
	}
}

func TestHostDialogDismissedOrMissingOffersTheTerminal(t *testing.T) {
	for name, answer := range map[string]*mcp.ElicitResult{
		"dismissed": {Action: "cancel"},
		"no-dialog": nil,
	} {
		t.Run(name, func(t *testing.T) {
			root := scopeActivationFixture(t)
			details := pausedActivation(t, root)
			result, text, isError, _ := askInHostDialog(t, root, details.HostDialogRequest.Arguments, answer)
			want, action := machinecontract.HumanApprovalStatusDismissed, machinecontract.HumanApprovalActionAskAgain
			if answer == nil {
				want, action = machinecontract.HumanApprovalStatusHostDialogUnavailable, machinecontract.HumanApprovalActionTerminal
			}
			commands, _ := result["next_commands"].([]any)
			if isError || result["status"] != want || result["next_action"] != action || len(commands) != 2 ||
				!commandsMention(result["next_commands"], "scope", "approve", "--out-file") ||
				!commandsMention(result["next_commands"], "scope", "apply", "--approval-file") {
				t.Fatalf("no terminal route offered: %s", text)
			}
		})
	}
}

func TestTeamTTYOnlyChannelKeepsApprovalsInTheTerminal(t *testing.T) {
	root := scopeActivationFixture(t)
	if _, _, code := runCLI(t, root, "--json", "config", "set", "human_approval_channel", "tty_only"); code != ExitOK {
		t.Fatalf("config set human_approval_channel exit=%d", code)
	}
	if got, _, _ := runCLI(t, root, "--json", "config", "get", "human_approval_channel"); strings.TrimSpace(got) != `"tty_only"` {
		t.Fatalf("config get human_approval_channel = %s", got)
	}
	details := pausedActivation(t, root)
	if details.HostDialogRequest != nil {
		t.Fatalf("a tty_only repository was offered a dialog route: %+v", details.HostDialogRequest)
	}
	arguments := map[string]any{"intent": machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindScopeChange, "preview_file": details.PreviewFile}}
	result, text, isError, shown := askInHostDialog(t, root, arguments,
		&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
	if isError || len(shown) != 0 || result["status"] != machinecontract.HumanApprovalStatusHostDialogDisabled ||
		!commandsMention(result["next_commands"], "scope", "approve", "--out-file") {
		t.Fatalf("tty_only still opened a dialog: %s shown=%q", text, shown)
	}
	// An approval minted in a dialog anyway is refused before anything moves.
	raw, err := os.ReadFile(details.PreviewFile)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := scopechange.DecodePreview(raw)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := scopechange.NewHostDialogApproval(preview, "host-dialog", "2026-10-27T01:02:03Z")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(approval)
	approvalFile := filepath.Join(filepath.Dir(details.PreviewFile), "approval.json")
	if err := os.WriteFile(approvalFile, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
	_, _, err = runScopeSplit(t, root, nil, true, "apply", "--preview-file", details.PreviewFile, "--approval-file", approvalFile)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_host_dialog_approval_disabled") {
		t.Fatalf("apply admitted a dialog approval under tty_only: %v", err)
	}
	assertActivationFiles(t, root, before)
	// Asking again names why that approval no longer counts instead of
	// reporting it approved or calling it someone else's.
	if _, text, isError, _ := askInHostDialog(t, root, arguments, nil); !isError || !strings.Contains(text, "human_approval_file_not_admitted") {
		t.Fatalf("a dialog approval the team no longer admits was reported as: %s", text)
	}
}

func TestStaleScopePreviewIsNeverShownToThePerson(t *testing.T) {
	root := scopeActivationFixture(t)
	details := pausedActivation(t, root)
	if _, err := runScopeCLI(t, root, "rule", "add", "future", "--action", "exclude", "--pattern", "future.txt",
		"--pattern-kind", "file", "--reason", "future artifact"); err != nil {
		t.Fatal(err)
	}
	result, text, isError, shown := askInHostDialog(t, root, details.HostDialogRequest.Arguments,
		&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
	if isError || len(shown) != 0 || result["status"] != machinecontract.HumanApprovalStatusStale ||
		result["next_action"] != machinecontract.HumanApprovalActionRebuild || !commandsMention(result["next_commands"], "scope", "activate") {
		t.Fatalf("a stale preview reached the person: %s shown=%q", text, shown)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(details.PreviewFile), "approval.json")); !os.IsNotExist(err) {
		t.Fatalf("a stale preview was approved: %v", err)
	}
}

func TestHostDialogReadsPreviewsOnlyFromTheRuntimeArea(t *testing.T) {
	root := scopeActivationFixture(t)
	details := pausedActivation(t, root)
	raw, err := os.ReadFile(details.PreviewFile)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "preview.json")
	if err := os.WriteFile(outside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	named := []string{outside, "preview.json", "../preview.json", ".aoci/../preview.json"}
	if runtime.GOOS != "windows" {
		link := filepath.Join(filepath.Dir(details.PreviewFile), "link.json")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		named = append(named, link)
	}
	for _, previewFile := range named {
		arguments := map[string]any{"intent": machinecontract.MaintainIntentHumanApproval,
			"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindScopeChange, "preview_file": previewFile}}
		_, text, isError, shown := askInHostDialog(t, root, arguments,
			&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}})
		if !isError || len(shown) != 0 || !strings.Contains(text, "human_approval_preview_path_invalid") {
			t.Fatalf("%s: preview outside the runtime area was read: %s", previewFile, text)
		}
	}
}

func TestHumanApprovalRequestShapeIsChecked(t *testing.T) {
	root := scopeActivationFixture(t)
	for _, arguments := range []map[string]any{
		{"intent": machinecontract.MaintainIntentHumanApproval},
		{"intent": machinecontract.MaintainIntentHumanApproval, "scope": "code",
			"approval_request": map[string]any{"kind": "onboarding"}},
		{"intent": machinecontract.MaintainIntentHumanApproval, "approval_request": map[string]any{"kind": "scope_change"}},
		{"intent": machinecontract.MaintainIntentHumanApproval,
			"approval_request": map[string]any{"kind": "onboarding", "preview_file": "x"}},
		{"approval_request": map[string]any{"kind": "onboarding"}},
	} {
		if _, text, isError, _ := askInHostDialog(t, root, arguments, nil); !isError {
			t.Fatalf("malformed request accepted: %v -> %s", arguments, text)
		}
	}
	// No onboarding review is waiting in this repository.
	_, text, isError, _ := askInHostDialog(t, root, map[string]any{"intent": machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": "onboarding"}}, nil)
	if !isError || !strings.Contains(text, "onboarding") {
		t.Fatalf("onboarding approval without a session: %s", text)
	}
}

func TestTerminalRejectionSaysWhatHappensNext(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, _ := reviewPreviewFile(t, root)
	grantTTY(t)
	prompt := capturePrompt(t)
	outPath := filepath.Join(t.TempDir(), "approval.json")
	_, _, err := runScopeSplit(t, root, strings.NewReader("\n"), true, "approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err == nil || !strings.Contains(err.Error(), "human_tty_digest_confirmation_not_confirmed") {
		t.Fatalf("an empty answer was not a rejection: %v", err)
	}
	shown := prompt.String()
	message := func(key string, args ...any) string {
		value, err := textassets.Message(textassets.DefaultLocale, key, args...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !strings.Contains(shown, message("cli.approval.reject_hint")) ||
		!strings.Contains(shown, message("cli.approval.not_approved", message("cli.approval.next.scope_change"))) {
		t.Fatalf("the person was not told how to reject or what comes next: %q", shown)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("a rejection produced an approval: %v", statErr)
	}
}

func TestApproveWithoutATerminalOffersTheDialogRoute(t *testing.T) {
	// The test process may itself sit on a terminal; the model's shell does not.
	previous := stdinIsCharDevice
	stdinIsCharDevice = func() bool { return false }
	t.Cleanup(func() { stdinIsCharDevice = previous })
	root := scopeActivationFixture(t)
	details := pausedActivation(t, root)
	stdout, stderr, code := runCLI(t, root, "--json", "scope", "approve", "--preview-file", details.PreviewFile, "--actor", "tester")
	var response struct {
		ErrorCode string                  `json:"error_code"`
		Details   hostInteractionRequired `json:"details"`
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("approve without a terminal: %v %s %s", err, stdout, stderr)
	}
	request := response.Details.HostDialogRequest
	relative, _ := filepath.Rel(root, details.PreviewFile)
	if code == ExitOK || response.ErrorCode != "interaction_required" || !response.Details.TTYRequired || request == nil ||
		request.Arguments["approval_request"].(map[string]any)["preview_file"] != filepath.ToSlash(relative) {
		t.Fatalf("no dialog route offered without a terminal: code=%d %s", code, stdout)
	}
	// A preview outside the runtime area has no dialog route to offer.
	elsewhere, _ := reviewPreviewFile(t, buildScopeCLIRepo(t))
	stdout, _, _ = runCLI(t, root, "--json", "scope", "approve", "--preview-file", elsewhere, "--actor", "tester")
	var terminalOnly struct {
		ErrorCode string                  `json:"error_code"`
		Details   hostInteractionRequired `json:"details"`
	}
	if err := json.Unmarshal([]byte(stdout), &terminalOnly); err != nil || terminalOnly.ErrorCode != "interaction_required" ||
		terminalOnly.Details.HostDialogRequest != nil {
		t.Fatalf("a dialog route was offered for a preview AOCI cannot read: %s", stdout)
	}
}
