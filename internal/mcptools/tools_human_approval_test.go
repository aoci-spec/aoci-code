package mcptools

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

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// pendingScopeApprovalRepo is a governed Volumes repository whose team has
// just tightened its posture to review, with the resulting Scope Change
// preview saved under .aoci the way a paused activation saves it.
func pendingScopeApprovalRepo(t *testing.T) (string, string, *scopechange.Preview) {
	t.Helper()
	return pendingScopeApprovalRepoWith(t, nil)
}

// pendingScopeApprovalRepoWith edits the team configuration before the
// preview is built: the preview binds the configuration bytes, so any later
// edit makes it stale.
func pendingScopeApprovalRepoWith(t *testing.T, configure func(*config.Config)) (string, string, *scopechange.Preview) {
	t.Helper()
	root := buildManagedScopeCognitionOptimizationRepo(t, 1)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(cfg)
	}
	cfg.ManagedScope.ApprovalMode = machinecontract.ScopeApprovalModeReview
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err := scopechange.Build(root, "2026-10-27T00:00:00Z", scopechange.CandidateSet{
		Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.InteractionRequired {
		t.Fatal("fixture preview does not need a person")
	}
	encoded, err := scopechange.Encode(preview)
	if err != nil {
		t.Fatal(err)
	}
	previewRel := ".aoci/scope-change/pending/preview.json"
	if err := os.MkdirAll(filepath.Join(root, ".aoci", "scope-change", "pending"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(previewRel)), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, previewRel, preview
}

func scopeApprovalArguments(previewFile string) map[string]any {
	return map[string]any{"intent": machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindScopeChange, "preview_file": previewFile}}
}

// askHost calls aoci_maintain from a host that answers every dialog with
// answer, or declares no dialogs when answer is nil, and returns the result,
// its raw text, whether it was a tool error, and the dialogs it was shown.
func askHost(t *testing.T, root string, client *mcp.Implementation, arguments map[string]any, answer *mcp.ElicitResult) (map[string]any, string, bool, []*mcp.ElicitParams) {
	t.Helper()
	server, err := NewServer(root, "human-approval-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	shown := []*mcp.ElicitParams{}
	options := &mcp.ClientOptions{}
	if answer != nil {
		options.ElicitationHandler = func(_ context.Context, request *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			shown = append(shown, request.Params)
			return answer, nil
		}
	}
	if client == nil {
		client = &mcp.Implementation{Name: "test host", Version: "1"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	clientSession, err := mcp.NewClient(client, options).Connect(ctx, clientTransport, nil)
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
			t.Fatalf("result is not JSON: %v %s", err, text)
		}
	}
	return decoded, text, result.IsError, shown
}

func approveAnswer() *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}
}

func TestHumanApprovalRecordsTheApprovalApplyReads(t *testing.T) {
	root, previewRel, preview := pendingScopeApprovalRepo(t)
	result, text, isError, shown := askHost(t, root, &mcp.Implementation{Name: "Claude Code / Desktop", Version: "1"},
		scopeApprovalArguments(previewRel), approveAnswer())
	if isError || result["status"] != machinecontract.HumanApprovalStatusApproved || len(shown) != 1 {
		t.Fatalf("approval not recorded: %s", text)
	}
	// One required decision with both outcomes named, and an optional note.
	schema, _ := json.Marshal(shown[0].RequestedSchema)
	for _, want := range []string{`"decision"`, `"approve"`, `"reject"`, `"enumNames"`, `"note"`, `"required":["decision"]`} {
		if !strings.Contains(string(schema), want) {
			t.Fatalf("dialog form lacks %s: %s", want, schema)
		}
	}
	if !strings.Contains(shown[0].Message, preview.Plan.PlanID) || !strings.Contains(shown[0].Message, scopechange.ApprovalEffectSummary(preview)) {
		t.Fatalf("dialog does not describe the change: %q", shown[0].Message)
	}
	approvalPath := filepath.Join(root, ".aoci", "scope-change", "pending", "approval.json")
	data, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := scopechange.DecodeApproval(data)
	if err != nil || !scopechange.ApprovalBinds(preview, approval) || approval.Actor != "host-dialog@Claude-Code---Desktop" {
		t.Fatalf("recorded approval %#v %v", approval, err)
	}
	if result["approval_file"] != ".aoci/scope-change/pending/approval.json" || result["formal_writes_started"] != false {
		t.Fatalf("approval location: %s", text)
	}
	again, _, _, shownAgain := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer())
	if again["status"] != machinecontract.HumanApprovalStatusAlreadyApproved || len(shownAgain) != 0 ||
		again["approval_mechanism"] != machinecontract.ApprovalMechanismHostElicitationConfirmation {
		t.Fatalf("a recorded approval was asked again: %v", again)
	}
	if _, err := scopechange.ApplyAuthorized(root, preview, approval, nil); err != nil {
		t.Fatalf("the dialog approval does not apply: %v", err)
	}
}

