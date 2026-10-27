package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// aoci_maintain intent=human_approval asks the person at the keyboard, through
// the MCP host's dialog, to approve or reject one pending protected change.
//
// The terminal was the only channel, and it failed people in two ways: they
// did not know they had to open a terminal and type a 64-hex phrase, and a
// rejection was a dead end with no word on what to do next. The dialog shows
// the change as AOCI reads it from disk — never as the caller describes it —
// so what the person approves is what Apply binds, and both answers come back
// with the next step. Nothing here applies a change: an approval is recorded
// where the CLI's apply command already looks, and the caller runs it.
const (
	humanApprovalResultVersion = 1
	humanApprovalNoteMaxRunes  = 500
	humanApprovalListedPaths   = 5
	humanApprovalActorMaxRunes = 64
	humanApprovalArtifactMode  = 0o600
)

type humanApprovalRequest struct {
	Kind        string `json:"kind"`
	PreviewFile string `json:"preview_file,omitempty"`
}

type humanApprovalResult struct {
	Version             int      `json:"version"`
	Status              string   `json:"status"`
	Kind                string   `json:"kind"`
	FormalWritesStarted bool     `json:"formal_writes_started"`
	Summary             string   `json:"summary,omitempty"`
	ApprovalFile        string   `json:"approval_file,omitempty"`
	ApprovalMechanism   string   `json:"approval_mechanism,omitempty"`
	HumanNote           string   `json:"human_note,omitempty"`
	Message             string   `json:"message"`
	NextAction          string   `json:"next_action"`
	NextCommands        []string `json:"next_commands,omitempty"`
}

// pendingApproval is one protected change waiting for its person, as AOCI
// read it: what the dialog shows, how a yes is recorded and where, and the
// commands each outcome leads to.
type pendingApproval struct {
	kind          string
	operation     string
	summary       string
	dialog        string
	eligible      bool
	approvalPath  string
	approvalRel   string
	bindsExisting func([]byte) (bool, string)
	record        func(actor, approvedAt string) ([]byte, error)
	save          func([]byte) error
	apply         []string
	terminal      [][]string
	status        []string
	rejected      string
}

func validateHumanApprovalRequest(input maintainIn) *Fail {
	request := input.ApprovalRequest
	if request == nil {
		return &Fail{Code: errBadArgs, Msg: "human_approval_request_required"}
	}
	if input.Scope != "" || len(input.ObjectRefs) != 0 {
		return &Fail{Code: errBadArgs, Msg: "human_approval_takes_only_approval_request"}
	}
	switch request.Kind {
	case machinecontract.HumanApprovalKindScopeChange:
		if strings.TrimSpace(request.PreviewFile) == "" {
			return &Fail{Code: errBadArgs, Msg: "human_approval_preview_file_required"}
		}
	case machinecontract.HumanApprovalKindOnboarding:
		if request.PreviewFile != "" {
			return &Fail{Code: errBadArgs, Msg: "human_approval_preview_file_not_allowed_for_onboarding"}
		}
	default:
		return &Fail{Code: errBadArgs, Msg: "human_approval_kind_invalid"}
	}
	return nil
}

func handleHumanApproval(ctx context.Context, session *mcp.ServerSession, root string, input maintainIn) *mcp.CallToolResult {
	start := time.Now()
	if fail := validateHumanApprovalRequest(input); fail != nil {
		return failResult(fail)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return failResult(&Fail{Code: errBadArgs, Msg: "human_approval_configuration_invalid"})
	}
	var pending *pendingApproval
	var early *humanApprovalResult
	var fail *Fail
	switch input.ApprovalRequest.Kind {
	case machinecontract.HumanApprovalKindScopeChange:
		pending, early, fail = loadScopeChangeApproval(root, input.ApprovalRequest.PreviewFile)
	default:
		pending, early, fail = loadOnboardingApproval(root)
	}
	if fail != nil {
		return failResult(fail)
	}
	result := early
	if result == nil {
		result, fail = decideHumanApproval(ctx, session, cfg, pending)
		if fail != nil {
			return failResult(fail)
		}
	}
	// The person's answer is the human's event; every outcome that asked no
	// one is the agent's. governance_result names which outcome it was.
	event := ledger.Event{Op: "human_approval", Source: ledger.SourceAgent, Result: ledger.ResultOK,
		GovernanceResult: result.Status, NextAction: result.NextAction, FormalWritesStarted: ledgerFlag(false),
		DurationMs: time.Since(start).Milliseconds()}
	switch result.Status {
	case machinecontract.HumanApprovalStatusApproved:
		event.Source = ledger.SourceHuman
	case machinecontract.HumanApprovalStatusRejected:
		event.Source, event.Result = ledger.SourceHuman, ledger.ResultRejected
	}
	ledger.Append(root, cfg.LedgerEnabled, event)
	data, err := json.Marshal(result)
	if err != nil {
		return errResult(errInternal, "human_approval_result_invalid", "")
	}
	return textResult(string(data) + "\n")
}

