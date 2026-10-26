package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

const hostInteractionVersion = machinecontract.HostInteractionV1

type hostInteractionRequired struct {
	Version             string `json:"version"`
	InteractionRequired bool   `json:"interaction_required"`
	InteractionKind     string `json:"interaction_kind"`
	ExactCommand        string `json:"exact_command"`
	Digest              string `json:"digest"`
	SafeSummary         string `json:"safe_summary"`
	ConfirmationPhrase  string `json:"confirmation_phrase"`
	ResumeAction        string `json:"resume_action"`
	TTYRequired         bool   `json:"tty_required"`
	ModelSelfApproval   bool   `json:"model_self_approval"`
	FormalWritesStarted bool   `json:"formal_writes_started"`
	// HostDialogRequest is the other route to the same person: the
	// aoci_maintain call that asks in the MCP host's dialog. Absent where no
	// dialog route exists for this action.
	HostDialogRequest *hostDialogRequest `json:"host_dialog_request,omitempty"`
}

// hostDialogRequest is an aoci_maintain call a host can make as written.
type hostDialogRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

// hostDialogAdmitted reports whether the team channel lets a person approve in
// the host's dialog; a route the apply step would refuse is never offered.
func hostDialogAdmitted(root string) bool {
	cfg, err := config.LoadReadOnly(root)
	return err == nil && cfg.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation)
}

// scopeChangeDialogRequest names the dialog route for a saved Scope Change
// preview. aoci_maintain reads previews only under the repository's .aoci
// directory, so a preview anywhere else has no dialog route, and neither has
// a repository whose team takes approvals in the terminal only. The path is
// repository-relative: the MCP server may spell the root differently (a
// symlinked /tmp on macOS), and a relative path means the same file to both.
func scopeChangeDialogRequest(root, previewFile string) *hostDialogRequest {
	if !hostDialogAdmitted(root) {
		return nil
	}
	absolute, err := filepath.Abs(previewFile)
	if err != nil {
		return nil
	}
	relative, err := filepath.Rel(filepath.Join(root, ".aoci"), absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return nil
	}
	return &hostDialogRequest{Tool: "aoci_maintain", Arguments: map[string]any{
		"intent": machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{
			"kind":         machinecontract.HumanApprovalKindScopeChange,
			"preview_file": ".aoci/" + filepath.ToSlash(relative),
		},
	}}
}

// onboardingDialogRequest names the dialog route for the prepared review of
// the current onboarding session.
func onboardingDialogRequest(root string) *hostDialogRequest {
	if !hostDialogAdmitted(root) {
		return nil
	}
	return &hostDialogRequest{Tool: "aoci_maintain", Arguments: map[string]any{
		"intent":           machinecontract.MaintainIntentHumanApproval,
		"approval_request": map[string]any{"kind": machinecontract.HumanApprovalKindOnboarding},
	}}
}

// humanConfirmation is one protected action a person must confirm: the
// phrase they type in a terminal, what the prompt tells them, what they are
// told when they do not approve, and the routes a caller without a terminal
// is offered instead.
type humanConfirmation struct {
	Phrase       string
	Prompt       string
	ExactCommand string
	Digest       string
	SafeSummary  string
	ResumeAction string
	// NotApproved is the localized next step printed when the person does not
	// approve; empty means running the same command again.
	NotApproved string
	Dialog      *hostDialogRequest
}

type hostInteractionError struct {
	State hostInteractionRequired
}

func (e *hostInteractionError) Error() string { return "human_tty_digest_confirmation_required" }

// humanPromptWriter is where a TTY confirmation prompt goes, and it is
// deliberately the process's own stderr rather than the cobra writer.
//
// The library entry point buffers both cobra writers into memory and flushes
// them only after Execute returns, so a prompt written through cmd.ErrOrStderr
// stayed invisible until the command had already finished. The prompt carries
// the exact phrase the operator has to type, which made the confirmation
// unreadable at the only moment it mattered: they typed blind or gave up. The
// branch below has already proven stdin is a character device, so the prompt
// and the read belong on the same unbuffered process-level pair.
//
// Tests redirect this; nothing else may. It is package-private, reachable from
// no flag and no environment variable.
var humanPromptWriter io.Writer = os.Stderr

// stdinIsCharDevice decides whether a real human can answer at all. Keeping it
// a variable lets the confirmed path be tested; it stays package-private so no
// caller outside this package can claim a TTY it does not have.
var stdinIsCharDevice = func() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// requireHumanConfirmation asks the person in a real terminal and accepts
// only the exact phrase. Anything else is a rejection, and the person is told
// both that nothing changed and what to do next: a confirmation whose only
// way out was a bare error left people who said no with no idea how to
// proceed.
func requireHumanConfirmation(cmd *cobra.Command, request humanConfirmation) error {
	if !stdinIsCharDevice() {
		return &hostInteractionError{State: hostInteractionRequired{
			Version: hostInteractionVersion, InteractionRequired: true,
			InteractionKind: "human_tty_digest_confirmation", ExactCommand: request.ExactCommand,
			Digest: request.Digest, SafeSummary: request.SafeSummary, ConfirmationPhrase: request.Phrase,
			ResumeAction: request.ResumeAction, TTYRequired: true, ModelSelfApproval: false,
			FormalWritesStarted: false, HostDialogRequest: request.Dialog,
		}}
	}
	fmt.Fprintln(humanPromptWriter, request.Prompt)
	fmt.Fprintln(humanPromptWriter, cliMessage("cli.approval.reject_hint"))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != request.Phrase {
		next := request.NotApproved
		if next == "" {
			next = cliMessage("cli.approval.next.again")
		}
		fmt.Fprintln(humanPromptWriter, cliMessage("cli.approval.not_approved", next))
		return fmt.Errorf("human_tty_digest_confirmation_not_confirmed")
	}
	return nil
}

// interactionExitError renders the zero-write boundary a caller without a
// terminal receives, naming the dialog route first when there is one.
func interactionExitError(interaction *hostInteractionError) *ExitError {
	message := cliMessage("cli.approval.interaction_required.terminal")
	if interaction.State.HostDialogRequest != nil {
		message = cliMessage("cli.approval.interaction_required.dialog")
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "interaction_required", Details: interaction.State, Msg: message}
}

func hostInteractionCommand(args ...string) string {
	executable, err := os.Executable()
	if err != nil {
		executable = "aoci"
	}
	parts := make([]string, 0, len(args)+1)
	if runtime.GOOS == "windows" {
		parts = append(parts, "& "+powerShellQuote(executable))
		for _, arg := range args {
			parts = append(parts, powerShellQuote(arg))
		}
		return strings.Join(parts, " ")
	}
	parts = append(parts, posixQuote(executable))
	for _, arg := range args {
		parts = append(parts, posixQuote(arg))
	}
	return strings.Join(parts, " ")
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