func TestHumanApprovalOutcomesThatWriteNothing(t *testing.T) {
	for name, test := range map[string]struct {
		answer *mcp.ElicitResult
		status string
		action string
		note   string
	}{
		"reject":    {&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "reject", "note": strings.Repeat("长", 600)}}, machinecontract.HumanApprovalStatusRejected, machinecontract.HumanApprovalActionAskPerson, strings.Repeat("长", humanApprovalNoteMaxRunes)},
		"decline":   {&mcp.ElicitResult{Action: "decline"}, machinecontract.HumanApprovalStatusRejected, machinecontract.HumanApprovalActionAskPerson, ""},
		"cancel":    {&mcp.ElicitResult{Action: "cancel"}, machinecontract.HumanApprovalStatusDismissed, machinecontract.HumanApprovalActionAskAgain, ""},
		"no-dialog": {nil, machinecontract.HumanApprovalStatusHostDialogUnavailable, machinecontract.HumanApprovalActionTerminal, ""},
	} {
		t.Run(name, func(t *testing.T) {
			root, previewRel, _ := pendingScopeApprovalRepo(t)
			result, text, isError, _ := askHost(t, root, nil, scopeApprovalArguments(previewRel), test.answer)
			if isError || result["status"] != test.status || result["next_action"] != test.action {
				t.Fatalf("%s: %s", name, text)
			}
			if note, _ := result["human_note"].(string); note != test.note {
				t.Fatalf("%s: note %q", name, note)
			}
			if _, err := os.Stat(filepath.Join(root, ".aoci", "scope-change", "pending", "approval.json")); !os.IsNotExist(err) {
				t.Fatalf("%s left an approval: %v", name, err)
			}
		})
	}
}

func TestHumanApprovalKeepsTerminalOnlyRepositoriesInTheTerminal(t *testing.T) {
	root, previewRel, preview := pendingScopeApprovalRepoWith(t, func(cfg *config.Config) {
		if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
			t.Fatal(err)
		}
	})
	result, text, isError, shown := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer())
	commands, _ := result["next_commands"].([]any)
	if isError || len(shown) != 0 || result["status"] != machinecontract.HumanApprovalStatusHostDialogDisabled || len(commands) != 2 {
		t.Fatalf("tty_only opened a dialog: %s", text)
	}
	approval, err := scopechange.NewHostDialogApproval(preview, "host-dialog", "2026-10-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := humanApprovalArtifactBytes(approval)
	if err := os.WriteFile(filepath.Join(root, ".aoci", "scope-change", "pending", "approval.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, text, isError, _ := askHost(t, root, nil, scopeApprovalArguments(previewRel), nil); !isError || !strings.Contains(text, "human_approval_file_not_admitted") {
		t.Fatalf("a dialog approval the team no longer admits: %s", text)
	}
}

func TestHumanApprovalRefusesWhatItCannotTrust(t *testing.T) {
	root, previewRel, _ := pendingScopeApprovalRepo(t)
	pending := filepath.Join(root, filepath.FromSlash(previewRel))
	raw, err := os.ReadFile(pending)
	if err != nil {
		t.Fatal(err)
	}
	// The copy lives outside the repository: a new file inside it would
	// change the inventory and make the real preview stale.
	outside := filepath.Join(t.TempDir(), "preview.json")
	if err := os.WriteFile(outside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	named := []string{"preview.json", outside, "../preview.json", ".aoci/../preview.json", ".aoci/scope-change/pending", ".aoci/scope-change/missing.json"}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, ".aoci", "scope-change", "pending", "link.json")); err != nil {
			t.Fatal(err)
		}
		named = append(named, ".aoci/scope-change/pending/link.json")
	}
	for _, previewFile := range named {
		if _, text, isError, shown := askHost(t, root, nil, scopeApprovalArguments(previewFile), approveAnswer()); !isError || len(shown) != 0 ||
			!strings.Contains(text, "human_approval_preview_path_invalid") {
			t.Fatalf("%s: %s", previewFile, text)
		}
	}
	// The absolute spelling of the real preview is read like the relative one.
	if result, text, _, _ := askHost(t, root, nil, scopeApprovalArguments(pending), nil); result["status"] != machinecontract.HumanApprovalStatusHostDialogUnavailable {
		t.Fatalf("absolute preview path: %s", text)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "scope-change", "pending", "approval.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, text, isError, _ := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer()); !isError || !strings.Contains(text, "human_approval_file_conflict") {
		t.Fatalf("a foreign approval file: %s", text)
	}
	if err := os.WriteFile(pending, []byte("{\"version\":\"other\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, text, isError, _ := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer()); !isError || !strings.Contains(text, "managed_scope_preview") {
		t.Fatalf("an invalid preview: %s", text)
	}
}