// decideHumanApproval runs the gates in the order a person would want them:
// an approval already on record is reported before anyone is asked again, a
// repository whose approvals stay in the terminal says so before the host is
// probed, and only then does the dialog open.
func decideHumanApproval(ctx context.Context, session *mcp.ServerSession, cfg *config.Config, pending *pendingApproval) (*humanApprovalResult, *Fail) {
	result := &humanApprovalResult{Version: humanApprovalResultVersion, Kind: pending.kind, Summary: pending.summary}
	conflict := &Fail{Code: errWriteConflict, Msg: "human_approval_file_conflict: " + pending.approvalRel,
		Hint: mcpMessage("mcp.human_approval.hint.file_conflict", pending.approvalRel)}
	if existing, err := os.ReadFile(pending.approvalPath); err == nil {
		binds, mechanism := pending.bindsExisting(existing)
		if !binds {
			return nil, conflict
		}
		if !cfg.AdmitsApprovalMechanism(mechanism) {
			return nil, &Fail{Code: errWriteConflict, Msg: "human_approval_file_not_admitted: " + pending.approvalRel,
				Hint: mcpMessage("mcp.human_approval.hint.file_not_admitted", pending.approvalRel)}
		}
		result.Status = machinecontract.HumanApprovalStatusAlreadyApproved
		result.ApprovalFile, result.ApprovalMechanism = pending.approvalRel, mechanism
		result.Message = mcpMessage("mcp.human_approval.result.already_approved", pending.approvalRel)
		result.NextAction = machinecontract.HumanApprovalActionApply
		result.NextCommands = humanApprovalCommands(pending.apply)
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, conflict
	}
	if !pending.eligible {
		return terminalApprovalResult(result, pending, machinecontract.HumanApprovalStatusTTYRequired,
			mcpMessage("mcp.human_approval.result.tty_required")), nil
	}
	if !cfg.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation) {
		return terminalApprovalResult(result, pending, machinecontract.HumanApprovalStatusHostDialogDisabled,
			mcpMessage("mcp.human_approval.result.host_dialog_disabled")), nil
	}
	if !hostShowsDialogs(session) {
		return terminalApprovalResult(result, pending, machinecontract.HumanApprovalStatusHostDialogUnavailable,
			mcpMessage("mcp.human_approval.result.host_dialog_unavailable")), nil
	}
	answer, err := session.Elicit(ctx, &mcp.ElicitParams{Mode: "form", Message: pending.dialog,
		RequestedSchema: humanApprovalFormSchema()})
	if err != nil || answer == nil || answer.Action == "cancel" {
		return terminalApprovalResult(result, pending, machinecontract.HumanApprovalStatusDismissed,
			mcpMessage("mcp.human_approval.result.dismissed")), nil
	}
	decision, note := humanApprovalAnswer(answer)
	result.HumanNote = note
	if decision != machinecontract.HumanApprovalDecisionApprove {
		result.Status = machinecontract.HumanApprovalStatusRejected
		result.Message = pending.rejected
		result.NextAction = machinecontract.HumanApprovalActionAskPerson
		result.NextCommands = humanApprovalCommands(pending.status)
		return result, nil
	}
	data, err := pending.record(hostDialogActor(session), time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
	if err == nil {
		err = pending.save(data)
	}
	if err != nil {
		return terminalApprovalResult(result, pending, machinecontract.HumanApprovalStatusDismissed,
			mcpMessage("mcp.human_approval.result.record_failed", localeSafeMCPDetail(err.Error()))), nil
	}
	result.Status = machinecontract.HumanApprovalStatusApproved
	result.ApprovalFile = pending.approvalRel
	result.ApprovalMechanism = machinecontract.ApprovalMechanismHostElicitationConfirmation
	result.Message = mcpMessage("mcp.human_approval.result.approved")
	result.NextAction = machinecontract.HumanApprovalActionApply
	result.NextCommands = humanApprovalCommands(pending.apply)
	return result, nil
}

// terminalApprovalResult is every outcome that leaves the decision to a
// terminal: nothing was written, and next_commands carries the approve-then-
// apply pair a person runs themselves.
func terminalApprovalResult(result *humanApprovalResult, pending *pendingApproval, status, message string) *humanApprovalResult {
	result.Status = status
	result.Message = message
	result.NextAction = machinecontract.HumanApprovalActionTerminal
	if status == machinecontract.HumanApprovalStatusDismissed {
		result.NextAction = machinecontract.HumanApprovalActionAskAgain
	}
	result.NextCommands = humanApprovalCommands(pending.terminal...)
	return result
}

func hostShowsDialogs(session *mcp.ServerSession) bool {
	if session == nil {
		return false
	}
	params := session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false
	}
	capabilities := params.Capabilities.Elicitation
	// A client that names only URL elicitation cannot render a form; one that
	// names neither predates the split and renders forms.
	return capabilities.Form != nil || capabilities.URL == nil
}

func humanApprovalFormSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"decision": map[string]any{
				"type":  "string",
				"title": mcpMessage("mcp.human_approval.field.decision"),
				"enum": []string{machinecontract.HumanApprovalDecisionApprove,
					machinecontract.HumanApprovalDecisionReject},
				"enumNames": []string{mcpMessage("mcp.human_approval.option.approve"),
					mcpMessage("mcp.human_approval.option.reject")},
			},
			// No maxLength: the SDK validates the answer against this schema,
			// and a host that let a longer note through would turn the
			// person's decision into a validation error. The note is cut here.
			"note": map[string]any{
				"type":        "string",
				"title":       mcpMessage("mcp.human_approval.field.note"),
				"description": mcpMessage("mcp.human_approval.field.note_description"),
			},
		},
		"required": []string{"decision"},
	}
}

// humanApprovalAnswer reads the form. Decline is a rejection: the person made
// an explicit choice, and it carries no note.
func humanApprovalAnswer(answer *mcp.ElicitResult) (string, string) {
	if answer.Action != "accept" {
		return machinecontract.HumanApprovalDecisionReject, ""
	}
	decision, _ := answer.Content["decision"].(string)
	note, _ := answer.Content["note"].(string)
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > humanApprovalNoteMaxRunes {
		note = string([]rune(note)[:humanApprovalNoteMaxRunes])
	}
	return decision, note
}

// hostDialogActor names who approved for the audit record: the dialog of the
// host that asked. The host's own name is reduced to the actor alphabet.
func hostDialogActor(session *mcp.ServerSession) string {
	actor := "host-dialog"
	if session == nil {
		return actor
	}
	params := session.InitializeParams()
	if params == nil || params.ClientInfo == nil {
		return actor
	}
	var name strings.Builder
	for _, character := range params.ClientInfo.Name {
		if name.Len() >= humanApprovalActorMaxRunes {
			break
		}
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9', character == '.', character == '_', character == '-':
			name.WriteRune(character)
		default:
			name.WriteRune('-')
		}
	}
	if client := strings.Trim(name.String(), "-._"); client != "" {
		actor += "@" + client
	}
	return actor
}