func TestHumanApprovalNeverShowsAStaleChange(t *testing.T) {
	root, previewRel, _ := pendingScopeApprovalRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope.ApprovalMode = machinecontract.ScopeApprovalModeAuto
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result, text, isError, shown := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer())
	if isError || len(shown) != 0 || result["status"] != machinecontract.HumanApprovalStatusStale ||
		result["next_action"] != machinecontract.HumanApprovalActionRebuild {
		t.Fatalf("a stale change reached the person: %s", text)
	}
	commands, _ := result["next_commands"].([]any)
	if len(commands) != 1 || !strings.Contains(commands[0].(string), "'scope' 'activate'") && !strings.Contains(commands[0].(string), `"scope" "activate"`) {
		t.Fatalf("stale result does not rebuild through activation: %v", commands)
	}
}

func TestHumanApprovalRequestShape(t *testing.T) {
	root, previewRel, _ := pendingScopeApprovalRepo(t)
	for _, input := range []maintainIn{
		{Intent: machinecontract.MaintainIntentHumanApproval},
		{Intent: machinecontract.MaintainIntentHumanApproval, Scope: "code", ApprovalRequest: &humanApprovalRequest{Kind: "onboarding"}},
		{Intent: machinecontract.MaintainIntentHumanApproval, ObjectRefs: []string{"code:a.go"}, ApprovalRequest: &humanApprovalRequest{Kind: "onboarding"}},
		{Intent: machinecontract.MaintainIntentHumanApproval, ApprovalRequest: &humanApprovalRequest{Kind: "scope_change"}},
		{Intent: machinecontract.MaintainIntentHumanApproval, ApprovalRequest: &humanApprovalRequest{Kind: "onboarding", PreviewFile: previewRel}},
		{Intent: machinecontract.MaintainIntentHumanApproval, ApprovalRequest: &humanApprovalRequest{Kind: "other"}},
	} {
		if result := handleHumanApproval(context.Background(), nil, root, input); !result.IsError {
			t.Fatalf("malformed request accepted: %#v", input)
		}
	}
	// Without a session to ask through the ordinary router refuses the intent,
	// and an approval request never rides another Maintain mode.
	for _, input := range []maintainIn{
		{Intent: machinecontract.MaintainIntentHumanApproval, ApprovalRequest: &humanApprovalRequest{Kind: "onboarding"}},
		{ApprovalRequest: &humanApprovalRequest{Kind: "onboarding"}},
	} {
		if result := handleMaintainInput(root, "test", input, nil); !result.IsError {
			t.Fatalf("router accepted %#v", input)
		}
	}
	// No onboarding review is waiting here.
	if _, text, isError, _ := askHost(t, root, nil, map[string]any{"intent": machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindOnboarding}}, nil); !isError || !strings.Contains(text, "onboarding") {
		t.Fatalf("onboarding approval without a session: %s", text)
	}
	// A preview that needs no person is answered with its apply command.
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope.ApprovalMode = machinecontract.ScopeApprovalModeAuto
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	quiet, err := scopechange.Build(root, "2026-10-27T00:00:00Z", scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := scopechange.Encode(quiet)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(previewRel)), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if !quiet.Plan.InteractionRequired {
		if _, text, isError, _ := askHost(t, root, nil, scopeApprovalArguments(previewRel), approveAnswer()); !isError ||
			!strings.Contains(text, "managed_scope_approval_not_required") {
			t.Fatalf("a preview needing no person: %s", text)
		}
	}
}

func TestHumanApprovalHelpers(t *testing.T) {
	if got := humanApprovalPathList([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("short list %q", got)
	}
	long := humanApprovalPathList([]string{"a", "b", "c", "d", "e", "f", "g"})
	if !strings.Contains(long, "a, b, c, d, e") || !strings.Contains(long, "2") || strings.Contains(long, "f") {
		t.Fatalf("long list %q", long)
	}
	if _, ok := quoteCommandArgument("line\nbreak"); ok {
		t.Fatal("a newline argument was quoted")
	}
	if commands := humanApprovalCommands([]string{"scope", "bad\nargument"}, nil, []string{"scope", "status"}); len(commands) != 1 ||
		!strings.Contains(commands[0], "status") {
		t.Fatalf("unquotable commands were not dropped: %v", commands)
	}
	if hostShowsDialogs(nil) || hostDialogActor(nil) != "host-dialog" {
		t.Fatal("a missing session claims dialogs or a name")
	}
}