// humanApprovalCommands prefixes each argument list with the running server's
// executable, quoting every argument the way the prefix is quoted. A list
// whose arguments cannot be quoted safely is left out: an absent advisory
// beats a wrong command.
func humanApprovalCommands(argumentLists ...[]string) []string {
	prefix := nextCommandPrefix()
	if prefix == "" {
		return nil
	}
	commands := []string{}
	for _, arguments := range argumentLists {
		if len(arguments) == 0 {
			continue
		}
		parts := []string{prefix}
		quotable := true
		for _, argument := range arguments {
			quoted, ok := quoteCommandArgument(argument)
			if !ok {
				quotable = false
				break
			}
			parts = append(parts, quoted)
		}
		if quotable {
			commands = append(commands, strings.Join(parts, " "))
		}
	}
	if len(commands) == 0 {
		return nil
	}
	return commands
}

func quoteCommandArgument(argument string) (string, bool) {
	if strings.ContainsAny(argument, "\x00\r\n") {
		return "", false
	}
	if strings.HasPrefix(nextCommandPrefix(), `"`) {
		if strings.Contains(argument, `"`) {
			return "", false
		}
		return `"` + argument + `"`, true
	}
	return "'" + strings.ReplaceAll(argument, "'", `'"'"'`) + "'", true
}

// runtimeArtifactPath resolves a caller-named artifact inside the repository's
// .aoci directory without following a symbolic link anywhere on the way. It
// creates nothing; an absolute path must already sit under the root.
func runtimeArtifactPath(root, named string) (string, string, *Fail) {
	invalid := &Fail{Code: errPathUnsafe, Msg: "human_approval_preview_path_invalid",
		Hint: mcpMessage("mcp.human_approval.hint.preview_path")}
	candidate := strings.TrimSpace(named)
	if filepath.IsAbs(candidate) {
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			return "", "", invalid
		}
		candidate = relative
	}
	relative, err := afs.NormalizeRelPath(candidate)
	if err != nil || !strings.HasPrefix(relative, ".aoci/") {
		return "", "", invalid
	}
	current := root
	components := strings.Split(relative, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", "", invalid
		}
		if index < len(components)-1 && !info.IsDir() || index == len(components)-1 && !info.Mode().IsRegular() {
			return "", "", invalid
		}
	}
	return current, relative, nil
}

func loadScopeChangeApproval(root, previewFile string) (*pendingApproval, *humanApprovalResult, *Fail) {
	previewPath, previewRel, fail := runtimeArtifactPath(root, previewFile)
	if fail != nil {
		return nil, nil, fail
	}
	data, err := os.ReadFile(previewPath)
	if err != nil {
		return nil, nil, &Fail{Code: errBadArgs, Msg: "human_approval_preview_unreadable"}
	}
	preview, err := scopechange.DecodePreview(data)
	if err != nil {
		return nil, nil, &Fail{Code: errBadArgs, Msg: err.Error()}
	}
	approvalPath := filepath.Join(filepath.Dir(previewPath), "approval.json")
	approvalRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(previewRel)), "approval.json"))
	pending := &pendingApproval{
		kind: machinecontract.HumanApprovalKindScopeChange, eligible: scopechange.HostDialogEligible(preview),
		approvalPath: approvalPath, approvalRel: approvalRel,
		apply: []string{"--repo", root, "scope", "apply", "--preview-file", previewPath, "--approval-file", approvalPath},
		terminal: [][]string{
			{"--repo", root, "scope", "approve", "--preview-file", previewPath, "--actor", "<id>", "--out-file", approvalPath},
			{"--repo", root, "scope", "apply", "--preview-file", previewPath, "--approval-file", approvalPath},
		},
		status:   []string{"--repo", root, "scope", "status"},
		rejected: mcpMessage("mcp.human_approval.result.rejected.scope_change"),
	}
	if !preview.Plan.InteractionRequired {
		return nil, nil, &Fail{Code: errBadArgs, Msg: "managed_scope_approval_not_required",
			Hint: strings.Join(humanApprovalCommands(
				[]string{"--repo", root, "scope", "apply", "--preview-file", previewPath}), "\n")}
	}
	rebuilt, err := scopechange.Build(root, preview.Plan.PreparedAt, preview.CandidateSet)
	if err != nil || rebuilt.EnvelopeDigest != preview.EnvelopeDigest {
		stale := &humanApprovalResult{Version: humanApprovalResultVersion, Kind: pending.kind,
			Status: machinecontract.HumanApprovalStatusStale, Message: mcpMessage("mcp.human_approval.result.stale"),
			NextAction: machinecontract.HumanApprovalActionRebuild}
		if len(preview.CandidateSet.Entries) == 0 && len(preview.CandidateSet.Dispositions) == 0 {
			stale.NextCommands = humanApprovalCommands([]string{"--repo", root, "scope", "activate"})
		}
		return nil, stale, nil
	}
	pending.summary = scopechange.ApprovalEffectSummary(preview)
	lines := []string{mcpMessage("mcp.human_approval.dialog.scope_change.title", filepath.Base(root)),
		mcpMessage("mcp.human_approval.dialog.effects", pending.summary)}
	removed := []string{}
	for _, change := range preview.Plan.EntryRemoves {
		removed = append(removed, change.Path)
	}
	if len(removed) > 0 {
		lines = append(lines, mcpMessage("mcp.human_approval.dialog.entries_removed", humanApprovalPathList(removed)))
	}
	leaving := []string{}
	for _, reduction := range preview.Plan.CoverageReductions {
		leaving = append(leaving, reduction.Path)
	}
	if len(leaving) > 0 {
		lines = append(lines, mcpMessage("mcp.human_approval.dialog.coverage_lost", humanApprovalPathList(leaving)))
	}
	lines = append(lines, mcpMessage("mcp.human_approval.dialog.identity", preview.Plan.PlanID),
		"", mcpMessage("mcp.human_approval.dialog.scope_change.footer"))
	pending.dialog = strings.Join(lines, "\n")
	pending.bindsExisting = func(existing []byte) (bool, string) {
		approval, err := scopechange.DecodeApproval(existing)
		if err != nil {
			return false, ""
		}
		return scopechange.ApprovalBinds(preview, approval), approval.Mechanism
	}
	pending.record = func(actor, approvedAt string) ([]byte, error) {
		approval, err := scopechange.NewHostDialogApproval(preview, actor, approvedAt)
		if err != nil {
			return nil, err
		}
		return humanApprovalArtifactBytes(approval)
	}
	pending.save = func(data []byte) error {
		return afs.AtomicCreateCASMode(approvalPath, data, humanApprovalArtifactMode)
	}
	return pending, nil, nil
}

func loadOnboardingApproval(root string) (*pendingApproval, *humanApprovalResult, *Fail) {
	session, envelopeData, err := onboarding.AwaitingApproval(root)
	if err != nil {
		return nil, nil, &Fail{Code: errBadArgs, Msg: err.Error(), Hint: strings.Join(humanApprovalCommands(
			[]string{"--repo", root, "cognition", "onboard", "status", "--json"}), "\n")}
	}
	approvalPath, approvalRel := onboarding.HumanApprovalPath(root, session)
	envelopePath := filepath.Join(root, filepath.FromSlash(session.EnvelopeArtifact))
	pending := &pendingApproval{
		kind: machinecontract.HumanApprovalKindOnboarding, operation: session.Operation, eligible: true,
		approvalPath: approvalPath, approvalRel: approvalRel,
		apply:  []string{"--repo", root, "cognition", "onboard", "apply", "--approval-file", approvalRel, "--json"},
		status: []string{"--repo", root, "cognition", "onboard", "status", "--json"},
	}
	var writeSet []string
	var digest, title, footer string
	repository := filepath.Base(root)
	if session.Operation == cognitionplan.OperationBootstrap {
		envelope, decodeErr := bootstrapapply.DecodeApplyEnvelope(envelopeData)
		if decodeErr != nil {
			return nil, nil, &Fail{Code: errBadArgs, Msg: decodeErr.Error()}
		}
		if guardErr := cognitionplan.ValidateExternalGuards(root, &envelope.Plan); guardErr != nil {
			return nil, staleOnboardingApproval(root), nil
		}
		writeSet, digest = envelope.WriteSet, envelope.EnvelopeDigest
		title = mcpMessage("mcp.human_approval.dialog.bootstrap.title", repository)
		footer = mcpMessage("mcp.human_approval.dialog.bootstrap.footer")
		pending.rejected = mcpMessage("mcp.human_approval.result.rejected.bootstrap")
		pending.bindsExisting = func(existing []byte) (bool, string) {
			approval, err := bootstrapapply.DecodeApproval(existing)
			if err != nil || bootstrapapply.ValidateApproval(envelope, approval) != nil {
				return false, ""
			}
			return true, approval.Mechanism
		}
		pending.record = func(actor, approvedAt string) ([]byte, error) {
			approval, err := bootstrapapply.RecordHostDialogApproval(envelope, actor, approvedAt)
			if err != nil {
				return nil, err
			}
			return humanApprovalArtifactBytes(approval)
		}
		pending.terminal = [][]string{
			{"--repo", root, "cognition", "bootstrap", "approve", "--envelope-file", envelopePath, "--actor", "<id>", "--out-file", approvalPath},
			pending.apply,
		}
	} else {
		envelope, decodeErr := migrationapply.DecodeApplyEnvelope(envelopeData)
		if decodeErr != nil {
			return nil, nil, &Fail{Code: errBadArgs, Msg: decodeErr.Error()}
		}
		if guardErr := cognitionplan.ValidateExternalGuards(root, &envelope.Plan); guardErr != nil {
			return nil, staleOnboardingApproval(root), nil
		}
		writeSet, digest = envelope.WriteSet, envelope.EnvelopeDigest
		title = mcpMessage("mcp.human_approval.dialog.migration.title", repository)
		footer = mcpMessage("mcp.human_approval.dialog.migration.footer")
		pending.rejected = mcpMessage("mcp.human_approval.result.rejected.migration")
		pending.bindsExisting = func(existing []byte) (bool, string) {
			approval, err := migrationapply.DecodeApproval(existing)
			if err != nil || migrationapply.ValidateApproval(envelope, approval) != nil {
				return false, ""
			}
			return true, approval.Mechanism
		}
		pending.record = func(actor, approvedAt string) ([]byte, error) {
			approval, err := migrationapply.RecordHostDialogApproval(envelope, actor, approvedAt)
			if err != nil {
				return nil, err
			}
			return humanApprovalArtifactBytes(approval)
		}
		pending.terminal = [][]string{
			{"--repo", root, "cognition", "migration", "approve", "--envelope-file", envelopePath, "--actor", "<id>", "--out-file", approvalPath},
			pending.apply,
		}
	}
	pending.summary = mcpMessage("mcp.human_approval.dialog.writes", len(writeSet), humanApprovalPathList(writeSet))
	pending.dialog = strings.Join([]string{title, pending.summary,
		mcpMessage("mcp.human_approval.dialog.identity", digest), "", footer}, "\n")
	pending.save = func(data []byte) error {
		_, err := onboarding.SaveHumanApproval(root, session, data)
		return err
	}
	return pending, nil, nil
}

func staleOnboardingApproval(root string) *humanApprovalResult {
	return &humanApprovalResult{Version: humanApprovalResultVersion, Kind: machinecontract.HumanApprovalKindOnboarding,
		Status: machinecontract.HumanApprovalStatusStale, Message: mcpMessage("mcp.human_approval.result.stale"),
		NextAction:   machinecontract.HumanApprovalActionRebuild,
		NextCommands: humanApprovalCommands([]string{"--repo", root, "cognition", "onboard", "prepare", "--json"})}
}

func humanApprovalArtifactBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// humanApprovalPathList names the first few paths and counts the rest, so a
// dialog stays readable however large the change.
func humanApprovalPathList(paths []string) string {
	if len(paths) <= humanApprovalListedPaths {
		return strings.Join(paths, ", ")
	}
	return mcpMessage("mcp.human_approval.dialog.more",
		strings.Join(paths[:humanApprovalListedPaths], ", "), len(paths)-humanApprovalListedPaths)
}
